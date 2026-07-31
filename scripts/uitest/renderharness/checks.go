// checks.go, the assertions. One function per reported issue, each reading a
// probe from scripts/uitest/probes.js and deciding whether what came back is a
// pass.
//
// The split matters: probes.js MEASURES (it is the half that needs a DOM) and
// this file JUDGES (it is the half that needs to explain itself). Every failure
// says what was expected, what was found, and what to change, because the owner
// of this repository orchestrates agents rather than debugging JavaScript by hand
// (root CLAUDE.md section 2).
package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// reporter collects the verdicts and prints them as they happen.
type reporter struct {
	failures []string
	passes   int
}

func (r *reporter) step(name string) {
	fmt.Printf("\n==> %s\n", name)
}

// assert is the single place a verdict is recorded. expected/actual/fix are all
// optional but a failure without them is a failure nobody can act on, so every
// caller supplies at least expected and actual.
func (r *reporter) assert(name string, ok bool, expected, actual, fix string) {
	if ok {
		r.passes++
		fmt.Printf("  PASS  %s\n", name)
		return
	}
	r.failures = append(r.failures, name)
	fmt.Printf("  FAIL  %s\n", name)
	if expected != "" {
		fmt.Printf("        expected: %s\n", expected)
	}
	if actual != "" {
		fmt.Printf("        actual:   %s\n", actual)
	}
	if fix != "" {
		fmt.Printf("        fix:      %s\n", fix)
	}
}

// fixture mirrors __uiProbes.fixture(): the seed facts both halves agree on.
type fixture struct {
	DocName            string `json:"docName"`
	PlaceholderPattern string `json:"placeholderPattern"`
	TooltipOriginal    string `json:"tooltipOriginal"`
	CategoryCount      int    `json:"categoryCount"`
}

// --- The layout contract ----------------------------------------------------

type layoutResult struct {
	Step            string `json:"step"`
	Down            int    `json:"down"`
	Across          int    `json:"across"`
	ViewClipsDown   int    `json:"viewClipsDown"`
	ViewClipsAcross int    `json:"viewClipsAcross"`
	Viewport        struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"viewport"`
	Scrollers []struct {
		Selector string `json:"selector"`
		Allowed  bool   `json:"allowed"`
		Down     int    `json:"down"`
		Across   int    `json:"across"`
	} `json:"scrollers"`
	Tallest struct {
		Selector string `json:"selector"`
		Height   int    `json:"height"`
	} `json:"tallest"`
}

// checkLayout walks all four wizard steps and asserts the page body does not
// scroll, in either direction.
//
// This is the fixed-height layout contract from frontend/CLAUDE.md, and it is the
// reason a renderer is needed at all: body and #app are 100vh, the chrome heights
// are fixed, and scrolling belongs inside a card body and nowhere else. A string
// test cannot see a column that grew past the window.
//
// The tolerance is 1 pixel, for sub-pixel rounding on a fractional viewport
// height. Anything above that is a real overflow.
func checkLayout(c *cdpClient, r *reporter) {
	r.step("The fixed-height layout contract")
	for _, step := range []string{"import", "identify", "anonymise", "export"} {
		var got layoutResult
		if err := c.eval(fmt.Sprintf("__uiProbes.layout(%q)", step), &got); err != nil {
			r.assert(step+" renders and can be measured", false,
				"a measurable screen", err.Error(),
				"The step threw while rendering. Run the node render tests for that view first.")
			continue
		}

		where := fmt.Sprintf("in a %dx%d viewport (tallest element inside #view: %s at %d px)",
			got.Viewport.Width, got.Viewport.Height, got.Tallest.Selector, got.Tallest.Height)

		// 1. The contract as written.
		r.assert(step+" does not scroll the page body", got.Down <= 1 && got.Across <= 1,
			"the body's scroll size equal to its client size, in both directions",
			fmt.Sprintf("%d px down, %d px across, %s", got.Down, got.Across, where),
			"body and #app are 100vh. Chrome that grew past the fixed header/step-bar/footer heights, "+
				"or wide content that is not inside its own overflow-x: auto container, will do this "+
				"(frontend/CLAUDE.md, the fixed-height layout contract).")

		// 2. The failure mode `main#view { overflow: hidden }` converts from a
		//    scrolling page into silently clipped content, which check 1 cannot see.
		r.assert(step+" fits the workspace without being clipped",
			got.ViewClipsDown <= 1 && got.ViewClipsAcross <= 1,
			"#view's scroll size equal to its client size: whatever it holds has to fit",
			fmt.Sprintf("%d px clipped off the bottom, %d px off the side, %s",
				got.ViewClipsDown, got.ViewClipsAcross, where),
			"main#view is overflow: hidden, so a card that does not fit is CUT OFF rather than "+
				"scrolling the page. A link in the chain from #view down to the scrolling card body is "+
				"missing min-height: 0.")

		// 3. "Scrolling happens inside a card body and nowhere else" is a claim
		//    about the whole screen, so it is checked over the whole screen.
		var stray []string
		for _, sc := range got.Scrollers {
			if !sc.Allowed {
				stray = append(stray, fmt.Sprintf("%s (%d px down, %d px across)", sc.Selector, sc.Down, sc.Across))
			}
		}
		r.assert(step+" scrolls only inside a card body", len(stray) == 0,
			"every scrolling element to be a card body, a group body, the rail or a .table-scroll",
			strings.Join(stray, "; "),
			"Move the overflow down onto the card body, or wrap wide content in its own "+
				"overflow-x: auto container.")
	}
}

// --- Reported issues 1 and 4: the source panes show source text -------------

type importPreviewResult struct {
	Error       string `json:"error"`
	Chars       int    `json:"chars"`
	Placeholder string `json:"placeholder"`
	Excerpt     string `json:"excerpt"`
	ShowsSource bool   `json:"showsSource"`
}

// checkImportPreview asserts the RENDERED text of the Import preview contains no
// placeholder.
//
// Reported issues 1 and 4 were the source panes showing pipeline output. The
// probe seeds a state that HAS a finished run in it, so there is real anonymised
// text available for a view to reach for by mistake; a state with no placeholders
// anywhere would pass this no matter what the view did.
//
// The pattern is compiled from the probe's own source string, so the two halves
// cannot drift apart on what a placeholder looks like.
func checkImportPreview(c *cdpClient, r *reporter, fx fixture) {
	r.step("The Import preview shows source text, not pipeline output")

	pattern, err := regexp.Compile(fx.PlaceholderPattern)
	if err != nil {
		r.assert("the placeholder pattern compiles in Go too", false,
			"a pattern valid in both RE2 and JavaScript", err.Error(),
			"Keep PLACEHOLDER_RE in scripts/uitest/probes.js to syntax both engines accept.")
		return
	}

	var got importPreviewResult
	if err := c.eval("__uiProbes.importPreview()", &got); err != nil {
		r.assert("the Import preview renders", false, "a rendered preview pane", err.Error(),
			"views/import.js previewBody must render for a seeded document.")
		return
	}
	if got.Error != "" {
		r.assert("the Import preview renders", false,
			"an .md-preview pane inside the Preview card", got.Error,
			"views/import.js previewCard renders previewBody(doc) when a document is selected.")
		return
	}

	r.assert("the Import preview contains no placeholder",
		got.Placeholder == "" && !pattern.MatchString(got.Excerpt),
		"no [CATEGORY_N] anywhere in the pane's rendered text",
		fmt.Sprintf("found %q around: %s", got.Placeholder, got.Excerpt),
		"views/import.js must render the imported markdown (state.documentSource), never anything "+
			"the pipeline produced.")

	r.assert("the Import preview actually shows the document",
		got.ShowsSource && got.Chars > 200,
		"the seeded source text, several hundred characters of it",
		fmt.Sprintf("%d characters rendered, source marker present: %t; opens with: %s",
			got.Chars, got.ShowsSource, got.Excerpt),
		"An empty pane would pass the placeholder check for the wrong reason, so it is asserted "+
			"separately.")
}

// --- Reported issue 3: the Configure rail -----------------------------------

type railResult struct {
	Error              string   `json:"error"`
	Sections           int      `json:"sections"`
	RailTabs           int      `json:"railTabs"`
	Routes             []string `json:"routes"`
	SmartOn            *bool    `json:"smartOn"`
	LocalOn            *bool    `json:"localOn"`
	CloudDisabled      *bool    `json:"cloudDisabled"`
	CloudOn            *bool    `json:"cloudOn"`
	Categories         int      `json:"categories"`
	CategoriesWithSize int      `json:"categoriesWithSize"`
}

// checkConfigureRail asserts the rail is three detection-route sections with the
// documented default switch positions and every category on screen.
//
// Reported issue 3: Configure stopped being a screen and became the left rail of
// Identify, restructured as three switchable DETECTION ROUTES rather than four
// peer tabs (root CLAUDE.md section 5, frontend/CLAUDE.md discipline rules).
// Smart detection is on by default and owns the scope controls; Local AI is off
// by default because sending the document to a model is the user's decision;
// Cloud AI renders disabled because a section with no switch reads as "always on".
func checkConfigureRail(c *cdpClient, r *reporter, fx fixture) {
	r.step("The Configure rail is three detection routes")

	var got railResult
	if err := c.eval("__uiProbes.configureRail()", &got); err != nil {
		r.assert("the Identify rail renders", false, "a rendered rail", err.Error(),
			"views/identifyrail.js railBody must render from a seeded state.")
		return
	}
	if got.Error != "" {
		r.assert("the Identify rail renders", false, "#identify-rail on the Identify screen", got.Error,
			"views/identify.js renders the rail section and hands it to renderIdentifyRail.")
		return
	}

	r.assert("the rail is three route sections", got.Sections == 3,
		"3 .rail-section elements", fmt.Sprintf("%d, routes: %s", got.Sections, strings.Join(got.Routes, ", ")),
		"views/identifyrail.js RAIL_SECTIONS defines Smart detection, Local AI and Cloud AI.")

	r.assert("the old tab strip is gone", got.RailTabs == 0,
		"0 [data-railtab] chips anywhere in the document", fmt.Sprintf("%d", got.RailTabs),
		"The rail switches sections on and off; it does not tab between them (BUILD-06).")

	r.assert("Smart detection is on by default", boolIs(got.SmartOn, true),
		"the rail-smart route switch checked", describeBool(got.SmartOn),
		"state.js settings.useSmartDetect defaults to true.")

	r.assert("Local AI is off by default", boolIs(got.LocalOn, false),
		"the rail-local route switch unchecked", describeBool(got.LocalOn),
		"state.js settings.useAI defaults to false. Detecting Ollama ENABLES this switch, "+
			"it never flips it.")

	r.assert("Cloud AI cannot be switched on",
		boolIs(got.CloudDisabled, true) && boolIs(got.CloudOn, false),
		"the rail-cloud route switch present, unchecked and disabled",
		fmt.Sprintf("disabled: %s, checked: %s", describeBool(got.CloudDisabled), describeBool(got.CloudOn)),
		"Cloud AI is not built (BUILD-05 decision 8) and renders disabled rather than omitted.")

	r.assert("every category checkbox is present", got.Categories >= fx.CategoryCount,
		fmt.Sprintf("at least %d .cat-toggle checkboxes", fx.CategoryCount),
		fmt.Sprintf("%d", got.Categories),
		"state.js ALL_CATEGORIES plus the country-specific ID categories must all reach the rail.")

	r.assert("every category checkbox is reachable without clicking",
		got.Categories > 0 && got.CategoriesWithSize == got.Categories,
		"all of them laid out with a non-zero height",
		fmt.Sprintf("%d of %d have a height", got.CategoriesWithSize, got.Categories),
		"A checkbox inside a folded group is in the DOM but not something the user can tick: the "+
			"category groups open by default.")
}

// --- Reported issue 6: the hover tooltip is VISIBLE -------------------------

type tooltipSample struct {
	Edge              string `json:"edge"`
	Appeared          bool   `json:"appeared"`
	Text              string `json:"text"`
	MarkOriginal      string `json:"markOriginal"`
	InsideCard        bool   `json:"insideCard"`
	InViewport        bool   `json:"inViewport"`
	HasSize           bool   `json:"hasSize"`
	InsidePaneSubtree bool   `json:"insidePaneSubtree"`
	PaintedOnTop      bool   `json:"paintedOnTop"`
	PaneOverflow      string `json:"paneOverflow"`
	TooltipRect       rect   `json:"tooltipRect"`
	CardRect          rect   `json:"cardRect"`
	PaneRect          rect   `json:"paneRect"`
}

type rect struct {
	Top, Left, Bottom, Right, Width, Height int
}

func (b rect) String() string {
	return fmt.Sprintf("(%d,%d)-(%d,%d) %dx%d", b.Left, b.Top, b.Right, b.Bottom, b.Width, b.Height)
}

type tooltipResult struct {
	Error     string          `json:"error"`
	Marks     int             `json:"marks"`
	Hoverable int             `json:"hoverable"`
	PaneWidth int             `json:"paneWidth"`
	Samples   []tooltipSample `json:"samples"`
}

// checkTooltip asserts a real hover produces a tooltip a user could actually see.
//
// This is THE check that cannot be made without a renderer, and the reason this
// layer exists. Reported issue 6 was a visible-only bug: the markup had been
// correct for months and `.pane-body { overflow: auto }` was clipping the tooltip
// out of existence near the pane's edges. Three marks are hovered, including the
// two nearest the pane's right and bottom edges, because the middle of the pane
// was never where it failed.
func checkTooltip(c *cdpClient, r *reporter, fx fixture) {
	r.step("The hover tooltip is visible, not clipped")

	var got tooltipResult
	if err := c.eval("__uiProbes.tooltipVisibility()", &got); err != nil {
		r.assert("the Compare card renders and a mark can be hovered", false,
			"a hoverable mark[data-original]", err.Error(),
			"views/anonymise.js compareCard plus highlight.js renderHighlighted.")
		return
	}
	if got.Error != "" {
		r.assert("the Compare card renders and a mark can be hovered", false,
			"marks in #anonymised-pane and a #mark-tooltip in #compare-card", got.Error,
			"views/anonymise.js compareCard renders the tooltip node as a child of the CARD, "+
				"not of the pane.")
		return
	}

	r.assert("the anonymised pane has marks a user could hover", got.Hoverable > 0,
		"at least one mark[data-original] visible inside the pane",
		fmt.Sprintf("%d marks rendered, %d of them visible in the pane", got.Marks, got.Hoverable),
		"highlight.js renders a mapped placeholder as a mark carrying data-original.")

	for _, s := range got.Samples {
		label := "the " + s.Edge + " mark"

		if !s.Appeared {
			r.assert(label+" shows a tooltip on mouseenter", false,
				"#mark-tooltip with hidden cleared", "the tooltip stayed hidden",
				"views/anonymise.js wireMarkTooltip binds mouseenter on every mark[data-original].")
			continue
		}

		r.assert(label+" shows the original value belonging to that mark",
			s.MarkOriginal != "" && strings.Contains(s.Text, s.MarkOriginal),
			fmt.Sprintf("the hovered mark's own original, %q, in the tooltip", s.MarkOriginal),
			fmt.Sprintf("%q", s.Text),
			"The tooltip's first line is mark.dataset.original, which comes from state.mapping.")

		r.assert(label+"'s tooltip is inside the Compare card", s.InsideCard,
			"the tooltip rect within the card rect",
			fmt.Sprintf("tooltip %s, card %s", s.TooltipRect, s.CardRect),
			"Anchor it to #compare-card and clamp it to the card's width, as wireMarkTooltip does.")

		r.assert(label+"'s tooltip is on screen", s.InViewport,
			"the tooltip rect within the viewport",
			fmt.Sprintf("tooltip %s", s.TooltipRect),
			"Flip the tooltip above the mark when there is no room below it.")

		r.assert(label+"'s tooltip has a size", s.HasSize,
			"a tooltip more than 10 px wide and tall", fmt.Sprintf("%dx%d", s.TooltipRect.Width, s.TooltipRect.Height),
			"An empty tooltip means innerHTML was never filled in.")

		r.assert(label+"'s tooltip is not inside the scrolling pane", !s.InsidePaneSubtree,
			fmt.Sprintf("#mark-tooltip outside #anonymised-pane, which is overflow: %s", s.PaneOverflow),
			"the tooltip is a descendant of the pane, so the pane clips it",
			"This is reported issue 6 exactly. Move the tooltip node up to #compare-card: an "+
				"overflow: auto ancestor clips it away near the pane's edges.")

		r.assert(label+"'s tooltip is painted, not clipped away", s.PaintedOnTop,
			"elementFromPoint at the tooltip's centre returning the tooltip",
			fmt.Sprintf("something else is at %s (pane %s, overflow: %s)",
				s.TooltipRect, s.PaneRect, s.PaneOverflow),
			"The rect of a clipped element is still a full-size rect, so this is the check that "+
				"catches it. Something is covering the tooltip or an ancestor is clipping it.")
	}
}

// --- No console error during the run ---------------------------------------

// bridgeAbsence recognises the failures this layer EXPECTS, because the Go bridge
// is not here.
//
// The page is served as static files, so window.go does not exist and every
// api.js call rejects with the readable error api.js is designed to throw. That is
// the documented boundary of this layer: it covers RENDERING, not the bridge. The
// bridge is covered by Go layer 1 (backend/app_e2e_test.go) and, on Windows, by
// the `wails dev` harness where the bridge really is attached.
//
// The exemption is deliberately narrow: it matches api.js's own wording and
// nothing else, so a genuine error is never waved through.
var bridgeAbsence = regexp.MustCompile(`(?i)wails bridge not available`)

// checkNoConsoleErrors asserts nothing unexpected reached the console.
func checkNoConsoleErrors(c *cdpClient, r *reporter) {
	r.step("No console error during the run")

	// Anything logged after the last probe has not been read off the socket yet.
	c.drain(600 * time.Millisecond)

	var unexpected []string
	var exempt int
	for _, line := range append(append([]string{}, c.consoleErrors...), c.pageExceptions...) {
		if bridgeAbsence.MatchString(line) {
			exempt++
			continue
		}
		unexpected = append(unexpected, line)
	}

	actual := "none"
	if len(unexpected) > 0 {
		actual = strings.Join(unexpected, " | ")
	}
	r.assert("the page logged no unexpected error", len(unexpected) == 0,
		"no console.error and no uncaught exception, apart from the absent Go bridge",
		actual,
		"Read the message above. If it IS about the missing bridge, the module is calling api.js "+
			"during render and needs to tolerate a rejection; this layer never has a bridge.")

	fmt.Printf("        (%d bridge-absence message(s) ignored: this layer covers rendering, not the bridge)\n", exempt)
}

func boolIs(p *bool, want bool) bool { return p != nil && *p == want }

func describeBool(p *bool) string {
	if p == nil {
		return "the control was not found at all"
	}
	return fmt.Sprintf("%t", *p)
}
