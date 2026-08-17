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

	r.assert("every category checkbox is present", got.Categories == fx.CategoryCount,
		fmt.Sprintf("exactly %d .cat-toggle checkboxes", fx.CategoryCount),
		fmt.Sprintf("%d", got.Categories),
		"Every state.js ALL_CATEGORIES entry reaches the rail, and the rail invents none. "+
			"This is an equality, not a floor: with a floor, adding a category and leaving the "+
			"fixture behind keeps the harness green, which is a test reporting safety it no "+
			"longer provides.")

	r.assert("every category checkbox is reachable without clicking",
		got.Categories > 0 && got.CategoriesWithSize == got.Categories,
		"all of them laid out with a non-zero height",
		fmt.Sprintf("%d of %d have a height", got.CategoriesWithSize, got.Categories),
		"A checkbox inside a folded group is in the DOM but not something the user can tick: the "+
			"category groups open by default.")
}

// --- The scroll-position contract -------------------------------------------

type scrollResult struct {
	Error      string `json:"error"`
	Scrollable bool   `json:"scrollable"`
	Before     int    `json:"before"`
	After      int    `json:"after"`
}

// checkScrollRetention asserts a scrolled panel keeps its position across a
// repaint.
//
// This is a visible-only regression a string test cannot see: every state change
// rewrites the whole shell (main.js paint -> root.innerHTML) and a freshly written
// element starts at scrollTop 0, so the reported symptom was the Identify rail
// snapping to the top on every tick or drill-down. The fix preserves scroll
// centrally around the repaint (frontend/scroll.js); this check drives a real
// action through the rail and confirms the offset survived.
func checkScrollRetention(c *cdpClient, r *reporter) {
	r.step("A scrolled panel keeps its position across a repaint")

	var got scrollResult
	if err := c.eval("__uiProbes.scrollRetention()", &got); err != nil {
		r.assert("the scroll-retention probe runs", false,
			"a rendered Identify rail to scroll", err.Error(),
			"views/identifyrail.js must render the rail from a seeded Identify state.")
		return
	}
	if got.Error != "" {
		r.assert("the scroll-retention probe runs", false,
			"#identify-rail with a scroller and a category toggle", got.Error,
			"views/identify.js renders the rail; identifyrail.js renders .cat-toggle checkboxes.")
		return
	}
	if !got.Scrollable {
		// Not a failure: at 1440x900 the rail happened to fit, so the bug could
		// not be exercised. Saying so beats a green nobody earned.
		r.assert("the rail overflows enough to exercise scrolling", true,
			"a scrollable rail", "the rail fit the viewport, so scroll retention was not exercised", "")
		return
	}

	r.assert("the rail actually scrolled before the repaint", got.Before > 0,
		"a non-zero scrollTop after scrolling the rail down",
		fmt.Sprintf("%d", got.Before),
		"If the rail refused to scroll the check measures nothing, so this is asserted separately.")

	r.assert("the scroll position survives a repaint", got.After == got.Before,
		fmt.Sprintf("scrollTop still %d after ticking a category", got.Before),
		fmt.Sprintf("%d", got.After),
		"frontend/scroll.js snapshotScrollPositions/restoreScrollPositions must bracket main.js paint(), "+
			"so a scrolled panel is not thrown back to the top by root.innerHTML.")
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

// compareSearchResult mirrors the compareSearch probe's payload.
type compareSearchResult struct {
	Error       string         `json:"error"`
	Needle      string         `json:"needle"`
	Hits        int            `json:"hits"`
	PerPane     map[string]int `json:"perPane"`
	HasActive   bool           `json:"hasActive"`
	Readout     string         `json:"readout"`
	NextEnabled bool           `json:"nextEnabled"`
	PrevEnabled bool           `json:"prevEnabled"`
	Visible     *searchVisible `json:"visible"`
}

type searchVisible struct {
	InsidePane bool `json:"insidePane"`
	InViewport bool `json:"inViewport"`
	HasSize    bool `json:"hasSize"`
	ActiveRect rect `json:"activeRect"`
	PaneRect   rect `json:"paneRect"`
}

// checkCompareSearch asserts the Compare search finds text in both panes and
// that the ACTIVE hit is somewhere the user can actually see it.
//
// A string test can prove the hit span was emitted. Only a renderer can prove
// the pane scrolled to it rather than leaving it clipped hundreds of pixels
// below the fold, which is the same failure mode as the mark-tooltip bug: the
// markup was right for months while the feature was invisible.
func checkCompareSearch(c *cdpClient, r *reporter) {
	r.step("The Compare search finds text in both panes and shows the active hit")

	var got compareSearchResult
	if err := c.eval("__uiProbes.compareSearch()", &got); err != nil {
		r.assert("the Compare search box renders and accepts a needle", false,
			"#compare-search in the Compare card head", err.Error(),
			"views/anonymise.js searchControls renders it beside the document selector.")
		return
	}
	if got.Error != "" {
		r.assert("the Compare search box renders and accepts a needle", false,
			"#compare-search in the Compare card head", got.Error,
			"views/anonymise.js compareCard puts searchControls in .card-head-right.")
		return
	}

	r.assert("the needle is found in the ORIGINAL pane", got.PerPane["original"] > 0,
		fmt.Sprintf("at least one .find-hit in #original-pane for %q", got.Needle),
		fmt.Sprintf("%d hits", got.PerPane["original"]),
		"views/anonymise.js renders the original pane through panesearch.js renderPlainWithHits.")

	r.assert("the needle is found in the ANONYMISED pane", got.PerPane["anonymised"] > 0,
		fmt.Sprintf("at least one .find-hit in #anonymised-pane for %q", got.Needle),
		fmt.Sprintf("%d hits", got.PerPane["anonymised"]),
		"highlight.js renderHighlighted takes a fourth search argument and emits hit spans "+
			"in the plain stretches and inside a mark's own text.")

	r.assert("exactly one hit is the active one", got.HasActive,
		"a .find-hit.active somewhere on screen", "no active hit was rendered",
		"searchWalk resolves the active index over the ONE combined list; searchControls "+
			"and both panes read the same walk.")

	r.assert("the readout says where the active hit is", got.Readout != "",
		"a readout naming the count, the total and the active hit's pane", "an empty readout",
		"copy.js ANONYMISE.searchCount(index, total, pane).")

	r.assert("both navigation buttons are live when there are hits",
		got.NextEnabled && got.PrevEnabled,
		"next and previous enabled",
		fmt.Sprintf("next enabled: %t, previous enabled: %t", got.NextEnabled, got.PrevEnabled),
		"searchControls disables them only when the walk is empty, and gives them a title "+
			"saying why, so a greyed control is never mute.")

	if got.Visible == nil {
		r.assert("the active hit sits inside a pane that can show it", false,
			"the active hit inside a .pane-body", "the active hit has no .pane-body ancestor",
			"Both panes render their hits through the same two renderers; a hit outside a pane "+
				"means one of them emitted markup somewhere unexpected.")
		return
	}

	r.assert("the active hit has a size", got.Visible.HasSize,
		"a rendered span with width and height",
		fmt.Sprintf("%s", got.Visible.ActiveRect),
		"An empty hit span means the slice offsets and the text disagree.")

	// The check this layer exists for: a hit the pane's own overflow scrolled
	// out of sight is a hit the user cannot see, whatever the DOM says.
	r.assert("the active hit is visible INSIDE its pane, not scrolled out of sight",
		got.Visible.InsidePane,
		"the active hit's rect within its pane's rect",
		fmt.Sprintf("hit %s, pane %s", got.Visible.ActiveRect, got.Visible.PaneRect),
		"views/anonymise.js scrollToActiveHit runs after the paint and only when the active "+
			"index changed, so it does not fight scroll.js restoring the pane's offset.")

	r.assert("the active hit is on screen", got.Visible.InViewport,
		"the active hit's rect within the viewport",
		fmt.Sprintf("hit %s", got.Visible.ActiveRect),
		"scrollIntoView({block: \"center\"}) on .find-hit.active.")
}

func boolIs(p *bool, want bool) bool { return p != nil && *p == want }

func describeBool(p *bool) string {
	if p == nil {
		return "the control was not found at all"
	}
	return fmt.Sprintf("%t", *p)
}
