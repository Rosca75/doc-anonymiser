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

// --- The Configure rail -----------------------------------------------------

type railResult struct {
	Error                         string   `json:"error"`
	Sections                      int      `json:"sections"`
	RailTabs                      int      `json:"railTabs"`
	Routes                        []string `json:"routes"`
	SmartOn                       *bool    `json:"smartOn"`
	LocalOn                       *bool    `json:"localOn"`
	Categories                    int      `json:"categories"`
	CategoriesWithSize            int      `json:"categoriesWithSize"`
	CategoriesWithSizeAfterExpand int      `json:"categoriesWithSizeAfterExpand"`
	SignalRows                    []string `json:"signalRows"`
	SignalMasters                 []string `json:"signalMasters"`
	SignalRowLine                 *struct {
		SameRow           *bool  `json:"sameRow"`
		DrillIsAfterLabel *bool  `json:"drillIsAfterLabel"`
		HelpIsAfterDrill  *bool  `json:"helpIsAfterDrill"`
		FitsTheRail       *bool  `json:"fitsTheRail"`
		Widths            string `json:"widths"`
	} `json:"signalRowLine"`
	MethodPairRow *struct {
		BuiltInTop            int      `json:"builtInTop"`
		HeuristicTop          int      `json:"heuristicTop"`
		SameRow               *bool    `json:"sameRow"`
		HeuristicIsToTheRight *bool    `json:"heuristicIsToTheRight"`
		LabelWidths           []string `json:"labelWidths"`
		LabelsFullyShown      *bool    `json:"labelsFullyShown"`
	} `json:"methodPairRow"`
}

// checkConfigureRail asserts the rail is the two detection-route sections with
// the documented default switch positions and every category on screen.
//
// The Configure choices are the left rail of Identify, restructured as
// switchable DETECTION ROUTES rather than peer tabs (root CLAUDE.md section 5,
// frontend/CLAUDE.md discipline rules). Smart detection is on by default and
// owns the scope controls; Local AI is off by default because handing the
// document to a model is the user's decision.
func checkConfigureRail(c *cdpClient, r *reporter, fx fixture) {
	r.step("The Configure rail is the two detection routes")

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

	r.assert("the rail is two route sections", got.Sections == 2,
		"2 .rail-section elements", fmt.Sprintf("%d, routes: %s", got.Sections, strings.Join(got.Routes, ", ")),
		"views/identifyrail.js RAIL_SECTIONS defines Smart detection and Local AI.")

	r.assert("the old tab strip is gone", got.RailTabs == 0,
		"0 [data-railtab] chips anywhere in the document", fmt.Sprintf("%d", got.RailTabs),
		"The rail switches sections on and off; it does not tab between them.")

	r.assert("Smart detection is on by default", boolIs(got.SmartOn, true),
		"the rail-smart route switch checked", describeBool(got.SmartOn),
		"Every Smart detection method defaults on, so the derived section state reads on.")

	r.assert("Local AI is off by default", boolIs(got.LocalOn, false),
		"the rail-local route switch unchecked", describeBool(got.LocalOn),
		"state.js settings.useAI defaults to false. Detecting Ollama ENABLES this switch, "+
			"it never flips it.")

	r.assert("every category checkbox is present", got.Categories == fx.CategoryCount,
		fmt.Sprintf("exactly %d .cat-toggle checkboxes", fx.CategoryCount),
		fmt.Sprintf("%d", got.Categories),
		"Every state.js ALL_CATEGORIES entry reaches the rail, and the rail invents none. "+
			"This is an equality, not a floor: with a floor, adding a category and leaving the "+
			"fixture behind keeps the harness green, which is a test reporting safety it no "+
			"longer provides.")

	r.assert("the category groups are folded by default",
		got.Categories > 0 && got.CategoriesWithSize == 0,
		"no category checkbox laid out until its group is opened",
		fmt.Sprintf("%d of %d have a height while nothing was clicked", got.CategoriesWithSize, got.Categories),
		"views/identifyrail.js seeds collapsedGroups with every cat-group id so the rail opens on the "+
			"route switches and the scope summary, not a wall of category lists.")

	r.assert("opening a category group lays out its checkboxes",
		got.Categories > 0 && got.CategoriesWithSizeAfterExpand == got.Categories,
		"every category checkbox laid out once its group is opened",
		fmt.Sprintf("%d of %d have a height after opening every group", got.CategoriesWithSizeAfterExpand, got.Categories),
		"A folded group is only useful if it opens: collapsibleGroup + wireGroups reveal the "+
			"checkboxes, and a folded-forever group would be a category the user cannot reach.")

	// The signal control is a tree hanging off the category row of the signal it
	// reads, and it is built from the frontend's lists, which the Go parity guard
	// holds to the engine's. One drill-down per signal, one master per drill-down: a
	// master over nothing, or a drill-down with no master, is a control that cannot
	// say what it does.
	r.assert("each signal has a drill-down on its own category row", len(got.SignalRows) == 1,
		"1 .signal-row (one implemented signal source)",
		fmt.Sprintf("%d: %v", len(got.SignalRows), got.SignalRows),
		"views/identifyrail.js hangs a signalDrillDown off the category row of every "+
			"state.js SIGNAL_SOURCES entry.")

	r.assert("every drill-down has its own master switch",
		len(got.SignalMasters) == len(got.SignalRows),
		"one .signal-master per drill-down",
		fmt.Sprintf("%d masters for %d drill-downs: %v", len(got.SignalMasters), len(got.SignalRows), got.SignalMasters),
		"The master is what saves switching a whole signal off one reading at a time.")

	// "On the row" is a claim about geometry: markup order proves nothing, since a
	// row narrower than its contents wraps the same markup onto two lines and the
	// rail is the narrowest column in the application.
	if line := got.SignalRowLine; line == nil {
		r.assert("the signal row renders its drill-down and help icon", false,
			".cat-label, .signal-drill and span.help in one .signal-row-head",
			"one of them is missing",
			"views/identifyrail.js signalCategoryRow passes the row, the button and the help "+
				"tooltip to ui.js signalDrillDown.")
	} else {
		r.assert("the drill-down and its help icon sit on the category row",
			boolIs(line.SameRow, true),
			"the label, the button and the icon at the same y (within 2px)",
			fmt.Sprintf("sameRow=%s (%s)", describeBool(line.SameRow), line.Widths),
			"style.css .signal-row-head is one flex line. A drill-down that wraps below its "+
				"own label no longer reads as belonging to it.")

		r.assert("they are ordered label, drill-down, help icon",
			boolIs(line.DrillIsAfterLabel, true) && boolIs(line.HelpIsAfterDrill, true),
			"the button after the label, the icon after the button",
			fmt.Sprintf("drillAfterLabel=%s, helpAfterDrill=%s",
				describeBool(line.DrillIsAfterLabel), describeBool(line.HelpIsAfterDrill)),
			"The icon explains what the button opens, so it follows the button.")

		r.assert("the row still fits the rail", boolIs(line.FitsTheRail, true),
			"no horizontal overflow in .signal-row-head",
			line.Widths,
			"The rail is the narrowest column in the application and the page body never "+
				"scrolls sideways (the fixed-height layout contract).")
	}

	// "Side by side" is a claim about geometry, so geometry is what answers it.
	// Markup order proves nothing here: a column-flex parent stacks the same
	// markup, which is exactly how these two spent two full rows on two words.
	if got.MethodPairRow == nil {
		r.assert("the two plain method switches render", false,
			"#smart-built-in and #smart-heuristic in the rail", "one of them is missing",
			"views/identifyrail.js smartMethods renders both inside .rail-toggle-pair.")
		return
	}
	pair := got.MethodPairRow

	r.assert("Built-in patterns and Heuristic discovery share one row",
		boolIs(pair.SameRow, true),
		"the two checkboxes at the same y (within 2px)",
		fmt.Sprintf("built-in at y=%d, heuristic at y=%d", pair.BuiltInTop, pair.HeuristicTop),
		"style.css .rail-toggle-pair is a two-column grid. The rail's height is its scarcest "+
			"resource and these are its shortest labels.")

	r.assert("they are ordered left to right, not overlapping",
		boolIs(pair.HeuristicIsToTheRight, true),
		"Heuristic discovery starting after Built-in patterns ends",
		describeBool(pair.HeuristicIsToTheRight),
		"Two switches at the same y that overlap are one unreadable control.")

	r.assert("halving the row did not truncate either label",
		boolIs(pair.LabelsFullyShown, true),
		"both .cat-label elements showing their whole text",
		fmt.Sprintf("%s (%s)", describeBool(pair.LabelsFullyShown), strings.Join(pair.LabelWidths, "; ")),
		"A pair of ellipses is worse than two rows. Below the rail's measure the pair stacks "+
			"instead, under the @media block beside the rule.")
}

// --- A value card's actions actually reach the store ------------------------

type valueCardResult struct {
	Error               string   `json:"error"`
	Cards               int      `json:"cards"`
	CardsWithIdentity   int      `json:"cardsWithIdentity"`
	InlineInputAppeared *bool    `json:"inlineInputAppeared"`
	Renamed             *bool    `json:"renamed"`
	RemovedOne          *bool    `json:"removedOne"`
	ValuesAfter         []string `json:"valuesAfter"`
}

// checkValueCardActions asserts a value card's controls CHANGE something.
//
// This is the layer that sees attribute lower-casing, and it is the only one
// that can. A card names the Value its handlers act on through `data-`
// attributes; a browser lower-cases attribute NAMES while a string test
// preserves them, so a camel-case `data-mainText` renders, matches every string
// assertion, and reaches the handler as an undefined `dataset.mainText`. Rename,
// remove, drop-a-spelling and merge then all silently do nothing, which is
// indistinguishable from a button that was never wired.
func checkValueCardActions(c *cdpClient, r *reporter) {
	r.step("A value card's actions reach the store")

	var got valueCardResult
	if err := c.eval("__uiProbes.valueCardActions()", &got); err != nil {
		r.assert("the value-card probe runs", false, "a rendered My values tab", err.Error(),
			"views/identifyworkspace.js valuesTab must render from a seeded values list.")
		return
	}
	if got.Error != "" {
		r.assert("the value-card probe runs", false, "value cards on the My values tab", got.Error,
			"views/identifyworkspace.js renders one .value-card per accepted Value.")
		return
	}

	r.assert("the seeded values render as cards", got.Cards == 2,
		"2 .value-card elements", fmt.Sprintf("%d", got.Cards),
		"The probe seeds two accepted Values, one per card.")

	r.assert("every card carries its own identity", got.Cards > 0 && got.CardsWithIdentity == got.Cards,
		"every card readable as dataset.category + dataset.mainText",
		fmt.Sprintf("%d of %d", got.CardsWithIdentity, got.Cards),
		"The card renders data-category and data-main-text. A camel-case data-mainText is "+
			"lower-cased by the parser, so dataset.mainText is undefined and every action on the "+
			"card resolves against it.")

	r.assert("clicking the name reveals an inline input", boolIs(got.InlineInputAppeared, true),
		"a .value-name-input in place of the name button", describeBool(got.InlineInputAppeared),
		"revealNameInput replaces the name button; native dialogs are banned.")

	r.assert("committing the input renames the Value", boolIs(got.Renamed, true),
		"the new name in state.values", describeBool(got.Renamed),
		"revealNameInput commits through renameValue, which needs the card's mainText.")

	r.assert("the card's remove control deletes the Value", boolIs(got.RemovedOne, true),
		"one fewer value in the store",
		fmt.Sprintf("%s, values now: %s", describeBool(got.RemovedOne), strings.Join(got.ValuesAfter, ", ")),
		"The .value-remove handler calls deleteValue(category, mainText) from the card's dataset.")
}

// --- The value card's geometry, and the scroll position it used to lose -----

type cardGeometryResult struct {
	Error          string `json:"error"`
	OverflowsChips *bool  `json:"overflowsChips"`
	ListScrolls    *bool  `json:"listScrolls"`
	Deleted        *bool  `json:"deleted"`
	Pending        *bool  `json:"pending"`
	HasWarningIcon *bool  `json:"hasWarningIcon"`

	HeightBefore    int `json:"heightBefore"`
	HeightAfterEdit int `json:"heightAfterEdit"`
	HeightRenamed   int `json:"heightRenamed"`
	HeightWarned    int `json:"heightWarned"`

	ScrollBefore    int `json:"scrollBefore"`
	ScrollAfterEdit int `json:"scrollAfterEdit"`
	ScrollRenamed   int `json:"scrollRenamed"`
	ScrollWarned    int `json:"scrollWarned"`

	CardHeight         int `json:"cardHeight"`
	ScrollBeforeDelete int `json:"scrollBeforeDelete"`
	ScrollAfterDelete  int `json:"scrollAfterDelete"`
}

// checkValueCardGeometry asserts a value card's HEIGHT does not depend on its
// data, and that the value list keeps its place through the actions that used to
// throw it.
//
// The reported symptom was the My values scrollbar jumping upward after editing a
// spelling, editing a name or deleting a card. The scroll preserver was not
// broken: it writes back a raw pixel offset, which is right only while the
// content is the same height. Editing a spelling sends the row back to pending,
// the chips are replaced by one line of text, the card shrinks, the browser
// CLAMPS the restored offset to the shorter scrollHeight, and the next repaint
// snapshots the clamped value. The position is then lost for good.
//
// Nothing cheaper can see this. The markup was correct at every step; only a
// renderer with a real scroll container can measure a height that changed and an
// offset that was clamped.
func checkValueCardGeometry(c *cdpClient, r *reporter) {
	r.step("A value card keeps its height, and the list keeps its place")

	var got cardGeometryResult
	if err := c.eval("__uiProbes.valueCardGeometry()", &got); err != nil {
		r.assert("the card-geometry probe runs", false,
			"a scrolling My values list", err.Error(),
			"views/identifyworkspace.js valuesTab must render one .value-card per seeded Value.")
		return
	}
	if got.Error != "" {
		r.assert("the card-geometry probe runs", false,
			"a My values list long enough to scroll", got.Error,
			"The probe seeds fifteen Values inside #identify-workspace .card-body.")
		return
	}

	r.assert("the measured card has more spellings than fit on its line",
		boolIs(got.OverflowsChips, true),
		"a \"+N more\" control on the card", describeBool(got.OverflowsChips),
		"The chip row spends a character budget (identifyworkspace.js SPELLING_PREVIEW_BUDGET) "+
			"and puts the rest behind \"+N more\". Without an overflow this check measures nothing.")

	r.assert("the popup's own list scrolls rather than growing the popup",
		boolIs(got.ListScrolls, true),
		"a .spelling-list taller than its box", describeBool(got.ListScrolls),
		"style.css .spelling-list caps its height and scrolls, so a Value with fifty "+
			"spellings is as reachable as one with two.")

	r.assert("a spelling was actually deleted, so the row went back to pending",
		boolIs(got.Deleted, true),
		"one spelling removed through the popup", describeBool(got.Deleted),
		"The popup's per-row Delete calls deleteVariant, which is the edit that used to "+
			"collapse the chip row.")

	r.assert("the card is the same height after a spelling edit",
		got.HeightBefore > 0 && got.HeightAfterEdit == got.HeightBefore,
		fmt.Sprintf("still %dpx", got.HeightBefore), fmt.Sprintf("%dpx", got.HeightAfterEdit),
		"style.css .spelling-row is one line, overflow:hidden, with a min-height, so a "+
			"pending expansion swaps the chips for a line of text INSIDE the row instead of "+
			"removing the row.")

	r.assert("the list keeps its scroll position across a spelling edit",
		got.ScrollBefore > 0 && got.ScrollAfterEdit == got.ScrollBefore,
		fmt.Sprintf("scrollTop still %d", got.ScrollBefore), fmt.Sprintf("%d", got.ScrollAfterEdit),
		"scroll.js restores a raw pixel offset, which the browser clamps when the content "+
			"got shorter. The card holding its height is what makes the restore exact.")

	r.assert("renaming the value sends its spellings back to pending",
		boolIs(got.Pending, true),
		"derivedSpellings null on the renamed Value", describeBool(got.Pending),
		"This is the case with the most teeth: with nothing settled to draw, the chip row "+
			"falls back to one line of text. Without it the probe never exercises the collapse "+
			"the owner reported, because deleting a spelling CURATES and leaves the list settled.")

	r.assert("the card is the same height while its spellings are pending",
		got.HeightRenamed > 0 && got.HeightRenamed == got.HeightAfterEdit,
		fmt.Sprintf("still %dpx", got.HeightAfterEdit), fmt.Sprintf("%dpx", got.HeightRenamed),
		"The pending line renders INSIDE the chip row, in place of the chips, never as a row "+
			"under it. A row that appears while Go answers is the collapse.")

	r.assert("the list keeps its scroll position while the spellings are pending",
		got.ScrollRenamed == got.ScrollAfterEdit,
		fmt.Sprintf("scrollTop still %d", got.ScrollAfterEdit), fmt.Sprintf("%d", got.ScrollRenamed),
		"")

	r.assert("a warning renders as an icon on the card", boolIs(got.HasWarningIcon, true),
		"a .warnpop on the card", describeBool(got.HasWarningIcon),
		"ui.js warningPopover: the warning text and its actions live in a hover surface, "+
			"not in a row of the card.")

	r.assert("the card is the same height once a warning appears",
		got.HeightWarned > 0 && got.HeightWarned == got.HeightRenamed,
		fmt.Sprintf("still %dpx", got.HeightRenamed), fmt.Sprintf("%dpx", got.HeightWarned),
		"A warning rendered as a row makes the card taller when it arrives and shorter when "+
			"it clears, which moves every card below it.")

	r.assert("the list keeps its scroll position when a warning appears",
		got.ScrollWarned == got.ScrollRenamed,
		fmt.Sprintf("scrollTop still %d", got.ScrollRenamed), fmt.Sprintf("%d", got.ScrollWarned), "")

	// Deleting a card genuinely shortens the list, so the offset MAY move. What
	// must not happen is a clamp to the top, which is what the user reported.
	drift := got.ScrollBeforeDelete - got.ScrollAfterDelete
	if drift < 0 {
		drift = -drift
	}
	r.assert("deleting a card moves the list by at most one card",
		got.CardHeight > 0 && drift <= got.CardHeight,
		fmt.Sprintf("scrollTop within %dpx of %d", got.CardHeight, got.ScrollBeforeDelete),
		fmt.Sprintf("%d (moved %dpx)", got.ScrollAfterDelete, drift),
		"The list really is shorter by one card, so a small clamp is expected. Jumping to the "+
			"top is not: that is the raw offset being clamped against content that shrank by more "+
			"than the deleted card.")
}

// --- The spellings popup ----------------------------------------------------

type spellingsPopupResult struct {
	Error          string   `json:"error"`
	MoreLabel      string   `json:"moreLabel"`
	Box            rect     `json:"box"`
	Painted        *bool    `json:"painted"`
	OnScreen       *bool    `json:"onScreen"`
	ListScrolls    *bool    `json:"listScrolls"`
	ListScrolled   *bool    `json:"listScrolled"`
	OnValueAfter   *bool    `json:"onValueAfter"`
	ChipsAfter     []string `json:"chipsAfter"`
	PopupHeight    int      `json:"popupHeight"`
	ViewportHeight int      `json:"viewportHeight"`
}

// checkSpellingsPopup asserts the popup opens, is genuinely on screen, scrolls
// inside itself, and updates the card behind it live.
//
// The card shows only the spellings that fit one line, so the popup is the only
// way to reach the rest. A surface that is in the DOM but clipped, or one that
// grows past the window because its list does not scroll, makes those spellings
// unreachable while every string test stays green.
func checkSpellingsPopup(c *cdpClient, r *reporter) {
	r.step("The spellings popup opens, scrolls, and updates the card live")

	var got spellingsPopupResult
	if err := c.eval("__uiProbes.spellingsPopup()", &got); err != nil {
		r.assert("the spellings-popup probe runs", false,
			"a value card with overflowing spellings", err.Error(),
			"views/identifyworkspace.js renders \"+N more\" when the chip budget overflows.")
		return
	}
	if got.Error != "" {
		r.assert("the spellings-popup probe runs", false,
			"a .spellings-popup opened from \"+N more\"", got.Error,
			"The \"+N more\" handler calls openSpellingsPopup, which sets the view state the "+
				"workspace renders spellingsPopupHTML from.")
		return
	}

	r.assert("\"+N more\" says how many are hidden", strings.HasPrefix(got.MoreLabel, "+"),
		"a label of the form \"+N more\"", got.MoreLabel,
		"copy.js WORKSPACE.moreSpellings(n). A control that does not say how much is behind it "+
			"is a control nobody opens.")

	r.assert("the popup is painted, not clipped away", boolIs(got.Painted, true),
		"elementFromPoint at the popup's centre returning the popup itself",
		fmt.Sprintf("%s at %s", describeBool(got.Painted), got.Box),
		"The rect of a clipped element is still a full-size rect, so this is the check with "+
			"teeth. The popup is rendered OUTSIDE the scrolling card body for exactly this reason.")

	r.assert("the popup is on screen", boolIs(got.OnScreen, true),
		fmt.Sprintf("a box inside the %dpx viewport", got.ViewportHeight),
		fmt.Sprintf("%s at %s", describeBool(got.OnScreen), got.Box), "")

	r.assert("the popup fits the window", got.PopupHeight > 0 && got.PopupHeight <= got.ViewportHeight,
		fmt.Sprintf("a popup no taller than %dpx", got.ViewportHeight),
		fmt.Sprintf("%dpx", got.PopupHeight),
		"style.css .spellings-popup caps its height and the list inside it scrolls, so a Value "+
			"with fifty spellings does not push the buttons off the bottom of the screen.")

	r.assert("the list scrolls INSIDE the popup", boolIs(got.ListScrolls, true),
		"a .spelling-list taller than its box", describeBool(got.ListScrolls), "")

	r.assert("and it really scrolls when scrolled", boolIs(got.ListScrolled, true),
		"a non-zero scrollTop on the .spelling-list", describeBool(got.ListScrolled),
		"An overflowing element with overflow:hidden reports the same scrollHeight and moves "+
			"nowhere, so the two checks are separate.")

	r.assert("adding in the popup reaches the Value", boolIs(got.OnValueAfter, true),
		"the new spelling in state.values", describeBool(got.OnValueAfter),
		"The popup's Add calls addSpelling(category, mainText, value) from the open popup's own "+
			"identity.")

	r.assert("and the compact card behind it updates on the same repaint",
		contains(got.ChipsAfter, "Zzz Popup Spelling"),
		"the new spelling among the card's chips",
		fmt.Sprintf("[%s]", strings.Join(got.ChipsAfter, ", ")),
		"The popup and the card read the same store, which is what makes the edit live: there "+
			"is no OK button and nothing to apply.")
}

// contains reports whether a string slice holds a value. The popup check needs
// it and the standard library has no generic helper this file already imports.
func contains(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}

// --- The Configure panel's height and its help tooltips ---------------------

type panelFitResult struct {
	Error           string `json:"error"`
	ScrollHeight    int    `json:"scrollHeight"`
	ClientHeight    int    `json:"clientHeight"`
	PageOverflows   *bool  `json:"pageOverflows"`
	FootReachable   *bool  `json:"footReachable"`
	ProseParagraphs int    `json:"proseParagraphs"`
	ProseHeight     int    `json:"proseHeight"`
	HelpTooltips    int    `json:"helpTooltips"`
}

// checkConfigurePanelFit asserts the Configure panel FITS, and that its
// explanations are tooltips rather than prose.
//
// The panel used to carry a paragraph under every control. Read once they are
// useful; read on every visit they are what pushes the panel past the window and
// buries the controls that are actually in use. The explanations moved into help
// tooltips, and this is the measurement that keeps them there: a string test can
// count paragraphs but cannot see whether the result fits.
func checkConfigurePanelFit(c *cdpClient, r *reporter) {
	r.step("The Configure panel fits, and explains itself on demand")

	var got panelFitResult
	if err := c.eval("__uiProbes.configurePanelFit()", &got); err != nil {
		r.assert("the panel-fit probe runs", false, "a rendered Identify rail", err.Error(),
			"views/identifyrail.js must render the rail from a seeded Identify state.")
		return
	}
	if got.Error != "" {
		r.assert("the panel-fit probe runs", false, "#identify-rail on the Identify screen",
			got.Error, "views/identify.js renders the rail and hands it to renderIdentifyRail.")
		return
	}

	// Measured in PIXELS, not counted by class: a paragraph given a different
	// class would pass a class count and still occupy the panel.
	r.assert("the panel spends no vertical space on prose",
		got.ProseParagraphs == 0 && got.ProseHeight == 0,
		"0 static paragraphs, 0px of them",
		fmt.Sprintf("%d paragraph(s), %dpx", got.ProseParagraphs, got.ProseHeight),
		"An explanation belongs in a help tooltip. Live read-outs carry .rail-readout and are "+
			"excluded, so this measures prose only.")

	r.assert("the explanations are still reachable", got.HelpTooltips >= 8,
		"at least 8 help tooltips in the rail", fmt.Sprintf("%d", got.HelpTooltips),
		"Removing the paragraphs must MOVE the explanations, not delete them: ui.js helpTooltip "+
			"is where each one now lives.")

	// The panel is allowed to scroll: it holds twenty-four category checkboxes and
	// the window is what it is. Two things are not allowed, and both are
	// visible-only properties a string test cannot reach.
	r.assert("the panel scrolls inside its own body, not the page",
		boolIs(got.PageOverflows, false),
		"a document no taller than the window", describeBool(got.PageOverflows),
		"The fixed-height layout contract: scrolling happens inside a card body and nowhere "+
			"else, so every link from #view down needs min-height: 0.")

	r.assert("the foot of the panel is reachable", boolIs(got.FootReachable, true),
		"the last block painted after scrolling the panel to its end",
		describeBool(got.FootReachable),
		"Prose under every control is what put the controls at the foot out of reach. Scrolling "+
			"to the end must land on them.")
}

// --- Opening a signal's drill-down shows its readings ------------------------

type signalDerivationResult struct {
	Error                    string `json:"error"`
	Source                   string `json:"source"`
	CollapsedRows            int    `json:"collapsedRows"`
	RowLaidOut               *bool  `json:"rowLaidOut"`
	CollapsedVisible         int    `json:"collapsedVisible"`
	OpenedVisible            int    `json:"openedVisible"`
	Derivation               string `json:"derivation"`
	ReadingWentOff           *bool  `json:"readingWentOff"`
	OtherReadingsStillOn     *bool  `json:"otherReadingsStillOn"`
	GroupStayedOpenAfterTick *bool  `json:"groupStayedOpenAfterTicking"`
	MasterAfterTick          *bool  `json:"masterAfterTick"`
}

// checkSignalDerivations asserts that opening a signal's drill-down REVEALS its
// readings and that each is independently switchable.
//
// Collapsed, the readings are in the DOM at zero height: present to a string test
// and absent to the user. So "opening it shows them" is a claim about geometry, and
// only this layer can answer it. It also drives the two controls that must stay
// separate, since two jobs on one element is how a setting gets flipped by
// accident: the button opens without ticking, and a reading ticks without closing.
func checkSignalDerivations(c *cdpClient, r *reporter) {
	r.step("Opening a signal's drill-down reveals its readings, each switchable on its own")

	var got signalDerivationResult
	if err := c.eval("__uiProbes.signalDerivations()", &got); err != nil {
		r.assert("the signal-derivation probe runs", false, "a rendered signal drill-down",
			err.Error(),
			"views/identifyrail.js signalCategoryRow renders ui.js signalDrillDown.")
		return
	}
	if got.Error != "" {
		r.assert("the signal-derivation probe runs", false, ".signal-row in the Identify rail",
			got.Error, "Every state.js SIGNAL_SOURCES entry is a category with a row of its own.")
		return
	}

	r.assert("a collapsed drill-down costs no vertical space",
		got.CollapsedRows > 0 && got.CollapsedVisible == 0 && boolIs(got.RowLaidOut, true),
		fmt.Sprintf("the category row laid out, its %d readings not", got.CollapsedRows),
		fmt.Sprintf("row laid out=%s, %d readings, %d with a height",
			describeBool(got.RowLaidOut), got.CollapsedRows, got.CollapsedVisible),
		"Collapsed the readings cost no row at all. That trade is what keeps the panel short "+
			"as signals and readings are added, and a string test cannot tell the two states apart.")

	r.assert("opening it reveals every reading",
		got.OpenedVisible == got.CollapsedRows,
		fmt.Sprintf("all %d readings laid out with a height after one click", got.CollapsedRows),
		fmt.Sprintf("%d of %d", got.OpenedVisible, got.CollapsedRows),
		"A checkbox in the DOM at zero height is not something the user can tick.")

	r.assert("ticking a reading reaches the store", boolIs(got.ReadingWentOff, true),
		fmt.Sprintf("%s stored as off", got.Derivation), describeBool(got.ReadingWentOff),
		"state.js setSignalDerivation writes the leaf, and that is what the next detection "+
			"run reads. A checkbox that unticks itself proves nothing about the run.")

	r.assert("the other readings are untouched", boolIs(got.OtherReadingsStillOn, true),
		"every other reading of that signal still on", describeBool(got.OtherReadingsStillOn),
		"The independence is the whole point of the per-reading switches; the engine "+
			"honours each on its own (backend/engine/signaldiscovery.go).")

	r.assert("ticking a reading does not close the drill-down",
		boolIs(got.GroupStayedOpenAfterTick, true),
		"the readings still laid out after the tick", describeBool(got.GroupStayedOpenAfterTick),
		"The checkbox stops the click reaching anything else. A drill-down that closes as you "+
			"tick it makes switching two readings a four-click job.")

	r.assert("the master stays on while any reading is on",
		boolIs(got.MasterAfterTick, true),
		"the drill-down's master still checked with one of two readings off",
		describeBool(got.MasterAfterTick),
		"The master is DERIVED (state.js signalSourceOn): on when any reading is on. A "+
			"master that dropped with the first reading would misreport what the run does.")
}

// --- The Discovery strictness block's measure and inset ---------------------

type strictnessResult struct {
	Error                  string `json:"error"`
	SelectWidth            int    `json:"selectWidth"`
	WidestOption           int    `json:"widestOption"`
	WidestText             string `json:"widestText"`
	SelectFitsWidestOption *bool  `json:"selectFitsWidestOption"`
	NestedLabelLeft        *int   `json:"nestedLabelLeft"`
	SectionLabelLeft       *int   `json:"sectionLabelLeft"`
	FieldLabelLefts        []int  `json:"fieldLabelLefts"`
	Labels                 []struct {
		Text       string `json:"text"`
		Height     int    `json:"height"`
		LineHeight int    `json:"lineHeight"`
	} `json:"labels"`
	RailOverflowsX *bool `json:"railOverflowsX"`
}

// checkStrictnessFields asserts the Discovery strictness block is readable and
// aligned.
//
// Two reported defects, neither visible to a string test. `.rail-field` gave the
// control column 6rem, narrower than the strictness select's own longest option,
// so the control read as a truncated stub. And `.cgroup-body` carries no padding
// of its own, so a nested subgroup's fields sat flush against its border while
// every label above them was inset. Only pixels can say either.
func checkStrictnessFields(c *cdpClient, r *reporter) {
	r.step("The Discovery strictness fields are readable and aligned")

	var got strictnessResult
	if err := c.eval("__uiProbes.strictnessFields()", &got); err != nil {
		r.assert("the strictness probe runs", false, "a rendered strictness block", err.Error(),
			"views/identifyrail.js smartTuning renders it as a .rail-subgroup inside Smart detection.")
		return
	}
	if got.Error != "" {
		r.assert("the strictness probe runs", false, "the Discovery strictness block in the rail",
			got.Error, "views/identifyrail.js smartSection nests it under Smart detection.")
		return
	}

	r.assert("the strictness select shows its longest option in full",
		boolIs(got.SelectFitsWidestOption, true),
		fmt.Sprintf("a box at least as wide as %q (%dpx)", got.WidestText, got.WidestOption),
		fmt.Sprintf("%dpx of box for %dpx of text", got.SelectWidth, got.WidestOption),
		"style.css .rail-field sizes the control column. A column narrower than the widest "+
			"option truncates it to an unreadable stub, and no string test can see that.")

	if got.NestedLabelLeft == nil || got.SectionLabelLeft == nil {
		r.assert("the strictness block has a label to align", false,
			"a .rail-field-label in the subgroup and a .section-label above it",
			"one of them is missing",
			"The comparison needs both; if the rail's markup changed, update the probe.")
		return
	}

	// Indented, not flush and not hanging left: a nested field sits at least as
	// far in as the labels of the section that contains it.
	r.assert("the nested fields are inset like the labels above them",
		*got.NestedLabelLeft >= *got.SectionLabelLeft,
		fmt.Sprintf("a nested label at x >= %dpx (the section labels' own inset)", *got.SectionLabelLeft),
		fmt.Sprintf("x = %dpx", *got.NestedLabelLeft),
		"style.css .rail-subgroup > .cgroup-body carries the inset. .cgroup-body has no padding "+
			"of its own, so a subgroup that matches no such rule sits flush against its border.")

	// One inset for the whole block: a single field left behind reads as a
	// misprint rather than as a hierarchy.
	sameInset := len(got.FieldLabelLefts) > 0
	for _, left := range got.FieldLabelLefts {
		if left != got.FieldLabelLefts[0] {
			sameInset = false
		}
	}
	r.assert("every field in the block shares one inset", sameInset,
		"all .rail-field-label left offsets equal",
		fmt.Sprintf("%v", got.FieldLabelLefts),
		"They are one form. A field at a different offset reads as a mistake.")

	// The other half of widening the control: the label column pays for it. A label
	// on two lines is the cost of taking "make the dropdown wider" too far, and
	// nothing but a measurement can see it.
	var wrapped []string
	for _, l := range got.Labels {
		if l.LineHeight > 0 && l.Height > l.LineHeight*3/2 {
			wrapped = append(wrapped, fmt.Sprintf("%q (%dpx over a %dpx line)", l.Text, l.Height, l.LineHeight))
		}
	}
	r.assert("no field label wraps to a second line", len(wrapped) == 0,
		"every .rail-field-label on one line",
		fmt.Sprintf("%d wrapped: %s", len(wrapped), strings.Join(wrapped, ", ")),
		"style.css .rail-field splits the row between the label and the control. Widening the "+
			"control column narrows the label's, and the labels are where that trade shows first.")

	r.assert("widening the control did not widen the rail", boolIs(got.RailOverflowsX, false),
		"a rail no wider than its column", describeBool(got.RailOverflowsX),
		"The fixed-height layout contract: wide content scrolls inside its own container and "+
			"never widens the page.")
}

// helpTrigger is the icon the user has to FIND before any of the behaviour below
// matters.
type helpTrigger struct {
	Width       int   `json:"width"`
	Height      int   `json:"height"`
	HasGlyph    *bool `json:"hasGlyph"`
	GlyphWidth  int   `json:"glyphWidth"`
	GlyphHeight int   `json:"glyphHeight"`
}

type helpTooltipResult struct {
	Error             string      `json:"error"`
	Trigger           helpTrigger `json:"trigger"`
	ClosedVisible     *bool       `json:"closedVisible"`
	OpenedOnHover     *bool       `json:"openedOnHover"`
	OnScreen          *bool       `json:"onScreen"`
	NotClipped        *bool       `json:"notClipped"`
	OverflowsScroller *bool       `json:"overflowsScroller"`
	ClosedOnLeave     *bool       `json:"closedOnLeave"`
	OpenedOnFocus     *bool       `json:"openedOnFocus"`
	ClosedOnEscape    *bool       `json:"closedOnEscape"`
}

// checkHelpTooltip asserts a help tooltip opens, is PAINTED rather than clipped,
// and closes again, through both the pointer and the keyboard.
//
// A bubble positioned inside the rail's `overflow: auto` body is cut off at the
// container's edge, and no assertion over an HTML string can see that. It is the
// same class of failure as the Compare-pane tooltip this layer already covers, so
// it gets the same treatment: a real pointer event, then a hit test at the
// bubble's own coordinates.
func checkHelpTooltip(c *cdpClient, r *reporter) {
	r.step("A Configure help tooltip opens, is painted, and closes")

	var got helpTooltipResult
	if err := c.eval("__uiProbes.helpTooltipVisibility()", &got); err != nil {
		r.assert("the help-tooltip probe runs", false, "a rendered help tooltip", err.Error(),
			"views/identifyrail.js renders ui.js helpTooltip beside each explained label.")
		return
	}
	if got.Error != "" {
		r.assert("the help-tooltip probe runs", false, "a help tooltip in the Identify rail",
			got.Error, "Every explained label in the rail carries one.")
		return
	}

	// Before any behaviour: is there anything on screen to hover? Every help
	// trigger in the application was an invisible hit area, because ui.js icon()
	// returns the empty string for a name absent from ICONS and helpTooltip asks
	// for "info". The mechanism below all worked; there was no glyph.
	r.assert("the help trigger has a glyph in it", boolIs(got.Trigger.HasGlyph, true),
		"an <svg> inside button.help-icon", describeBool(got.Trigger.HasGlyph),
		"ui.js helpTooltip renders icon(\"info\"), and icon() returns the EMPTY STRING for a "+
			"name absent from frontend/icons.js ICONS. icon_parity_test.go is the cheap guard; "+
			"this is the one that sees the result.")

	r.assert("the trigger is big enough to aim at",
		got.Trigger.Width >= 14 && got.Trigger.Height >= 14,
		"a trigger at least 14x14 CSS pixels",
		fmt.Sprintf("%dx%d", got.Trigger.Width, got.Trigger.Height),
		"style.css .help-icon sizes it; a trigger smaller than this is a target nobody hits.")

	r.assert("the glyph is painted at a readable size",
		got.Trigger.GlyphWidth >= 10 && got.Trigger.GlyphHeight >= 10,
		"a glyph at least 10x10 CSS pixels",
		fmt.Sprintf("%dx%d", got.Trigger.GlyphWidth, got.Trigger.GlyphHeight),
		"An svg present in the DOM at zero size is the same invisible control with extra markup.")

	r.assert("the bubble is hidden until asked for", boolIs(got.ClosedVisible, false),
		"a zero-height bubble before any interaction", describeBool(got.ClosedVisible),
		"An always-visible bubble is the paragraph the tooltip replaced, with extra steps.")

	r.assert("hover opens it", boolIs(got.OpenedOnHover, true),
		"a painted bubble after pointerenter", describeBool(got.OpenedOnHover),
		"ui.js wireHelpTooltips sets data-open on pointerenter; style.css reveals it.")

	r.assert("the bubble is on screen", boolIs(got.OnScreen, true),
		"the whole bubble inside the viewport", describeBool(got.OnScreen),
		"A bubble that opens off the edge of the window is a bubble nobody reads.")

	r.assert("the bubble is painted, not clipped by the scrolling panel",
		boolIs(got.NotClipped, true),
		"the bubble itself under a hit test at its own coordinates",
		describeBool(got.NotClipped),
		"This is the reason the bubble is positioned outside the rail's clipping context. An "+
			"absolutely positioned bubble inside an overflow:auto ancestor is cut at the "+
			"container's edge, and only a hit test can see it.")

	r.assert("leaving closes it", boolIs(got.ClosedOnLeave, true),
		"a zero-height bubble after pointerleave", describeBool(got.ClosedOnLeave), "")

	r.assert("keyboard focus opens it too", boolIs(got.OpenedOnFocus, true),
		"a painted bubble after focusin", describeBool(got.OpenedOnFocus),
		"An explanation only a pointer can reach is one half the users never get.")

	r.assert("Escape closes it", boolIs(got.ClosedOnEscape, true),
		"a zero-height bubble after Escape", describeBool(got.ClosedOnEscape),
		"A tooltip the keyboard can open and not dismiss is a keyboard trap.")
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

// compareSearchResult mirrors the compareSearch probe's payload: one entry per
// pane, because each pane now carries its own search bar.
type compareSearchResult struct {
	Error      string                      `json:"error"`
	Needle     string                      `json:"needle"`
	Panes      map[string]paneSearchResult `json:"panes"`
	Typing     *typingResult               `json:"typing"`
	StepScroll *stepScrollResult           `json:"stepScroll"`
}

// typingResult is the focus-retention outcome: a word typed one character at a
// time, with a pause past the debounce between each so the repaint lands
// mid-word. A box that loses focus there takes exactly one character.
type typingResult struct {
	KeptFocus    bool   `json:"keptFocus"`
	FocusedAfter bool   `json:"focusedAfter"`
	TypedValue   string `json:"typedValue"`
}

// stepScrollResult is the move-to-next-match outcome: after stepping forward to
// a hit well below the fold, whether that hit ended up inside the pane's box.
type stepScrollResult struct {
	Steps      int   `json:"steps"`
	HasActive  bool  `json:"hasActive"`
	InsidePane bool  `json:"insidePane"`
	ActiveRect *rect `json:"activeRect"`
	PaneRect   *rect `json:"paneRect"`
}

// paneSearchResult is one pane's search outcome.
type paneSearchResult struct {
	Hits        int            `json:"hits"`
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
	r.step("Each Compare pane's own search bar finds text and shows the active hit")

	var got compareSearchResult
	if err := c.eval("__uiProbes.compareSearch()", &got); err != nil {
		r.assert("each pane's search box renders and accepts a needle", false,
			"#compare-search-original and #compare-search-anonymised in the pane captions", err.Error(),
			"views/anonymise.js paneCaption renders a paneSearchControls bar aligned right in each caption.")
		return
	}
	if got.Error != "" {
		r.assert("each pane's search box renders and accepts a needle", false,
			"#compare-search-original and #compare-search-anonymised in the pane captions", got.Error,
			"views/anonymise.js compareCard builds each pane with paneCaption, which carries its own bar.")
		return
	}

	// Each pane is asserted the same way against its own bar: the two searches
	// are independent, so neither may borrow the other's hit or active state.
	for _, pane := range []string{"original", "anonymised"} {
		p := got.Panes[pane]

		r.assert(fmt.Sprintf("the needle is found in the %s pane", pane), p.Hits > 0,
			fmt.Sprintf("at least one .find-hit in #%s-pane for %q", pane, got.Needle),
			fmt.Sprintf("%d hits", p.Hits),
			"the pane renders its hits from its own paneWalk (renderPlainWithHits for the "+
				"original, renderHighlighted's search argument for the anonymised).")

		r.assert(fmt.Sprintf("the %s pane has an active hit", pane), p.HasActive,
			"a .find-hit.active inside the pane", "no active hit was rendered",
			"paneWalk resolves this pane's active index; the caption bar and the pane read the same walk.")

		r.assert(fmt.Sprintf("the %s pane's readout says the count and total", pane), p.Readout != "",
			"a readout naming the count and the total", "an empty readout",
			"copy.js ANONYMISE.searchCount(index, total).")

		r.assert(fmt.Sprintf("the %s pane's navigation buttons are live when there are hits", pane),
			p.NextEnabled && p.PrevEnabled,
			"next and previous enabled",
			fmt.Sprintf("next enabled: %t, previous enabled: %t", p.NextEnabled, p.PrevEnabled),
			"paneSearchControls disables them only when the pane's walk is empty, and gives them a "+
				"title saying why, so a greyed control is never mute.")

		if p.Visible == nil {
			r.assert(fmt.Sprintf("the %s pane's active hit sits inside the pane", pane), false,
				"the active hit inside the .pane-body", "the active hit has no .pane-body ancestor",
				"The pane renders its hits inside its own .pane-body; a hit outside it means the "+
					"renderer emitted markup somewhere unexpected.")
			continue
		}

		r.assert(fmt.Sprintf("the %s pane's active hit has a size", pane), p.Visible.HasSize,
			"a rendered span with width and height",
			p.Visible.ActiveRect.String(),
			"An empty hit span means the slice offsets and the text disagree.")

		// The check this layer exists for: a hit the pane's own overflow scrolled
		// out of sight is a hit the user cannot see, whatever the DOM says.
		r.assert(fmt.Sprintf("the %s pane's active hit is visible INSIDE the pane, not scrolled out of sight", pane),
			p.Visible.InsidePane,
			"the active hit's rect within its pane's rect",
			fmt.Sprintf("hit %s, pane %s", p.Visible.ActiveRect, p.Visible.PaneRect),
			"views/anonymise.js scrollToActiveHit runs per pane after the paint and only when that "+
				"pane's active index changed, so it does not fight scroll.js restoring the offset.")

		r.assert(fmt.Sprintf("the %s pane's active hit is on screen", pane), p.Visible.InViewport,
			"the active hit's rect within the viewport",
			fmt.Sprintf("hit %s", p.Visible.ActiveRect),
			"scrollIntoView({block: \"center\"}) on the pane's .find-hit.active.")
	}

	// Typing must not lose focus. Each keystroke schedules a debounced setState
	// that rewrites the whole shell and replaces the search input; if the refocus
	// aims at the old, detached field, the box takes exactly one character.
	if got.Typing == nil {
		r.assert("the search box keeps focus while typing", false,
			"a typing result from the probe", "the probe returned none",
			"views/anonymise.js wirePaneSearch re-queries the LIVE document for the field after the repaint.")
	} else {
		r.assert("the search box stays focused through a word typed with pauses", got.Typing.KeptFocus,
			"the field is document.activeElement after every keystroke",
			fmt.Sprintf("keptFocus=%t, focusedAfter=%t, value=%q",
				got.Typing.KeptFocus, got.Typing.FocusedAfter, got.Typing.TypedValue),
			"The debounced repaint replaces the input; wirePaneSearch must refocus the FRESH field "+
				"(container.ownerDocument.getElementById), not the detached one this closure captured.")
	}

	// Moving to the next match must scroll it into view. scrollToActiveHit runs
	// during the render, but scroll.js restores the pane's previous offset AFTER
	// the render, so the scroll is deferred a frame to outlive that restore.
	if got.StepScroll == nil {
		r.assert("stepping to the next match scrolls it into view", false,
			"a stepScroll result from the probe", "the probe returned none",
			"views/anonymise.js scrollToActiveHit defers scrollIntoView past scroll.js restoreScrollPositions.")
	} else {
		r.assert("stepping forward keeps finding an active hit", got.StepScroll.HasActive,
			"an active hit after stepping through the matches", "no active hit",
			"stepSearch wraps the index; paneWalk always resolves an active hit when there are any.")
		r.assert("the match reached by stepping down is scrolled INSIDE the pane, not left below the fold",
			got.StepScroll.InsidePane,
			"the active hit's rect within its pane's rect after stepping",
			fmt.Sprintf("after %d steps: hit %v, pane %v", got.StepScroll.Steps,
				got.StepScroll.ActiveRect, got.StepScroll.PaneRect),
			"scrollToActiveHit runs scrollIntoView on the next frame, so it wins over scroll.js "+
				"restoring the pre-step offset; without the deferral the restore drags the pane back.")
	}
}

// selectionPanelResult mirrors the selectionPanel probe's payload.
type selectionPanelResult struct {
	Error                   string   `json:"error"`
	SelectedText            string   `json:"selectedText"`
	InsideCompare           bool     `json:"insideCompare"`
	InViewport              bool     `json:"inViewport"`
	HasSize                 bool     `json:"hasSize"`
	OptionValues            []string `json:"optionValues"`
	SelectedValue           string   `json:"selectedValue"`
	HasDatalist             bool     `json:"hasDatalist"`
	SurvivedFirstKeystroke  bool     `json:"survivedFirstKeystroke"`
	SurvivedSecondKeystroke bool     `json:"survivedSecondKeystroke"`
	FieldValue              string   `json:"fieldValue"`
	Picks                   []string `json:"picks"`
	CardRect                rect     `json:"cardRect"`
	CompareRect             rect     `json:"compareRect"`
}

// declarableCategories is the set of types a user may declare a Value under,
// mirroring engine.AllValueCategories minus custom_patterns (which is declarative
// and reached through the Patterns tab, not this dropdown). ../../
// category_parity_test.go holds the engine and the store to each other; this holds
// the rendered dropdown to the same list.
var declarableCategories = map[string]bool{
	"entity_names": true, "project_names": true, "identifier_names": true,
	"person_names": true, "product_names": true, "brand_names": true,
	"other_names": true,
}

// checkSelectionPanel asserts the Compare pane's REPLACE SELECTION panel opens
// against a real text selection, that its type dropdown emits CATEGORY KEYS, and
// that its spelling field survives being typed into.
//
// This is the only layer that renders a native <select> and the only one that can
// open the panel the way a mouse drag does. Both defects it guards against left
// the markup correct and the feature unusable: a dropdown whose option values were
// "person_names,Person names" produced Values the engine dropped before
// validation, and a field whose input handler repainted the view was destroyed
// mid-word and accepted exactly one letter.
func checkSelectionPanel(c *cdpClient, r *reporter) {
	r.step("The Compare selection panel declares a Value the engine can apply")

	var got selectionPanelResult
	if err := c.eval("__uiProbes.selectionPanel()", &got); err != nil {
		r.assert("selecting text in the anonymised pane opens the panel", false,
			"#selection-card beside the selection", err.Error(),
			"views/anonymise.js wireTextSelection listens for mouseup on .pane-body.")
		return
	}
	if got.Error != "" {
		r.assert("selecting text in the anonymised pane opens the panel", false,
			"#selection-card beside the selection", got.Error,
			"views/anonymise.js wireTextSelection listens for mouseup on .pane-body.")
		return
	}

	r.assert("the panel names the text that was selected", got.SelectedText != "",
		"the selected run echoed in .selection-text", "an empty .selection-text",
		"views/anonymise.js selectionPanel renders the selection's own text, escaped.")

	r.assert("the panel is inside the Compare card", got.InsideCompare,
		"the panel's rect within #compare-card",
		fmt.Sprintf("panel %s, card %s", got.CardRect, got.CompareRect),
		"wireTextSelection clamps x to the card's bounds, the same clamp the mark tooltip makes.")
	r.assert("the panel is on screen", got.InViewport,
		"the panel's rect within the viewport", "the panel is off screen",
		"SELECTION_PANEL_WIDTH is the clamp's half-width; the panel is translate(-50%, -100%).")
	r.assert("the panel is painted", got.HasSize,
		"a panel with a width and a height", "a zero-size panel",
		".selection-card in style.css sets its width and padding.")

	// The type list: every option value is a category the engine knows.
	bad := []string{}
	for _, v := range got.OptionValues {
		if !declarableCategories[v] {
			bad = append(bad, v)
		}
	}
	r.assert("the type list offers only real category keys", len(got.OptionValues) > 0 && len(bad) == 0,
		"one option per declarable category, each value a category key",
		fmt.Sprintf("%d option(s), not a category: %v", len(got.OptionValues), bad),
		"views/anonymise.js selectionStageFields uses the shared categorySelect builder; a "+
			"hand-rolled copy read the [key, label] pair list as a list of strings.")
	r.assert("a type is pre-selected", declarableCategories[got.SelectedValue],
		"the panel's current type selected in the list",
		fmt.Sprintf("selected value %q", got.SelectedValue),
		"categorySelect marks the option whose KEY equals the selected category.")

	// The spelling target: the field is typeable and it suggests.
	r.assert("no native datalist is used for the suggestions", !got.HasDatalist,
		"real pick buttons", "a <datalist> in the page",
		"a platform popup is destroyed by every repaint and is empty on the render the user "+
			"starts typing into, because valueAutocomplete with an empty query returns nothing.")
	r.assert("the field survives the first keystroke", got.SurvivedFirstKeystroke,
		"the same input element still in the DOM", "the element was replaced",
		"the input handler patches the suggestion list in place and never calls setState.")
	r.assert("the field survives the second keystroke", got.SurvivedSecondKeystroke,
		"the same input element still in the DOM", "the element was replaced",
		"this is the one-letter symptom: a repaint destroys the element the next keystroke needs.")
	r.assert("both keystrokes are in the field", got.FieldValue == "mar",
		"the field holding mar", fmt.Sprintf("the field holding %q", got.FieldValue),
		"nothing in the keystroke path resets the value.")
	r.assert("the suggestions narrow to what was typed", len(got.Picks) > 0,
		"at least one .selection-pick for the query",
		fmt.Sprintf("%d pick(s): %v", len(got.Picks), got.Picks),
		"views/anonymise.js selectionPicks renders valueAutocomplete's answer as buttons.")
}

func boolIs(p *bool, want bool) bool { return p != nil && *p == want }

func describeBool(p *bool) string {
	if p == nil {
		return "the control was not found at all"
	}
	return fmt.Sprintf("%t", *p)
}
