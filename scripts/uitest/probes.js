// probes.js, the browser-side half of the real-rendering test layer
// (docs/UITESTING.md layer 3). ONE copy, read by BOTH harnesses:
//
//   scripts/uitest/renderharness Linux, Chromium, Go. Runs in CI.
//   scripts/uitest/Invoke-UITest.ps1 Windows, Edge, PowerShell. Additional
//                                     platform check, run by hand or on tags.
//
// Why the probes live here rather than inside each harness: the assertions are
// the valuable part and they are identical on both platforms. Two copies of
// "which selector holds the Configure rail" is two copies to forget to update,
// and the harness that never runs is the copy that rots. The harnesses now own
// only the plumbing (start a browser, speak the DevTools Protocol, print a
// verdict); WHAT is asserted is here.
//
// HOW IT IS USED
//
// The whole file is one expression. A harness evaluates it once with
// Runtime.evaluate, which installs `window.__uiProbes` and returns the string
// "installed". Every probe after that is a separate Runtime.evaluate of
// `__uiProbes.<name>(...)` with awaitPromise + returnByValue, so each probe
// hands back plain JSON data and the harness does the comparing.
//
// WHAT THIS LAYER CANNOT SEE
//
// The Go bridge is ABSENT here. The page is served as static files, so
// `window.go` does not exist and every api.js call rejects with the readable
// "Wails bridge not available" error api.js is designed to throw. That is
// deliberate and it is the boundary of this layer: it covers RENDERING (does
// the screen fit, is the tooltip visible, does the preview show source text),
// never the bridge. Application state is therefore seeded straight into
// state.js, exactly as the user's own actions would leave it, and no probe may
// depend on an api.js call resolving.
//
// This file is NOT part of the shipped frontend. It lives under scripts/ so
// `//go:embed all:frontend` never sees it and nothing test-shaped reaches the
// binary.
(() => {
  // The placeholder shape the pipeline produces: [CATEGORY_N] (root CLAUDE.md
  // section 5). Matches [PERSON_1], [ENTITY_12], [DE_STEUER_ID_3]; deliberately
  // does not match [lowercase_1], [PERSON] or a bare PERSON_1, because those are
  // not placeholders this application emits.
  const PLACEHOLDER_RE = /\[[A-Z][A-Z0-9_]*_\d+\]/;

  // A repaint is synchronous but layout and the tooltip's own measuring pass are
  // not, so every probe gives the renderer a beat before it measures. 250 ms is
  // generous on a loaded CI runner and still keeps the whole run under a few
  // seconds.
  const settle = (ms = 250) => new Promise((r) => setTimeout(r, ms));

  // Long enough for views/identifyworkspace.js INTERSECTION_DEBOUNCE_MS to fire
  // and for the bridgeless refusal behind it to settle. A probe that seeds an
  // intersection inside that window measures a warning the screen is about to
  // clear on its own.
  const INTERSECTION_SETTLE_MS = 700;

  // The store is imported once and cached: a dynamic import per probe would hand
  // back the same module instance anyway, but the cache makes that explicit.
  // The path is absolute so it resolves identically under a server rooted at
  // frontend/ (the Linux harness) and under `wails dev` (the Windows one).
  let storePromise = null;
  const store = () => (storePromise ??= import("/state.js"));

  // --- The seed --------------------------------------------------------------
  //
  // One document, one finished run over it. Source text long enough that the
  // preview and compare panes genuinely have to scroll, which is the only way
  // the layout contract gets tested rather than trivially satisfied.

  const PEOPLE = ["Marie Duval", "Thomas Berger", "Alice Nowak"];
  const SOURCE_LINES = [
    "# Engagement summary",
    "",
    "Prepared for Meridian Consulting by the audit team.",
    "Contact: marie.duval@meridian-consulting.example (+352 621 123 456).",
    "",
  ];
  for (let i = 1; i <= 60; i += 1) {
    SOURCE_LINES.push(
      `${i}. ${PEOPLE[i % PEOPLE.length]} reviewed workpaper WP-${100 + i} for ` +
      "Meridian Consulting and signed it off without further comment or query.",
    );
  }
  // ONE occurrence where a Value appears under a SPELLING rather than its main
  // text. Without it the seed cannot exercise the hover link between the panes
  // at all: with every occurrence spelled the same way, a tint covering only the
  // main text would look exactly like a tint covering the whole family.
  const SPELLING = "Duval";
  const SPELLING_SOURCE = `Countersigned for the engagement team by ${SPELLING}.`;
  SOURCE_LINES.push("", SPELLING_SOURCE);

  const SOURCE = SOURCE_LINES.join("\n");

  // The anonymised counterpart: the same text with placeholders in it. It exists
  // so the Import-preview probe MEANS something. Reported issues 1 and 4 were
  // the source panes showing pipeline output; a state with no placeholders
  // anywhere would pass that assertion no matter what the view rendered.
  const ANONYMISED = SOURCE
    .replaceAll("Marie Duval", "[PERSON_1]")
    .replaceAll("Thomas Berger", "[PERSON_2]")
    .replaceAll("Alice Nowak", "[PERSON_3]")
    .replaceAll("Meridian Consulting", "[ENTITY_1]")
    .replaceAll("marie.duval@meridian-consulting.example", "[EMAIL_1]")
    .replaceAll("+352 621 123 456", "[PHONE_1]")
    .replaceAll(SPELLING_SOURCE, SPELLING_SOURCE.replace(SPELLING, "[PERSON_1]"));

  // Which spelling each occurrence of a placeholder replaced, positionally, the
  // way Go emits it (ResultDocument.occurrenceSpellings): "" means that
  // occurrence WAS the main text. Every [PERSON_1] above the last one is
  // "Marie Duval"; the last is the spelling line.
  const PERSON_1_OCCURRENCES = ANONYMISED.split("[PERSON_1]").length - 1;
  const OCCURRENCE_SPELLINGS = {
    "[PERSON_1]": [
      ...Array(Math.max(0, PERSON_1_OCCURRENCES - 1)).fill(""),
      SPELLING,
    ],
  };

  const VALUES = [
    { original: "Marie Duval", placeholder: "[PERSON_1]", category: "person_names", count: 22 },
    { original: "Thomas Berger", placeholder: "[PERSON_2]", category: "person_names", count: 20 },
    { original: "Alice Nowak", placeholder: "[PERSON_3]", category: "person_names", count: 20 },
    { original: "Meridian Consulting", placeholder: "[ENTITY_1]", category: "entity_names", count: 61 },
    { original: "marie.duval@meridian-consulting.example", placeholder: "[EMAIL_1]", category: "email", count: 1 },
    { original: "+352 621 123 456", placeholder: "[PHONE_1]", category: "phone", count: 1 },
  ];

  const DOC_NAME = "engagement.md";

  /**
   * seed(step) puts the application on `step` with one imported document and one
   * finished run, then waits for the repaint to land.
   *
   * It writes through setState, the store's own entry point, so the state it
   * produces is a state the application could actually reach. Nothing here pokes
   * at internals or at the DOM.
   */
  async function seed(step) {
    const s = await store();
    s.setState({
      screen: "wizard",
      step,
      // A plausible probe result so no LLM control renders in its "probing"
      // state: the bridge is absent, so the real probe never answers.
      ollama: { available: false, models: [], detail: "Not detected on 127.0.0.1:11434." },
      bridge: "pong",
      documents: [{
        name: DOC_NAME,
        format: "md",
        sizeBytes: SOURCE.length,
        markdown: SOURCE,
        previewTruncated: false,
        isGrid: false,
        experimental: false,
        warnings: [],
        unitCount: 0,
      }],
      previewDoc: DOC_NAME,
      importErrors: [],
      sourceCache: {},
      running: false,
      progress: null,
      discovery: null,
      dismissedWarnings: [],
      notice: null,
      confirm: null,
      results: {
        documents: [{
          name: DOC_NAME,
          anonymised: ANONYMISED,
          byCategory: { person_names: 62, entity_names: 61, email: 1, phone: 1 },
          occurrenceSpellings: OCCURRENCE_SPELLINGS,
        }],
        report: {
          level: "medium",
          totalReplacements: 125,
          byCategory: { person_names: 62, entity_names: 61, email: 1, phone: 1 },
          values: VALUES,
          documents: [{ name: DOC_NAME, values: VALUES }],
        },
      },
      mapping: Object.fromEntries(
        VALUES.map((v) => [v.placeholder, { original: v.original, category: v.category }]),
      ),
      // The step 3 Replaced values table. It mirrors the Go registry, which the
      // harness does not have, so the seed stands in for it: without this the
      // table renders empty and the checks below measure a card that is not
      // there.
      replacedValues: VALUES,
      removedValues: [],
    });
    await settle();
  }

  // The two accepted Values the value-card probe acts on. Their spellings are
  // already settled (derivedSpellings set, so nothing is "pending"), because the
  // bridge is absent here and a value awaiting expansion would render a spinner
  // instead of the controls under test.
  const CARD_VALUES = [
    {
      category: "person_names", mainText: "Marie Duval",
      spellings: [], derivedSpellings: ["Marie Duval", "Marie"],
      spellingsError: null, discoveryMethods: ["manual"], evidence: [],
      spellingPolicy: "automatic", status: "accepted",
    },
    {
      category: "entity_names", mainText: "Meridian Consulting",
      spellings: [], derivedSpellings: ["Meridian Consulting"],
      spellingsError: null, discoveryMethods: ["manual"], evidence: [],
      spellingPolicy: "automatic", status: "accepted",
    },
  ];

  // The value the geometry probe measures, and the list it sits in.
  //
  // MEASURED_VALUE carries enough spellings to overflow TWO different boxes, and
  // both matter: the card's one-line chip budget, because a card whose chips all
  // fit has no overflow control to check and no chip row to collapse; and the
  // popup's capped list, because a popup that never scrolls proves nothing about
  // whether a Value with fifty spellings is reachable. The other Values exist so
  // the card LIST genuinely scrolls: a scroll position can only be lost by a list
  // that has one.
  const MEASURED_VALUE = "Meridian";
  // What the rename case renames it to. A distinct name so the probe can tell a
  // renamed card from one the rename silently failed on.
  const RENAMED_VALUE = "Meridian Renamed";
  const MEASURED_SPELLINGS = [
    "Meridian Consulting Group Societe Anonyme", "Meridian Consulting Group",
    "Meridian Consulting", "Meridian Partners", "Meridian", "MCG",
    ...Array.from({ length: 12 }, (_, i) => `Meridian Branch ${i + 1}`),
  ];
  const filler = (i) => ({
    category: "person_names", mainText: `${PEOPLE[i % PEOPLE.length]} ${i + 1}`,
    spellings: [], derivedSpellings: [`${PEOPLE[i % PEOPLE.length]} ${i + 1}`],
    spellingsError: null, discoveryMethods: ["manual"], evidence: [],
    spellingPolicy: "automatic", status: "accepted",
  });
  // The measured card sits in the MIDDLE of the list, not at its head, because
  // the probe scrolls to the middle before it acts. A card that is scrolled out
  // of sight is not the situation being tested: focusing a control inside a
  // scroll container scrolls it into view, so acting on an off-screen card
  // measures the browser doing its job, not the list losing its place.
  const GEOMETRY_VALUES = [
    ...Array.from({ length: 7 }, (_, i) => filler(i)),
    {
      category: "entity_names", mainText: MEASURED_VALUE,
      spellings: [],
      derivedSpellings: MEASURED_SPELLINGS,
      spellingsError: null, discoveryMethods: ["manual"], evidence: [],
      spellingPolicy: "automatic", status: "accepted",
    },
    ...Array.from({ length: 7 }, (_, i) => filler(i + 7)),
  ];

  /** rect(el) is getBoundingClientRect as plain, transferable numbers. */
  const rect = (el) => {
    const r = el.getBoundingClientRect();
    return {
      top: Math.round(r.top), left: Math.round(r.left),
      bottom: Math.round(r.bottom), right: Math.round(r.right),
      width: Math.round(r.width), height: Math.round(r.height),
    };
  };

  globalThis.__uiProbes = {
    // The seed data the harnesses assert against, so an expectation is never
    // spelled twice. The Go harness reads `placeholderPattern` and re-checks it
    // on its own side too: the probe reports what it found, the harness decides
    // whether that is a pass.
    fixture: () => ({
      docName: DOC_NAME,
      placeholderPattern: PLACEHOLDER_RE.source,
      tooltipOriginal: "Marie Duval",
      // Every state.js ALL_CATEGORIES entry that has a SWITCH. custom_patterns
      // has none: it is declarative, permanently on, and edited on the
      // workspace's Custom patterns tab, so the rail renders no checkbox for it.
      categoryCount: 29,
    }),

    /**
     * layout(step) measures the fixed-height layout contract on one wizard step.
     *
     * The contract (frontend/CLAUDE.md): body and #app are 100vh, the page body
     * never scrolls in either direction, and scrolling happens INSIDE A CARD BODY
     * and nowhere else. No string-based test can answer any of that; only a
     * renderer can.
     *
     * It is measured in three parts, because the contract has three failure modes
     * and the first one alone would be a weak test:
     *
     *   1. the page body does not scroll. This is the contract as written, and it
     *      catches chrome that grew past the window (the header, step bar and
     *      footer heights are fixed) and content that widened the page.
     *
     *   2. #view does not CLIP. `main#view { overflow: hidden }` is load-bearing
     *      (see the note above the rule in style.css): it is what stops a
     *      mis-sized card from scrolling the page. The cost is that a card which
     *      does not fit is silently cut off instead, which check 1 would never
     *      see. So the workspace's own overflow is measured too: whatever #view
     *      holds has to FIT, which is exactly what the charter says.
     *
     *   3. every element that actually scrolls is a card body. "Scrolling happens
     *      inside a card body and nowhere else" is a claim about the whole
     *      screen, so it is asserted over the whole screen rather than assumed.
     */
    async layout(step) {
      await seed(step);
      const b = document.body;
      const d = document.documentElement;
      const view = document.getElementById("view");

      const describe = (el) => el.tagName.toLowerCase() +
        (el.id ? `#${el.id}` : "") +
        (el.className && typeof el.className === "string"
          ? `.${el.className.trim().split(/\s+/).join(".")}` : "");

      // The elements ALLOWED to scroll. This list IS the contract's enumeration,
      // so it is spelled out rather than inferred, and each entry earns its place
      // from style.css:
      //
      //   .card-body / .pane-body the card's scrolling surface, the head and
      //                             foot staying put above and below it.
      //   .cgroup-body a collapsible group's body inside the rail.
      //   .rail the Identify rail, itself a card.
      //   .card-column              "a column of stacked cards that scrolls as a
      //                             whole, used for the left side of Anonymise
      //                             and Export. The cards inside it do NOT scroll
      //                             individually." That is a deliberate design
      //                             decision with its reasoning in style.css, not
      //                             an exception to the contract.
      //   .table-scroll the horizontal escape hatch a wide markdown
      //                             table gets so it never widens the page.
      //
      // Anything else that scrolls is a finding. Add to this list only with the
      // style.css rule that justifies it.
      const ALLOWED_SCROLLERS = [
        ".card-body", ".pane-body", ".cgroup-body", ".rail", ".card-column", ".table-scroll",
        // The Replaced values table scrolls inside its own box: a batch can
        // replace hundreds of values, and the card body may not grow the page.
        ".report-value-rows",
      ];

      const scrollers = [];
      for (const el of document.querySelectorAll("#view, #view *")) {
        const style = getComputedStyle(el);
        const scrollsDown = /(auto|scroll)/.test(style.overflowY) &&
          el.scrollHeight - el.clientHeight > 1;
        const scrollsAcross = /(auto|scroll)/.test(style.overflowX) &&
          el.scrollWidth - el.clientWidth > 1;
        if (!scrollsDown && !scrollsAcross) continue;
        scrollers.push({
          selector: describe(el),
          allowed: ALLOWED_SCROLLERS.some((sel) => el.matches(sel) || el.closest(sel) === el),
          down: el.scrollHeight - el.clientHeight,
          across: el.scrollWidth - el.clientWidth,
        });
      }

      return {
        step,
        down: Math.max(b.scrollHeight - b.clientHeight, d.scrollHeight - d.clientHeight),
        across: Math.max(b.scrollWidth - b.clientWidth, d.scrollWidth - d.clientWidth),
        viewClipsDown: view ? Math.max(0, view.scrollHeight - view.clientHeight) : -1,
        viewClipsAcross: view ? Math.max(0, view.scrollWidth - view.clientWidth) : -1,
        viewport: { width: innerWidth, height: innerHeight },
        scrollers,
        // What the harness needs in its failure message: the tallest thing inside
        // the workspace, which is nearly always the item missing `min-height: 0`.
        tallest: (() => {
          let worst = { selector: "", height: 0 };
          for (const el of document.querySelectorAll("#view *")) {
            if (el.scrollHeight > worst.height) {
              worst = { selector: describe(el), height: el.scrollHeight };
            }
          }
          return worst;
        })(),
      };
    },

    /**
     * importPreview() reads the RENDERED text of the Import preview.
     *
     * Reported issues 1 and 4 seen through the pixels: the source panes were
     * showing pipeline output. innerText, not innerHTML, because what matters is
     * what a human reads off the screen. The state is seeded WITH a finished run
     * (see ANONYMISED above), so a view that reached for the wrong text has
     * something wrong to reach for.
     */
    async importPreview() {
      await seed("import");
      const pane = document.querySelector("#import-preview .md-preview")
        ?? document.querySelector(".md-preview");
      if (!pane) {
        return { error: "no .md-preview rendered on the Import screen" };
      }
      const text = pane.innerText ?? "";
      const match = text.match(PLACEHOLDER_RE);
      return {
        chars: text.length,
        placeholder: match ? match[0] : "",
        // A short window around the offending placeholder, or the opening of the
        // pane when there is none, so a failure says WHERE it went wrong.
        excerpt: (match
          ? text.slice(Math.max(0, match.index - 60), match.index + 60)
          : text.slice(0, 120)).replace(/\s+/g, " "),
        showsSource: text.includes("Meridian Consulting"),
      };
    },

    /**
     * configureRail() reports the shape of the Identify screen's left rail.
     *
     * The Configure choices are switchable DETECTION ROUTE sections rather than
     * peer tabs, one section per mechanism: Built-in patterns on, Heuristic
     * discovery on, Local LLM discovery off. Two switch-less panels follow them,
     * Detection quality and Load profile, and they carry .rail-panel rather than
     * .rail-section so a utility panel is never counted as a route.
     *
     * The category groups start FOLDED, so the rail opens on the route switches
     * and the scope summary rather than a wall of category lists; this probe
     * measures both that they are folded by default and that opening a group lays
     * its checkboxes out.
     */
    async configureRail() {
      // The store is read for the lists the rail is meant to RENDER, so an
      // expectation is never written twice.
      const s = await store();
      await seed("identify");
      const railOf = () => document.querySelector("#identify-rail");
      if (!railOf()) return { error: "no #identify-rail rendered on the Identify screen" };

      // Count the category checkboxes that are actually laid out. A folded
      // group's body is display:none, so its checkboxes have no box at all. Read
      // fresh each time, because a fold toggles through setState and rebuilds the
      // rail, detaching the nodes measured before it.
      const catWithSize = () => [...railOf().querySelectorAll(".cat-toggle")]
        .filter((c) => c.getBoundingClientRect().height > 0).length;

      // Folded by default: expect zero laid out. Then open every category group
      // and measure again, which proves they are reachable once opened; then fold
      // them all back so the probes that run after this one see the default rail.
      const categoriesWithSize = catWithSize();
      const catGroupIds = [...railOf().querySelectorAll("[data-group-toggle]")]
        .map((h) => h.dataset.groupToggle)
        .filter((id) => id && id.startsWith("cat-group-"));
      const clickGroup = async (id) => {
        const head = railOf().querySelector(`[data-group-toggle="${id}"]`);
        if (head) { head.click(); await settle(60); }
      };
      for (const id of catGroupIds) await clickGroup(id);
      const categoriesWithSizeAfterExpand = catWithSize();

      // A signal's readings hang off that signal's own category row: the row, then
      // the button opening the readings, then the icon explaining it, then the
      // example. "On one line" is a claim about geometry, and it can only be
      // measured while the group is open, so it is measured here rather than in the
      // string tests, which see markup order and stop there.
      const signalRowLine = (() => {
        const head = railOf().querySelector(".signal-row .signal-row-head");
        const label = head?.querySelector(".cat-label");
        const drill = head?.querySelector(".signal-drill");
        const help = head?.querySelector("span.help");
        if (!head || !label || !drill || !help) return null;
        const l = label.getBoundingClientRect();
        const d = drill.getBoundingClientRect();
        const h = help.getBoundingClientRect();
        return {
          sameRow: Math.abs(l.top - d.top) <= 2 && Math.abs(l.top - h.top) <= 2,
          drillIsAfterLabel: d.left >= l.right - 1,
          helpIsAfterDrill: h.left >= d.right - 1,
          // The rail is narrow, and a row that overflows it is a control the user
          // cannot see: the page body never scrolls sideways (the layout contract).
          fitsTheRail: head.scrollWidth <= head.clientWidth + 1,
          widths: `row ${Math.round(head.clientWidth)} of ${Math.round(head.scrollWidth)}px`,
        };
      })();

      for (const id of catGroupIds) await clickGroup(id);

      // The Local LLM discovery section is folded by default, so its controls are
      // in the DOM at zero height: a string test reads them as present and a user
      // reads them as absent. Open it, measure the detail level (a label and a
      // select on one line, in the narrowest column of the application), then fold
      // it back so the probes after this one see the default rail.
      await clickGroup("rail-local");
      const detailLevelRow = (() => {
        const row = [...railOf().querySelectorAll(".rail-field-row")]
          .find((r) => r.querySelector("#ai-detail-level"));
        const label = row?.querySelector(".rail-field-label");
        const select = row?.querySelector("#ai-detail-level");
        const help = row?.querySelector("span.help");
        if (!row || !label || !select || !help) return null;
        const l = label.getBoundingClientRect();
        const sel = select.getBoundingClientRect();
        // "One row" is a claim about vertical CENTRES, not about tops. The field
        // is a two-column grid with align-items:center, so a short label and a
        // taller select are centred on the same line and their TOPS therefore
        // differ by half the height difference. Comparing tops fails a layout
        // that is correct.
        //
        // What could still be wrong is the label WRAPPING inside a narrow column,
        // which is a genuine two-line label, so that is measured separately: an
        // inline span laid out over two lines returns two client rects.
        const labelMid = l.top + l.height / 2;
        const selectMid = sel.top + sel.height / 2;
        return {
          laidOut: sel.height > 0,
          // A rail label clipped to an ellipsis is a control the user cannot read.
          labelFullyShown: label.scrollWidth <= label.clientWidth + 1,
          label: `${(label.textContent ?? "").trim()}: ${label.clientWidth} of ${label.scrollWidth}px`,
          sameRow: Math.abs(labelMid - selectMid) <= 2,
          labelLines: label.getClientRects().length,
          fitsTheRail: row.scrollWidth <= row.clientWidth + 1,
          widths: `row ${Math.round(row.clientWidth)} of ${Math.round(row.scrollWidth)}px, ` +
            `label ${Math.round(l.height)}px centred at ${Math.round(labelMid)}, ` +
            `select ${Math.round(sel.height)}px centred at ${Math.round(selectMid)}`,
          // Exactly one option marked, or the browser chooses the level by option
          // ordering, which is the defect that picked the wrong model.
          optionsSelected: [...select.querySelectorAll("option")]
            .filter((o) => o.selected).map((o) => o.value),
        };
      })();
      await clickGroup("rail-local");

      const rail = railOf();
      const toggles = [...rail.querySelectorAll(".route-toggle")];
      const byRoute = (route) => toggles.find((t) => t.dataset.route === route);
      return {
        sections: rail.querySelectorAll(".rail-section").length,
        // The switch-less panels, counted separately: .rail-section marks a
        // detection ROUTE, so a panel wearing it would be counted as one.
        panels: rail.querySelectorAll(".rail-panel").length,
        railTabs: document.querySelectorAll("[data-railtab]").length,
        routes: toggles.map((t) => t.dataset.route),
        // One reading per route switch, because each is its own stored flag: a
        // single "the offline route is on" answer could not say which of the two
        // mechanisms the user actually left running.
        patternsOn: byRoute("rail-patterns")?.checked ?? null,
        heuristicOn: byRoute("rail-heuristic")?.checked ?? null,
        localOn: byRoute("rail-local")?.checked ?? null,
        categories: rail.querySelectorAll(".cat-toggle").length,
        // Folded by default, so zero are laid out now; all of them once opened.
        categoriesWithSize,
        categoriesWithSizeAfterExpand,
        // The signal control is a tree hanging off the category row of the signal
        // it reads: one drill-down per signal, its readings one click below.
        // Collapsed the readings are in the DOM at zero height, which a string test
        // reads as "present" and a user reads as absent, so both states are
        // measured. The expanding half is done in its own probe below, because it
        // has to click.
        signalRows: [...rail.querySelectorAll(".signal-row")]
          .map((r) => r.dataset.signalSource),
        // The store's own list, so the harness compares the rail against what it
        // is meant to render rather than against a number written in the harness:
        // a hardcoded count is left behind by the next signal source.
        signalSources: [...s.SIGNAL_SOURCES],
        signalMasters: [...rail.querySelectorAll(".signal-master")]
          .map((b) => b.dataset.source),
        signalRowLine,
        detailLevelRow,
        // Each route's switch sits on that route's OWN header, beside its title
        // and its help icon. "On the same row, and the title not truncated by
        // them" is a claim about geometry, so geometry answers it: markup order
        // proves nothing, because a column-flex header stacks the same markup and
        // an overflowing title is clipped without anything throwing.
        routeHeaders: [...rail.querySelectorAll("section.rail-section")].map((section) => {
          const head = section.querySelector(".cgroup-head");
          const title = head?.querySelector(".cgroup-title");
          const state = head?.querySelector(".route-state");
          const box = head?.querySelector(".route-toggle");
          if (!head || !title || !state || !box) return null;
          const t = title.getBoundingClientRect();
          const st = state.getBoundingClientRect();
          return {
            route: box.dataset.route ?? "",
            title: (title.textContent ?? "").trim(),
            // Vertical CENTRES, not tops: the header is a flex row with
            // align-items centre, so a short title and a taller switch label are
            // on one line while their tops differ by half the height difference.
            sameRow: Math.abs((t.top + t.height / 2) - (st.top + st.height / 2)) <= 2,
            switchIsToTheRight: st.left >= t.right - 1,
            // The title must not be clipped to an ellipsis by the switch and the
            // help icon beside it: a route the user cannot read the name of is
            // a switch with no label.
            titleFullyShown: title.scrollWidth <= title.clientWidth + 1,
            titleLines: title.getClientRects().length,
            hasHelp: !!head.querySelector("span.help"),
            widths: `${(title.textContent ?? "").trim()}: ${title.clientWidth} of ${title.scrollWidth}px`,
            // The header never overflows the rail, which is the narrowest column
            // in the application and the one the page must not scroll sideways for.
            fitsTheRail: head.scrollWidth <= head.clientWidth + 1,
          };
        }),
      };
    },

    /**
     * valueCardActions() drives a value card's controls in the real browser and
     * reports what reached the store.
     *
     * This is the only layer that can see the defect it exists for. A card names
     * the Value its handlers act on through `data-` attributes, and the BROWSER
     * lower-cases attribute names while a string test preserves them: a
     * camel-case `data-mainText` renders, satisfies every string assertion, and
     * arrives as `dataset.maintext`, so `dataset.mainText` is undefined and
     * renaming, removing, dropping a spelling and merging all silently do
     * nothing. Nothing throws, so nothing anywhere else notices.
     *
     * It reports the store's own answer after each action, never the markup: the
     * markup rendering correctly is exactly the state the bug leaves it in.
     */
    async valueCardActions() {
      const s = await store();
      await seed("identify");
      s.setState({ values: CARD_VALUES.map((v) => ({ ...v })) });
      await settle();

      // The workspace opens on Suggestions; My values is a click away, and view
      // state inside the module, so it is only reachable the way a user reaches
      // it.
      const tab = document.querySelector('[data-wstab="values"]');
      if (!tab) return { error: "no [data-wstab] tabs rendered in the Identify workspace" };
      tab.click();
      await settle();

      const cardFor = (mainText) => [...document.querySelectorAll(".value-card")]
        .find((c) => c.dataset.mainText === mainText) ?? null;

      const before = [...document.querySelectorAll(".value-card")];
      const identified = before.filter((c) => c.dataset.category && c.dataset.mainText).length;
      if (before.length === 0) return { error: "the My values tab rendered no .value-card" };

      // 1. Rename. Clicking the name reveals an inline input; typing and blurring
      // commits it.
      let renamed = null;
      const target = cardFor("Marie Duval");
      if (target) {
        target.querySelector(".value-name")?.click();
        await settle(80);
        const input = target.querySelector(".value-name-input");
        if (input) {
          input.value = "Marie Dupont";
          input.dispatchEvent(new Event("blur"));
          await settle();
          renamed = s.getState().values.some((v) => v.mainText === "Marie Dupont");
        }
      }

      // 2. Remove. The count in the store is the answer; the card disappearing
      // is not, because a repaint reads the store either way.
      const countBeforeRemove = s.getState().values.length;
      const removable = cardFor("Meridian Consulting");
      removable?.querySelector(".value-remove")?.click();
      await settle();

      return {
        cards: before.length,
        cardsWithIdentity: identified,
        inlineInputAppeared: !!target && !!document.querySelector(".value-card") && renamed !== null,
        renamed,
        removedOne: s.getState().values.length === countBeforeRemove - 1,
        valuesAfter: s.getState().values.map((v) => v.mainText),
      };
    },

    /**
     * builtInPatternsTabLayout() measures the read-only Built-in patterns tab
     * with a match long enough to threaten the card's width.
     *
     * Three pixel claims a markup test cannot reach:
     *
     *   1. a long matched text (a URL, a street address) does not widen the page.
     *      The layout contract is that the page body never scrolls horizontally,
     *      and a monospaced string with no spaces in it is the classic way to
     *      break that.
     *
     *   2. the occurrence note stays ON the row rather than being pushed out of
     *      the card by the text beside it. The note is the answer to "where is
     *      this", so a note off the right edge is a note nobody reads.
     *
     *   3. the rows scroll INSIDE the card body, like every other list on this
     *      screen, rather than growing the card.
     *
     * It also reports the row and section counts, so the harness can confirm the
     * tab renders a section per ACTIVE category, empty ones included: that is the
     * whole reason the tab exists, and an empty section dropped by a repaint
     * would look exactly like a category that never ran.
     */
    async builtInPatternsTabLayout() {
      const s = await store();
      await seed("identify");
      s.setState({
        builtInPatterns: {
          on: true,
          // Postal codes are ACTIVE and match nothing here: the section must
          // still be drawn.
          categories: ["url", "address", "postal_code"],
          matches: [
            {
              category: "url",
              text: "https://intranet.meridian-consulting.example.com/engagements/2026/framework-agreement-schedule-4?revision=17",
              count: 4, documents: [DOC_NAME], confidence: 1,
            },
            {
              category: "address",
              text: "12, Avenue de l'Innovation et du Developpement Economique",
              count: 2, documents: [DOC_NAME], confidence: 1,
            },
          ],
        },
      });
      await settle();

      const tab = document.querySelector('[data-wstab="builtin"]');
      if (!tab) return { error: "no [data-wstab=builtin] tab rendered in the Identify workspace" };
      tab.click();
      await settle();

      const rows = [...document.querySelectorAll(".builtin-row")];
      if (rows.length === 0) return { error: "the Built-in patterns tab rendered no .builtin-row" };
      const groups = [...document.querySelectorAll(".builtin-group")];
      const body = rows[0].closest(".card-body") ?? rows[0].parentElement;

      // The widest row against the card that holds it: a row wider than its own
      // container is the overflow, whether or not the page has scrolled yet.
      let widest = 0;
      let noteInside = true;
      for (const row of rows) {
        const r = rect(row);
        widest = Math.max(widest, r.right);
        const note = row.querySelector(".builtin-where");
        if (note && rect(note).right > r.right + 1) noteInside = false;
      }

      return {
        rows: rows.length,
        groups: groups.length,
        emptyGroups: groups.filter((g) => g.querySelector(".grid-empty")).length,
        actions: document.querySelectorAll(".builtin-row button, .builtin-row input").length,
        pageScrollsSideways: document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
        widestRowRight: Math.round(widest),
        cardRight: rect(body).right,
        noteInside,
        bodyScrolls: body.scrollHeight > body.clientHeight,
        bodyScrollsSideways: body.scrollWidth > body.clientWidth + 1,
      };
    },

    /**
     * valuesTabLayout() measures the My values tab's two captioned blocks and its
     * Ctrl+click selection.
     *
     * Three of the four claims here are pixel claims a string test cannot reach.
     * "The search and the type filter are on one row" and "Clear all is aligned
     * with Add value" are about laid-out geometry, and a container class only
     * PREDICTS them: a wrapped flex row renders both controls inside the right
     * parent and still stacks them. "The type filter is not bold and matches the
     * add row's own dropdown" is about computed style, which is where a stray
     * `font-weight: 700` from a shared class survives every markup assertion. And
     * the selection tint is a colour: the card carries .selected either way, so
     * only a computed background says whether the user can SEE what they picked.
     *
     * The gestures are the real ones: a Ctrl+click on the card's own surface, then
     * the same click again to let it go. Anything that swallowed the modifier
     * (a control catching the click, a preventDefault in the wrong place) shows up
     * as a card that never changes colour.
     */
    async valuesTabLayout() {
      const s = await store();
      await seed("identify");
      s.setState({ values: CARD_VALUES.map((v) => ({ ...v })) });
      await settle();

      const tab = document.querySelector('[data-wstab="values"]');
      if (!tab) return { error: "no [data-wstab] tabs rendered in the Identify workspace" };
      tab.click();
      await settle();

      const captions = [...document.querySelectorAll("#identify-workspace .values-section")]
        .map((section) => section.querySelector(".section-label")?.textContent?.trim() ?? "");

      const search = document.querySelector("#values-search");
      const typeFilter = document.querySelector("#values-type");
      const draft = document.querySelector("#value-draft");
      const category = document.querySelector("#value-category");
      const addBtn = document.querySelector("#btn-add-value");
      const clearBtn = document.querySelector("#btn-clear-values");
      if (!search || !typeFilter || !draft || !category || !addBtn || !clearBtn) {
        return { error: "the My values tab is missing one of its filter or add controls" };
      }

      // Centre lines, not tops: a select and a button of different heights are
      // still aligned, and comparing tops would report a false failure.
      const centreY = (el) => {
        const r = el.getBoundingClientRect();
        return (r.top + r.bottom) / 2;
      };
      const filterRowOffset = Math.round(Math.abs(centreY(search) - centreY(typeFilter)));
      const addRowOffset = Math.round(Math.abs(centreY(addBtn) - centreY(clearBtn)));

      const filterStyle = getComputedStyle(typeFilter);
      const categoryStyle = getComputedStyle(category);
      const weight = (style) => Number.parseInt(style.fontWeight, 10) || 400;

      // The card is picked by Ctrl+click on its own surface. The event is
      // dispatched on the CARD rather than on a child, because a child is either
      // a control (which must keep its own meaning) or a text node inside one.
      const cardFor = (mainText) => [...document.querySelectorAll(".value-card")]
        .find((c) => c.dataset.mainText === mainText) ?? null;
      const picked = cardFor("Marie Duval");
      const other = cardFor("Meridian Consulting");
      if (!picked || !other) return { error: "the seeded Values did not both render a card" };

      const plainBg = getComputedStyle(picked).backgroundColor;
      const clearLabelPlain = clearBtn.textContent.trim();

      picked.dispatchEvent(new MouseEvent("click", { bubbles: true, ctrlKey: true }));
      await settle();

      const pickedAgain = cardFor("Marie Duval");
      const selectedBg = pickedAgain ? getComputedStyle(pickedAgain).backgroundColor : "";
      const selectedCount = document.querySelectorAll(".value-card.selected").length;
      const othersTinted = getComputedStyle(cardFor("Meridian Consulting")).backgroundColor;
      const clearLabelPicked = document.querySelector("#btn-clear-values")?.textContent?.trim() ?? "";

      // The way back, through the same gesture: a selection with no undo turns a
      // mis-click into a destroyed list.
      pickedAgain.dispatchEvent(new MouseEvent("click", { bubbles: true, ctrlKey: true }));
      await settle();
      const selectedAfterUndo = document.querySelectorAll(".value-card.selected").length;
      const clearLabelUndone = document.querySelector("#btn-clear-values")?.textContent?.trim() ?? "";

      return {
        captions,
        filterRowOffset, addRowOffset,
        filterWeight: weight(filterStyle), categoryWeight: weight(categoryStyle),
        filterFontSize: filterStyle.fontSize, categoryFontSize: categoryStyle.fontSize,
        plainBg, selectedBg, othersTinted,
        selectedCount, selectedAfterUndo,
        clearLabelPlain, clearLabelPicked, clearLabelUndone,
      };
    },

    /**
     * valueCardGeometry() measures whether a value card's HEIGHT is independent
     * of its data, and whether the list keeps its scroll position across the
     * actions that used to move it.
     *
     * This is the probe for the complaint the compact card exists to fix. The
     * scroll preserver in scroll.js writes back a RAW pixel offset, which is
     * sound only while the content is the same height. Editing a spelling resets
     * the row to pending, the chips are momentarily replaced by one line of text,
     * the card shrinks, the browser CLAMPS the restored offset to the now-shorter
     * scrollHeight, and the NEXT repaint snapshots the clamped value. The
     * position is then lost upward, permanently. Warnings appearing and clearing
     * did the same thing from the other direction.
     *
     * No string test can see any of that: the markup was correct throughout. It
     * needs a renderer, a scroll container with real overflow, and a measurement
     * before and after.
     *
     * Four cases, because the height came from four places:
     *   1. deleting a spelling through the popup, which shortens the chip row;
     *   2. renaming the value, which sends the row back to PENDING: the chips
     *      are replaced by one line of text, which is the collapse the owner
     *      actually reported. It is a separate case from 1 because deleting
     *      CURATES (the list is settled, derivedSpellings []) while renaming
     *      leaves derivedSpellings null for Go to answer;
     *   3. a warning appearing, which used to add a row to the card;
     *   4. deleting a whole card, which legitimately shortens the list. Here the
     *      offset may move, but by at most one card: clamping to the top is the
     *      failure.
     */
    async valueCardGeometry() {
      const s = await store();
      await seed("identify");
      // Enough cards that the list genuinely scrolls, and enough spellings on the
      // measured one that its chip row has something to overflow.
      s.setState({ values: GEOMETRY_VALUES.map((v) => ({ ...v })) });
      await settle();

      const tab = document.querySelector('[data-wstab="values"]');
      if (!tab) return { error: "no [data-wstab] tabs rendered in the Identify workspace" };
      tab.click();
      await settle();

      const scroller = document.querySelector("#identify-workspace .card-body");
      if (!scroller) return { error: "no scrolling .card-body in the Identify workspace" };
      const cardFor = (mainText) => [...document.querySelectorAll(".value-card")]
        .find((c) => c.dataset.mainText === mainText) ?? null;

      const measured = cardFor(MEASURED_VALUE);
      if (!measured) return { error: `no .value-card for ${MEASURED_VALUE}` };
      if (scroller.scrollHeight <= scroller.clientHeight) {
        return { error: "the value list does not overflow, so it cannot lose a scroll position" };
      }

      // Scroll to the middle: at the top there is nothing to lose, which is how a
      // broken preserver passes.
      scroller.scrollTop = Math.round((scroller.scrollHeight - scroller.clientHeight) / 2);
      await settle();
      const scrollBefore = scroller.scrollTop;
      const heightBefore = Math.round(measured.getBoundingClientRect().height);
      const overflowsChips = !!measured.querySelector(".spelling-more");

      // 1. A spelling edit. It is driven through the popup, which is where that
      // gesture lives, and it sends the row back to pending on the way out.
      measured.querySelector(".spelling-add")?.click();
      await settle();
      const popup = document.querySelector(".spellings-layer");
      const listScrolls = !!popup &&
        popup.querySelector(".spelling-list")?.scrollHeight >
          popup.querySelector(".spelling-list")?.clientHeight;
      let deleted = null;
      if (popup) {
        const row = [...popup.querySelectorAll(".spelling-list-row")]
          .find((r) => r.dataset.spellingRow);
        row?.querySelector(".spelling-delete")?.click();
        await settle();
        deleted = !!row;
        document.querySelector(".spellings-close")?.click();
        await settle();
      }
      const afterEdit = cardFor(MEASURED_VALUE);
      const heightAfterEdit = afterEdit
        ? Math.round(afterEdit.getBoundingClientRect().height) : 0;
      const scrollAfterEdit = document
        .querySelector("#identify-workspace .card-body").scrollTop;

      // 2. Renaming, which is really two cases, because the sentinel a rename
      // writes depends on the row's spelling POLICY (valuemodel.js repend).
      //
      // 2a. The row edited in step 1 is now CURATED, and amending a curated row
      // SETTLES it: its chips are its list, so there is nothing for Go to derive.
      // Sending it back to pending instead is the stuck card, because
      // pendingExpansions skips curated rows and no expansion ever arrives to
      // clear the sentinel.
      const renameCard = (card, to) => {
        card.querySelector(".value-name")?.click();
        return settle(80).then(() => {
          const nameInput = card.querySelector(".value-name-input");
          if (!nameInput) return false;
          nameInput.value = to;
          nameInput.dispatchEvent(new Event("blur"));
          return settle().then(() => true);
        });
      };

      let curatedSettled = null;
      if (afterEdit && await renameCard(afterEdit, RENAMED_VALUE)) {
        const row = s.getState().values.find((v) => v.mainText === RENAMED_VALUE);
        curatedSettled = !!row && row.spellingPolicy === "curated" &&
          Array.isArray(row.derivedSpellings);
      }
      const renamed = cardFor(RENAMED_VALUE);
      const heightRenamed = renamed
        ? Math.round(renamed.getBoundingClientRect().height) : 0;
      const scrollRenamed = document
        .querySelector("#identify-workspace .card-body").scrollTop;

      // 2b. An AUTOMATIC row is where the pending state lives, and it is the case
      // with the most teeth for the layout: with nothing settled to draw, the chip
      // row falls back to one line of "working out the other spellings...", and
      // that swap is what used to shrink the card and lose the scroll position. It
      // must happen INSIDE the row.
      let pending = null;
      let heightAutoBefore = 0;
      let heightAutoPending = 0;
      let scrollAutoPending = scrollRenamed;
      const auto = [...document.querySelectorAll(".value-card")]
        .find((c) => {
          const row = s.getState().values
            .find((v) => v.mainText === c.dataset.mainText);
          return row && row.spellingPolicy !== "curated";
        });
      if (auto) {
        const from = auto.dataset.mainText;
        heightAutoBefore = Math.round(auto.getBoundingClientRect().height);
        if (await renameCard(auto, `${from} Renamed`)) {
          pending = s.getState().values.some((v) =>
            v.mainText === `${from} Renamed` && v.derivedSpellings === null);
          const after = cardFor(`${from} Renamed`);
          heightAutoPending = after
            ? Math.round(after.getBoundingClientRect().height) : 0;
          scrollAutoPending = document
            .querySelector("#identify-workspace .card-body").scrollTop;
        }
      }

      // 3. A warning appearing. It must be an icon on a row that already exists,
      // never a row of its own.
      //
      // Seeded only once the workspace's own debounced recheck has fired and
      // come back empty. That recheck asks Go, there is no bridge here, and a
      // refusal clears the list: seeding before it fires would have the probe
      // measuring a warning the screen was about to discard. Setting
      // `intersections` does not itself schedule another recheck, because the
      // signature it watches is the value list.
      await settle(INTERSECTION_SETTLE_MS);
      s.setState({
        intersections: [{
          value: RENAMED_VALUE, category: "entity_names", matchClass: "user_defined",
          winnerValue: `hello@${RENAMED_VALUE.toLowerCase()}.example`,
          winnerCategory: "email", winnerMatchClass: "built_in_pattern",
          occurrences: 3, totalOccurrences: 3, documents: [DOC_NAME],
        }],
      });
      await settle();
      const warned = cardFor(RENAMED_VALUE);
      const heightWarned = warned ? Math.round(warned.getBoundingClientRect().height) : 0;
      const hasWarningIcon = !!warned?.querySelector(".warnpop");
      const scrollWarned = document
        .querySelector("#identify-workspace .card-body").scrollTop;

      // 4. Deleting a whole card. The list really is shorter, so the offset may
      // move; clamping it to the top is the failure.
      const doomed = [...document.querySelectorAll(".value-card")]
        .find((c) => c.dataset.mainText !== RENAMED_VALUE);
      const cardHeight = doomed ? Math.round(doomed.getBoundingClientRect().height) : 0;
      const scrollBeforeDelete = document
        .querySelector("#identify-workspace .card-body").scrollTop;
      doomed?.querySelector(".value-remove")?.click();
      await settle();
      const scrollAfterDelete = document
        .querySelector("#identify-workspace .card-body").scrollTop;

      return {
        overflowsChips, listScrolls, deleted, pending, curatedSettled,
        heightAutoBefore, heightAutoPending, scrollAutoPending, hasWarningIcon,
        heightBefore, heightAfterEdit, heightRenamed, heightWarned,
        scrollBefore, scrollAfterEdit, scrollRenamed, scrollWarned,
        cardHeight, scrollBeforeDelete, scrollAfterDelete,
      };
    },

    /**
     * spellingsPopup() opens the popup and drives it, which is the half of the
     * compact card that a string test cannot reach: whether the surface is on
     * screen at all, whether its list scrolls inside itself rather than growing
     * the popup past the window, and whether an add there reaches the card behind
     * it on the same repaint.
     */
    async spellingsPopup() {
      const s = await store();
      await seed("identify");
      s.setState({ values: GEOMETRY_VALUES.map((v) => ({ ...v })) });
      await settle();

      const tab = document.querySelector('[data-wstab="values"]');
      if (!tab) return { error: "no [data-wstab] tabs rendered in the Identify workspace" };
      tab.click();
      await settle();

      const cardFor = (mainText) => [...document.querySelectorAll(".value-card")]
        .find((c) => c.dataset.mainText === mainText) ?? null;
      const card = cardFor(MEASURED_VALUE);
      if (!card) return { error: `no .value-card for ${MEASURED_VALUE}` };

      const moreLabel = card.querySelector(".spelling-more")?.textContent?.trim() ?? "";
      card.querySelector(".spelling-more")?.click();
      await settle();

      const popup = document.querySelector(".spellings-popup");
      if (!popup) return { error: '"+N more" opened no .spellings-popup' };
      const box = rect(popup);
      const list = popup.querySelector(".spelling-list");
      if (!list) return { error: "the popup rendered no .spelling-list" };

      // Painted, not merely present: the rect of a clipped element is still a
      // full-size rect, so the hit test at its own centre is the check with teeth.
      const centre = document.elementFromPoint(
        Math.round((box.left + box.right) / 2), Math.round((box.top + box.bottom) / 2));
      const painted = !!centre && popup.contains(centre);
      const onScreen = box.top >= 0 && box.left >= 0 &&
        box.bottom <= window.innerHeight && box.right <= window.innerWidth;

      // The list scrolls INSIDE the popup rather than growing it past the window.
      const listScrolls = list.scrollHeight > list.clientHeight;
      list.scrollTop = list.scrollHeight;
      await settle(80);
      const listScrolled = list.scrollTop > 0;

      // An add here reaches the card behind it, live, with no OK to press.
      const draft = popup.querySelector("#spelling-draft");
      const added = "Zzz Popup Spelling";
      let chipsAfter = [];
      let onValueAfter = false;
      if (draft) {
        draft.value = added;
        draft.dispatchEvent(new Event("input", { bubbles: true }));
        popup.querySelector("#btn-add-spelling")?.click();
        await settle();
        onValueAfter = s.getState().values.some((v) =>
          v.mainText === MEASURED_VALUE && (v.spellings ?? []).includes(added));
        const back = cardFor(MEASURED_VALUE);
        chipsAfter = [...(back?.querySelectorAll(".spelling-chip") ?? [])]
          .map((c) => c.dataset.spelling);
      }

      return {
        moreLabel, box, painted, onScreen, listScrolls, listScrolled,
        onValueAfter, chipsAfter,
        popupHeight: box.height,
        viewportHeight: window.innerHeight,
      };
    },

    /**
     * configurePanelFit() measures whether the Configure panel FITS.
     *
     * The panel used to carry a paragraph under every control, and the sum of
     * them made it taller than the window: the controls at the foot were
     * unreachable without scrolling past prose that had been read on the first
     * visit and never again. The explanations moved into help tooltips, and this
     * is the measurement that keeps them there.
     *
     * Two numbers matter and neither can be asserted from a string of HTML:
     * whether the rail's scrollable body actually overflows, and whether any
     * static paragraph is still inside it.
     */
    async configurePanelFit() {
      await seed("identify");
      const rail = document.querySelector("#identify-rail");
      if (!rail) return { error: "no #identify-rail rendered on the Identify screen" };
      // The scrolling element is the rail's card body; fall back to the rail
      // itself so the probe reports a number rather than an error if the class
      // moves.
      const body = rail.querySelector(".card-body") ?? rail;

      // Scrolling the panel to its foot and measuring the LAST control is the
      // question that matters. The panel is allowed to scroll: it holds
      // every category checkbox and the window is what it is. What is not allowed
      // is a foot the user cannot get to, which is what a paragraph under every
      // control produced.
      const footReachable = await (async () => {
        // The LAST SECTION's header, not the last .rail-block: a block inside a
        // folded section has zero height by design, so measuring one would report
        // an unreachable foot for a panel that is perfectly fine.
        const sections = [...rail.querySelectorAll("section.rail-section, section.rail-panel")];
        const last = sections[sections.length - 1]?.querySelector("[data-group-toggle]");
        if (!last) return null;
        body.scrollTop = body.scrollHeight;
        await new Promise((r) => requestAnimationFrame(() => r()));
        const box = last.getBoundingClientRect();
        const clip = body.getBoundingClientRect();
        const visible = box.height > 0 && box.bottom > clip.top && box.top < clip.bottom;
        body.scrollTop = 0;
        return visible;
      })();

      // Static explanatory prose, however it is marked up: a paragraph inside the
      // rail that is not one of the live read-outs. Counting the class alone would
      // pass the moment a paragraph were given a different one.
      const prose = [...rail.querySelectorAll("p")]
        .filter((el) => !el.classList.contains("rail-readout"));

      return {
        scrollHeight: Math.round(body.scrollHeight),
        clientHeight: Math.round(body.clientHeight),
        // The page itself must not have grown to fit the rail: the rail scrolls
        // inside its own card body and nowhere else.
        pageOverflows: document.documentElement.scrollHeight > window.innerHeight + 1,
        footReachable,
        proseParagraphs: prose.length,
        proseHeight: Math.round(prose.reduce((sum, el) => sum + el.getBoundingClientRect().height, 0)),
        helpTooltips: rail.querySelectorAll("span.help").length,
      };
    },

    /**
     * signalDerivations() opens a signal's drill-down and measures what appears.
     *
     * Collapsed, a signal's readings are in the DOM with zero height: present to a
     * string test and absent to the user. So "opening it shows them" is a claim
     * about geometry, and this answers it with geometry, by clicking the button the
     * user clicks and measuring the readings before and after.
     *
     * The drill-down hangs off the signal's own CATEGORY row, and the category
     * groups start folded, so the group is opened first: with it folded, "no reading
     * is laid out" would be true for a reason that has nothing to do with the
     * drill-down. rowLaidOut is what says the row itself IS on screen while its
     * readings are not.
     *
     * It also drives the two controls that must NOT be one: the button opens
     * without ticking, and a reading ticks without closing. Two jobs on one element
     * is how a setting gets flipped by accident.
     */
    async signalDerivations() {
      const s = await store();
      await seed("identify");
      const railOf = () => document.querySelector("#identify-rail");
      const rowOf = () => railOf()?.querySelector(".signal-row");
      if (!rowOf()) return { error: "no .signal-row in the Identify rail" };
      const source = rowOf().dataset.signalSource;

      const boxes = () => [...(rowOf()?.querySelectorAll("input.signal-box") ?? [])];
      const visibleRows = () => boxes()
        .filter((b) => b.getBoundingClientRect().height > 0).length;
      const collapsedRows = boxes().length;

      // Open the category group the row lives in. Every repaint below replaces the
      // nodes, so each step re-queries from the rail rather than holding a handle.
      const groupToggle = () => {
        const group = rowOf()?.closest("section.cgroup");
        return group?.querySelector("[data-group-toggle]");
      };
      const openedGroup = groupToggle();
      if (!openedGroup) return { error: "the signal row is in no foldable category group" };
      openedGroup.click();
      await settle();

      const rowLaidOut = (rowOf()?.getBoundingClientRect().height ?? 0) > 0;
      const collapsedVisible = visibleRows();

      const drill = rowOf()?.querySelector(".signal-drill");
      if (!drill) return { error: "the signal row has no drill-down button to open it" };
      drill.click();
      await settle();
      const openedVisible = visibleRows();

      // Ticking a reading must not close the drill-down: the checkbox stops the
      // click reaching anything else. Measured through the STORE, because a checkbox
      // visually unticking itself proves nothing about what the next run reads.
      const first = boxes()[0];
      const derivation = first?.dataset.derivation;
      first?.click();
      await settle();

      const stillOpen = visibleRows() > 0;
      const stored = s.getState().settings?.signalSuggestionSources?.[source] ?? {};
      const masterAfterTick = rowOf()?.querySelector(".signal-master")?.checked ?? null;

      // Leave the rail as the next probe expects it: the drill-down closed and the
      // category group folded again. Both are module-level view state, so they
      // outlive the re-seed every probe starts with.
      rowOf()?.querySelector(".signal-drill")?.click();
      await settle();
      groupToggle()?.click();
      await settle();

      return {
        source,
        collapsedRows,
        rowLaidOut,
        collapsedVisible,
        openedVisible,
        derivation,
        readingWentOff: stored[derivation] === false,
        otherReadingsStillOn: Object.entries(stored)
          .filter(([key]) => key !== derivation).every(([, on]) => on !== false),
        groupStayedOpenAfterTicking: stillOpen,
        // The master is DERIVED: one reading off out of two leaves it on.
        masterAfterTick,
      };
    },

    /**
     * strictnessFields() measures the Discovery strictness block: how wide its
     * select is, and how far its fields are inset.
     *
     * Both were reported against the built application and neither is visible to
     * a string test. `.rail-field` gave the control column 6rem, which is
     * narrower than the select's own longest option, so "How much to trust" read
     * as a truncated stub; and `.cgroup-body` carries no padding of its own, so a
     * subgroup's fields sat flush against its border while everything above them
     * was inset. Pixels are the only way to say either.
     */
    async strictnessFields() {
      await seed("identify");
      const rail = document.querySelector("#identify-rail");
      if (!rail) return { error: "no #identify-rail rendered on the Identify screen" };

      const subgroup = rail.querySelector(".rail-subgroup");
      if (!subgroup) return { error: "no .rail-subgroup (the Discovery strictness block) in the rail" };
      // The block folds; open it the way the user does, through its own head.
      if (subgroup.dataset.open === "false") {
        subgroup.querySelector(".cgroup-title")?.click();
        await settle();
      }

      const select = document.querySelector("#smart-strictness");
      if (!select) return { error: "no #smart-strictness select in the strictness block" };

      // The widest option decides whether the box is readable. Measured by
      // rendering the option text into a span that inherits the select's font,
      // because a <option>'s own box is drawn by the platform and has no useful
      // rect.
      const probeSpan = document.createElement("span");
      const style = window.getComputedStyle(select);
      probeSpan.style.font = style.font || `${style.fontSize} ${style.fontFamily}`;
      probeSpan.style.position = "fixed";
      probeSpan.style.visibility = "hidden";
      probeSpan.style.whiteSpace = "pre";
      document.body.appendChild(probeSpan);
      let widestOption = 0;
      let widestText = "";
      for (const option of select.options) {
        probeSpan.textContent = option.textContent;
        const w = probeSpan.getBoundingClientRect().width;
        if (w > widestOption) {
          widestOption = w;
          widestText = option.textContent;
        }
      }
      probeSpan.remove();

      // The indentation: a nested field's label against a label of the section
      // ABOVE it. Both are read as viewport x, so the comparison is the one the
      // eye makes.
      const nestedLabel = subgroup.querySelector(".rail-field-label");
      // Anchored on the DOCUMENT COUNTRY label, the first labelled block in the
      // rail (it leads the Built-in patterns section). A positional selector
      // would silently re-point at another section's first label the moment the
      // sections change, and the comparison would then be against nothing in
      // particular.
      const sectionLabel = rail.querySelector("#document-country")
        ?.closest(".rail-block")?.querySelector(".section-label")
        ?? rail.querySelector(".rail-section .cgroup-body .section-label");

      return {
        selectWidth: Math.round(select.getBoundingClientRect().width),
        widestOption: Math.round(widestOption),
        widestText,
        // A select's own padding and its dropdown arrow both eat into the text
        // room, so the box has to be WIDER than the text, not merely equal.
        selectFitsWidestOption: select.getBoundingClientRect().width >= widestOption,
        nestedLabelLeft: nestedLabel ? Math.round(nestedLabel.getBoundingClientRect().left) : null,
        sectionLabelLeft: sectionLabel ? Math.round(sectionLabel.getBoundingClientRect().left) : null,
        // Every field in the block, so a stray one that escaped the inset shows.
        fieldLabelLefts: [...subgroup.querySelectorAll(".rail-field-label")]
          .map((el) => Math.round(el.getBoundingClientRect().left)),
        // Widening the control column narrows the label column, so the labels are
        // where that trade shows first. A label wrapping to two lines is what
        // "make the dropdown wider" costs if it is taken too far, and it is
        // invisible to everything but a measurement.
        labels: [...subgroup.querySelectorAll(".rail-field-label")].map((el) => ({
          text: (el.textContent ?? "").trim(),
          height: Math.round(el.getBoundingClientRect().height),
          lineHeight: Math.round(parseFloat(window.getComputedStyle(el).lineHeight) || 0),
        })),
        // The rail must not have been widened past its column to fit any of this.
        railOverflowsX: rail.scrollWidth > rail.clientWidth + 1,
      };
    },

    /**
     * helpTooltipVisibility() opens one help tooltip and measures where the
     * bubble actually lands.
     *
     * This is the failure the layer exists for: a bubble positioned inside the
     * rail's `overflow: auto` body is CLIPPED at the container's edge, and no
     * amount of asserting on HTML strings can see it. So the probe dispatches a
     * real pointerenter, then compares the bubble's painted rectangle against
     * the viewport and against the scrolling container.
     *
     * It also drives the KEYBOARD path, because an explanation only a pointer can
     * reach is one half the users never get.
     */
    async helpTooltipVisibility() {
      await seed("identify");
      const rail = document.querySelector("#identify-rail");
      const help = rail?.querySelector("span.help");
      if (!help) return { error: "no help tooltip rendered in the Identify rail" };
      const iconBtn = help.querySelector("button.help-icon");
      const bubble = help.querySelector("span.help-bubble");
      if (!iconBtn || !bubble) return { error: "a help tooltip with no icon or no bubble" };

      const closedVisible = bubble.getBoundingClientRect().height > 0;

      // The TRIGGER itself, measured before anything is opened. A trigger with no
      // glyph is the defect that made every tooltip in the application
      // undiscoverable: ui.js icon() returns the empty string for a name absent
      // from ICONS, so helpTooltip rendered a button with nothing in it and the
      // whole mechanism worked on an invisible hit area.
      const triggerBox = iconBtn.getBoundingClientRect();
      const glyph = iconBtn.querySelector("svg");
      const trigger = {
        width: Math.round(triggerBox.width),
        height: Math.round(triggerBox.height),
        hasGlyph: !!glyph,
        glyphWidth: glyph ? Math.round(glyph.getBoundingClientRect().width) : 0,
        glyphHeight: glyph ? Math.round(glyph.getBoundingClientRect().height) : 0,
      };

      help.dispatchEvent(new PointerEvent("pointerenter", { bubbles: false }));
      await new Promise((r) => requestAnimationFrame(() => r()));
      const box = bubble.getBoundingClientRect();
      const scroller = rail.querySelector(".card-body") ?? rail;
      const clip = scroller.getBoundingClientRect();

      const opened = {
        trigger,
        closedVisible,
        openedOnHover: box.height > 0 && box.width > 0,
        // Inside the viewport on every side.
        onScreen: box.top >= 0 && box.left >= 0
          && box.bottom <= window.innerHeight && box.right <= window.innerWidth,
        // NOT clipped by the rail's scrolling body: the bubble may extend past
        // it, which is exactly what fixed positioning is for, but it must not be
        // cut off by it. Painted-and-visible at its own centre is the test.
        notClipped: (() => {
          const x = Math.round(box.left + box.width / 2);
          const y = Math.round(box.top + Math.min(8, box.height / 2));
          const hit = document.elementFromPoint(x, y);
          return !!hit && (hit === bubble || bubble.contains(hit));
        })(),
        overflowsScroller: box.bottom > clip.bottom,
      };

      help.dispatchEvent(new PointerEvent("pointerleave", { bubbles: false }));
      await new Promise((r) => requestAnimationFrame(() => r()));
      opened.closedOnLeave = bubble.getBoundingClientRect().height === 0;

      iconBtn.focus();
      help.dispatchEvent(new FocusEvent("focusin", { bubbles: true }));
      await new Promise((r) => requestAnimationFrame(() => r()));
      opened.openedOnFocus = bubble.getBoundingClientRect().height > 0;

      help.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
      await new Promise((r) => requestAnimationFrame(() => r()));
      opened.closedOnEscape = bubble.getBoundingClientRect().height === 0;

      return opened;
    },

    /**
     * scrollRetention() proves a scrolled panel keeps its position across a
     * repaint.
     *
     * This is a visible-only regression, exactly the kind this layer exists for:
     * every state change rewrites the whole shell (state.js setState -> main.js
     * paint -> root.innerHTML), and a freshly written element starts at
     * scrollTop 0. The reported symptom was the Identify left rail snapping back
     * to the top on every tick or drill-down. The fix preserves scroll centrally
     * around the repaint (frontend/scroll.js), so the assertion is simply: scroll
     * the rail, do a real action that forces a repaint, and the offset survives.
     *
     * The action is a genuine change event on a category checkbox, so the view's
     * own listener runs and drives the same setState a user's click would.
     */
    async scrollRetention() {
      await seed("identify");
      const rail = document.querySelector("#identify-rail");
      if (!rail) return { error: "no #identify-rail rendered on the Identify screen" };

      // The rail's scroller named by BEHAVIOUR, not by class: whichever element
      // inside actually overflows. This keeps the probe honest if the markup is
      // ever restructured, and it is the same element the fix has to preserve.
      const overflowing = (root) =>
        [root, ...root.querySelectorAll("*")].find((el) => el.scrollHeight - el.clientHeight > 8);

      const scroller = overflowing(rail);
      if (!scroller) {
        // A rail that fits the viewport cannot demonstrate the bug. Report it
        // rather than passing vacuously, so the harness can say the check could
        // not run instead of claiming a green it did not earn.
        return { scrollable: false, before: 0, after: 0 };
      }

      const target = Math.min(200, scroller.scrollHeight - scroller.clientHeight);
      scroller.scrollTop = target;
      const before = scroller.scrollTop;

      const box = rail.querySelector(".cat-toggle:not([disabled])");
      if (!box) return { error: "no enabled .cat-toggle to force a repaint with" };
      box.checked = !box.checked;
      box.dispatchEvent(new Event("change", { bubbles: true }));
      await settle();

      // Re-query on the fresh DOM: the element measured above was thrown away by
      // the repaint. Its replacement is what must carry the offset now.
      const freshRail = document.querySelector("#identify-rail");
      const freshScroller = freshRail ? overflowing(freshRail) : null;
      const after = freshScroller ? freshScroller.scrollTop : -1;
      return { scrollable: true, before, after };
    },

    /**
     * tooltipVisibility() hovers a real mark and measures where the tooltip ends
     * up.
     *
     * THIS is the assertion that cannot be made without a renderer, and the
     * reason this layer exists. Reported issue 6 was a visible-only bug: the
     * markup had been correct for months and `.pane-body { overflow: auto }` was
     * clipping the tooltip out of existence near the pane's edges. Every
     * string-level test passed throughout.
     *
     * The hover is a real MouseEvent on a real element, so the view's own
     * mouseenter listener runs and does its own measuring, exactly as it would
     * under a user's cursor.
     */
    async tooltipVisibility() {
      await seed("anonymise");
      const pane = document.querySelector("#anonymised-pane");
      const card = document.querySelector("#compare-card");
      if (!pane || !card) {
        return { error: "the Compare card did not render (#compare-card / #anonymised-pane missing)" };
      }
      const all = [...pane.querySelectorAll("mark[data-original]")];
      if (all.length === 0) {
        return { error: "no mark[data-original] in #anonymised-pane, so nothing to hover" };
      }

      // ONLY marks currently visible inside the pane are suggestions. The pane is
      // `overflow: auto` over a long document, so most marks are scrolled out of
      // sight, and a user cannot hover what they cannot see: their
      // getBoundingClientRect is hundreds of pixels below the window, and
      // asserting a tooltip stayed on screen for one of those would be asserting
      // something impossible. What the harness wants is the WORST REACHABLE case.
      const paneBox = pane.getBoundingClientRect();
      const visible = all.filter((m) => {
        const b = m.getBoundingClientRect();
        return b.width > 0 && b.height > 0 &&
          b.top >= paneBox.top - 1 && b.bottom <= paneBox.bottom + 1 &&
          b.left >= paneBox.left - 1 && b.right <= paneBox.right + 1;
      });
      if (visible.length === 0) {
        return { error: "no mark[data-original] is visible inside #anonymised-pane, so nothing is hoverable" };
      }

      // The mark nearest the pane's right edge and the one nearest its bottom
      // edge, plus the first one. Those two edges are precisely where reported
      // issue 6 was visible; the middle of the pane is where it was not.
      const marks = visible;
      const rightmost = marks.reduce((a, b) =>
        b.getBoundingClientRect().right > a.getBoundingClientRect().right ? b : a);
      const lowest = marks.reduce((a, b) =>
        b.getBoundingClientRect().bottom > a.getBoundingClientRect().bottom ? b : a);

      const tip = document.querySelector("#mark-tooltip");
      if (!tip) return { error: "no #mark-tooltip element in the Compare card" };

      const samples = [];
      for (const [edge, mark] of [["first", marks[0]], ["rightmost", rightmost], ["lowest", lowest]]) {
        mark.dispatchEvent(new MouseEvent("mouseleave", { bubbles: false }));
        mark.dispatchEvent(new MouseEvent("mouseenter", { bubbles: false }));
        await settle(120);
        if (tip.hidden) {
          samples.push({ edge, appeared: false });
          continue;
        }
        const t = rect(tip);
        const c = rect(card);
        const p = rect(pane);
        samples.push({
          edge,
          appeared: true,
          text: (tip.innerText ?? "").replace(/\s+/g, " ").trim(),
          // The value the harness will look for IN that text. Reading it off the
          // mark rather than hardcoding it keeps the expectation honest: the
          // tooltip has to show the original belonging to the mark that was
          // hovered, not merely some original.
          markOriginal: mark.dataset.original ?? "",
          insideCard: t.left >= c.left - 1 && t.right <= c.right + 1 &&
            t.top >= c.top - 1 && t.bottom <= c.bottom + 1,
          inViewport: t.top >= 0 && t.left >= 0 &&
            t.right <= innerWidth + 1 && t.bottom <= innerHeight + 1,
          hasSize: t.width > 10 && t.height > 10,
          // The clipping check itself, in two parts.
          //
          // STRUCTURAL: the tooltip must not be a descendant of the scrolling
          // pane. That subtree is what `overflow: auto` clips, and moving the
          // tooltip out of it is the whole fix. It is anchored to the card, so it
          // is perfectly allowed to overhang the PANE's rect; what it must never
          // be is inside the pane's box, at the pane's mercy.
          insidePaneSubtree: pane.contains(tip),
          // VISUAL: whatever the browser finds at the tooltip's centre point has
          // to be the tooltip. This is the check that a rect comparison cannot
          // make, because the rect of a CLIPPED element is still a full-size
          // rect: elementFromPoint respects the ancestor overflow that made
          // issue 6 invisible, and it respects paint order.
          //
          // The tooltip ships with `pointer-events: none` (style.css) so it never
          // eats the click meant for the mark, which also makes it invisible to
          // elementFromPoint. The probe lifts that for the duration of the single
          // hit test and puts it straight back, so the page is left exactly as it
          // was found. Clipping and stacking are unaffected by the swap, which is
          // why the answer still means something.
          paintedOnTop: (() => {
            const saved = tip.style.pointerEvents;
            tip.style.pointerEvents = "auto";
            const hit = document.elementFromPoint(
              Math.round((t.left + t.right) / 2), Math.round((t.top + t.bottom) / 2));
            tip.style.pointerEvents = saved;
            return !!hit && (hit === tip || tip.contains(hit));
          })(),
          paneOverflow: getComputedStyle(pane).overflow,
          tooltipRect: t, cardRect: c, paneRect: p,
        });
        mark.dispatchEvent(new MouseEvent("mouseleave", { bubbles: false }));
      }
      return {
        marks: all.length,
        hoverable: marks.length,
        paneWidth: Math.round(paneBox.width),
        samples,
      };
    },

    /**
     * originLink() hovers a placeholder in the ANONYMISED pane and reports what
     * the ORIGINAL pane does about it.
     *
     * Only a renderer can answer this one. A string test proves the origin spans
     * were emitted and a wiring test proves the class is toggled; neither can say
     * the tinted span is PAINTED and inside the visible part of a pane that is
     * `overflow: auto` over a long document. A tint the user has to scroll to
     * find answers nothing, which is the same failure the mark tooltip had.
     *
     * The mark it hovers is one whose Value was matched under more than one
     * spelling, because the whole point of the feature is that the tint covers
     * the family and not merely the main text.
     */
    async originLink() {
      await seed("anonymise");
      const anon = document.querySelector("#anonymised-pane");
      const origin = document.querySelector("#original-pane");
      if (!anon || !origin) {
        return { error: "the Compare card did not render (#original-pane / #anonymised-pane missing)" };
      }

      const spans = [...origin.querySelectorAll(".value-origin")];
      if (spans.length === 0) {
        return { error: "no .value-origin in #original-pane, so nothing can be tinted" };
      }
      // The placeholder that stands for more than one spelling in the source.
      const families = new Map();
      for (const span of spans) {
        const ph = span.dataset.ph ?? "";
        if (!families.has(ph)) families.set(ph, new Set());
        families.get(ph).add(span.innerText);
      }
      const target = [...families].find(([, forms]) => forms.size > 1)?.[0];
      if (!target) {
        return { error: "no placeholder in #original-pane covers more than one spelling, " +
          "so the seed cannot exercise the family case" };
      }

      const mark = [...anon.querySelectorAll("mark[data-ph]")]
        .find((m) => m.dataset.ph === target);
      if (!mark) return { error: `no mark for ${target} in #anonymised-pane` };

      const mine = spans.filter((sp) => sp.dataset.ph === target);
      const others = spans.filter((sp) => sp.dataset.ph !== target);
      const litBefore = spans.filter((sp) => sp.classList.contains("is-linked")).length;

      mark.dispatchEvent(new MouseEvent("mouseenter", { bubbles: false }));
      await settle(150);

      // Scroll the first tinted span into view before measuring: the pane is
      // taller than its box, and "is it painted" is a question about a span the
      // user can actually see.
      mine[0].scrollIntoView({ block: "center" });
      await settle(120);

      const paneBox = rect(origin);
      const sampleBox = rect(mine[0]);
      // Both READ AS STRINGS here, not held as live CSSStyleDeclarations: the
      // object a browser returns from getComputedStyle keeps tracking the
      // element, so a value read after the mouseleave below would report the
      // resting colour and the tint would look like a rule that never applied.
      const background = getComputedStyle(mine[0]).backgroundColor;
      const plain = others.length > 0 ? getComputedStyle(others[0]).backgroundColor : "";

      const lit = mine.filter((sp) => sp.classList.contains("is-linked")).length;
      const bled = others.filter((sp) => sp.classList.contains("is-linked")).length;

      mark.dispatchEvent(new MouseEvent("mouseleave", { bubbles: false }));
      await settle(120);
      const litAfter = spans.filter((sp) => sp.classList.contains("is-linked")).length;

      return {
        placeholder: target,
        forms: [...families.get(target)],
        spans: spans.length,
        family: mine.length,
        litBefore,
        lit,
        bled,
        litAfter,
        // The tint as the browser resolved it, so a rule that never applied (a
        // typo in the class, a token that does not exist) is visible here as a
        // background identical to an untinted span's.
        background,
        plainBackground: plain,
        // Painted and reachable: the span has a size, sits inside the pane's
        // visible box, and is what the browser finds at its own centre.
        hasSize: sampleBox.width > 0 && sampleBox.height > 0,
        insidePane: sampleBox.top >= paneBox.top - 1 && sampleBox.bottom <= paneBox.bottom + 1,
        paintedOnTop: (() => {
          const hit = document.elementFromPoint(
            Math.round((sampleBox.left + sampleBox.right) / 2),
            Math.round((sampleBox.top + sampleBox.bottom) / 2));
          return !!hit && (hit === mine[0] || mine[0].contains(hit) || hit.contains(mine[0]));
        })(),
        paneRect: paneBox,
        spanRect: sampleBox,
      };
    },

    /**
     * compareSearch() types a needle into EACH pane's own search bar and reports,
     * per pane, whether that pane found it and scrolled its active hit into view.
     *
     * Each pane now carries its own bar in its caption, so the two are driven and
     * measured independently. A string test proves the hit span was emitted; only
     * a renderer can prove the pane scrolled to it rather than leaving it clipped
     * hundreds of pixels below the fold, which is exactly how the mark-tooltip bug
     * reached a build with a green suite.
     */
    async compareSearch() {
      await seed("anonymise");

      // A needle the seeded fixture carries in both panes: the ORIGINAL pane
      // holds the source prose and the ANONYMISED pane the rewritten copy, and
      // both keep the ordinary words between the replacements.
      const NEEDLE = "the";
      const out = { needle: NEEDLE, panes: {} };

      for (const name of ["original", "anonymised"]) {
        const input = document.querySelector(`#compare-search-${name}`);
        if (!input) {
          return { error: `the ${name} pane search box did not render (#compare-search-${name} missing)` };
        }
        input.value = NEEDLE;
        input.dispatchEvent(new Event("input", { bubbles: true }));
        // The search box is debounced so a burst of typing does not repaint per
        // keystroke; wait past that before measuring.
        await settle(400);

        const paneBody = document.querySelector(`#${name}-pane`);
        const bar = document.querySelector(`.compare-search[data-pane="${name}"]`);
        // The active hit is looked up INSIDE this pane, so one pane's search can
        // never be mistaken for the other's.
        const active = paneBody ? paneBody.querySelector(".find-hit.active") : null;
        const readout = bar ? bar.querySelector(".search-readout") : null;
        const next = bar ? bar.querySelector(".search-next") : null;
        const prev = bar ? bar.querySelector(".search-prev") : null;

        let visible = null;
        if (active && paneBody) {
          const a = active.getBoundingClientRect();
          const p = paneBody.getBoundingClientRect();
          visible = {
            // Inside its pane's box: a hit the pane's own overflow has scrolled
            // out of sight is a hit the user cannot see, whatever the DOM says.
            insidePane: a.top >= p.top - 1 && a.bottom <= p.bottom + 1 &&
              a.left >= p.left - 1 && a.right <= p.right + 1,
            inViewport: a.top >= 0 && a.left >= 0 &&
              a.right <= innerWidth + 1 && a.bottom <= innerHeight + 1,
            hasSize: a.width > 0 && a.height > 0,
            activeRect: rect(active), paneRect: rect(paneBody),
          };
        }

        out.panes[name] = {
          hits: paneBody ? paneBody.querySelectorAll(".find-hit").length : 0,
          hasActive: !!active,
          readout: (readout?.innerText ?? "").replace(/\s+/g, " ").trim(),
          // The navigation buttons must be live when there is something to step to.
          nextEnabled: !!next && !next.disabled,
          prevEnabled: !!prev && !prev.disabled,
          visible,
        };
      }

      // The two behaviours a string test cannot see, both exercised on the
      // original pane. The seed left "the" in both boxes; each block below starts
      // by clearing the box, so they run against a known-empty field.
      const box = () => document.querySelector("#compare-search-original");

      // TYPING must survive the debounced repaint. Every keystroke schedules a
      // setState that rewrites the whole shell (main.js paint) and replaces this
      // very input; if the refocus that follows aims at the OLD, now-detached
      // field, the box takes exactly one character and the user has to click back
      // in. So type a word ONE character at a time, pausing PAST the 150 ms
      // debounce between each so the repaint lands mid-word, and confirm the box
      // is still the focused element the whole way through.
      {
        let el = box();
        el.value = "";
        el.dispatchEvent(new Event("input", { bubbles: true }));
        await settle(200);
        box().focus();
        let keptFocus = true;
        for (const ch of "Meridian") {
          el = box();
          el.value += ch;
          el.setSelectionRange(el.value.length, el.value.length);
          el.dispatchEvent(new Event("input", { bubbles: true }));
          await settle(200); // > the 150 ms debounce, so the repaint fires here
          if (document.activeElement !== box()) keptFocus = false;
        }
        const after = box();
        out.typing = {
          keptFocus,
          focusedAfter: document.activeElement === after,
          typedValue: after ? after.value : "",
        };
      }

      // MOVING to the next match must bring it into view, the way a find in any
      // editor scrolls the document to each hit. scrollToActiveHit runs during the
      // render, but scroll.js restores each pane's previous offset AFTER the
      // render, so the scroll has to outlive that restore. Type a needle with many
      // hits, step forward far enough to reach one well below the fold, and check
      // the active hit ends up inside the pane's own box.
      {
        let el = box();
        el.value = "";
        el.dispatchEvent(new Event("input", { bubbles: true }));
        await settle(200);
        el = box();
        el.value = "Meridian";
        el.dispatchEvent(new Event("input", { bubbles: true }));
        await settle(200);
        const bar = document.querySelector('.compare-search[data-pane="original"]');
        const STEPS = 30;
        for (let i = 0; i < STEPS; i += 1) {
          bar.querySelector(".search-next").click();
          await settle(60); // lets the deferred (rAF) scroll land before the next step
        }
        await settle(200);
        const pane = document.querySelector("#original-pane");
        const active = pane ? pane.querySelector(".find-hit.active") : null;
        let insidePane = false;
        let activeRect = null;
        let paneRect = null;
        if (active && pane) {
          const a = active.getBoundingClientRect();
          const p = pane.getBoundingClientRect();
          insidePane = a.top >= p.top - 1 && a.bottom <= p.bottom + 1;
          activeRect = rect(active);
          paneRect = rect(pane);
        }
        out.stepScroll = {
          steps: STEPS, hasActive: !!active, insidePane, activeRect, paneRect,
        };
      }

      return out;
    },
    /**
     * selectionPanel() opens the Compare pane's REPLACE SELECTION panel by
     * selecting text the way a mouse drag does, then reports what its two modes
     * actually render.
     *
     * This is the only layer that renders a native <select>, and the one that can
     * prove the panel opens against a real Selection at all. Both halves matter:
     *
     *   the new-Value mode's type list must offer CATEGORY KEYS. A hand-rolled
     *     dropdown emitted the whole [key, label] pair as each option value, so
     *     nothing was ever pre-selected and every Value declared through it was
     *     filtered out by the engine before validation: no replacement, no
     *     placeholder, no report row, no warning.
     *   the spelling mode's target field must survive being typed into. Its input
     *     handler repainted the whole view, which rebuilt the panel from strings
     *     and destroyed the element mid-word, so the field took exactly one
     *     letter and the mode could not be completed at all.
     *
     * No string test can see either one: the markup was correct throughout.
     */
    async selectionPanel() {
      const s = await store();
      await seed("anonymise");
      // A Value to be a spelling OF, so the pick list has something to offer.
      s.setState({ values: [] });
      s.addValues([{ category: "person_names", mainText: "Marie Duval" }]);
      await settle();

      const pane = document.querySelector("#anonymised-pane");
      if (!pane) return { error: "the Compare card did not render #anonymised-pane" };

      // Select the first text run in the pane, then release the mouse over it:
      // exactly the gesture wireTextSelection listens for.
      const target = [...pane.querySelectorAll("*")]
        .find((el) => el.children.length === 0 && (el.textContent ?? "").trim().length > 2)
        ?? pane;
      const range = document.createRange();
      range.selectNodeContents(target);
      const selection = getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      pane.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
      await settle();

      const card = document.querySelector("#selection-card");
      if (!card) {
        return { error: "selecting text in the anonymised pane opened no #selection-card" };
      }
      // Measured NOW, while both nodes are the ones on screen: every stage click
      // below repaints the view, which replaces them, and a detached node reports
      // a zero rect.
      const compare = document.querySelector("#compare-card");
      const cardRect = rect(card);
      const compareRect = compare ? rect(compare) : null;
      const geometry = {
        insideCompare: !!compareRect &&
          cardRect.left >= compareRect.left - 1 && cardRect.right <= compareRect.right + 1,
        inViewport: cardRect.top >= 0 && cardRect.left >= 0 &&
          cardRect.right <= innerWidth + 1 && cardRect.bottom <= innerHeight + 1,
        hasSize: cardRect.width > 0 && cardRect.height > 0,
        cardRect,
        compareRect,
      };

      // Stage 1 to stage 2, then into the new-Value mode.
      document.querySelector("#btn-selection-replace")?.click();
      await settle(80);
      const valueMode = [...document.querySelectorAll(".selection-mode")]
        .find((r) => r.value === "value");
      if (!valueMode) return { error: "the panel offered no new-Value mode radio" };
      valueMode.checked = true;
      valueMode.dispatchEvent(new Event("change", { bubbles: true }));
      await settle();

      const typeSelect = document.querySelector("#selection-category");
      const optionValues = typeSelect
        ? [...typeSelect.options].map((o) => o.value)
        : [];
      const selectedValue = typeSelect ? typeSelect.value : "";

      // Back to stage 2, then into the spelling mode, and type into its field.
      document.querySelector("#btn-cancel-selection")?.click();
      await settle(80);
      document.querySelector("#btn-selection-replace")?.click();
      await settle(80);
      const spellingMode = [...document.querySelectorAll(".selection-mode")]
        .find((r) => r.value === "spelling");
      if (!spellingMode) return { error: "the panel offered no spelling mode radio" };
      spellingMode.checked = true;
      spellingMode.dispatchEvent(new Event("change", { bubbles: true }));
      await settle();

      const field = document.querySelector("#selection-target");
      if (!field) return { error: "the spelling mode rendered no #selection-target field" };
      // Two keystrokes, because ONE used to be all the field accepted: the first
      // repaint destroyed the element the second was aimed at.
      field.value = "m";
      field.dispatchEvent(new Event("input", { bubbles: true }));
      await settle(80);
      const survivedFirst = document.querySelector("#selection-target") === field;
      field.value = "mar";
      field.dispatchEvent(new Event("input", { bubbles: true }));
      await settle(80);

      const after = document.querySelector("#selection-target");
      // Scoped to the target's own list: the Selected placeholder card renders
      // the same .reassign-pick buttons from the same builder, so an unscoped
      // query would count another card's suggestions as this field's.
      const picks = [...(document.querySelector("#selection-target-list")
        ?.querySelectorAll(".reassign-pick") ?? [])].map((b) => b.dataset.mainText);

      return {
        selectedText: (card.querySelector(".selection-text")?.innerText ?? "").trim(),
        // The panel is positioned against the Compare card, so it must be inside
        // it and on screen: a floating panel clipped away is a dead control.
        ...geometry,
        optionValues,
        selectedValue,
        hasDatalist: !!document.querySelector("datalist"),
        survivedFirstKeystroke: survivedFirst,
        survivedSecondKeystroke: after === field,
        fieldValue: after ? after.value : "",
        picks,
      };
    },

    /**
     * imageTabGeometry() switches step 3 to its IMAGE half over a forty-picture
     * inventory and measures the two things only a renderer can answer.
     *
     * THE PAGE MUST NOT SCROLL. The IMAGE half is a full-width card inside the
     * same fixed-height workspace as everything else, so the list is the scroll
     * owner and the window is not. A screen built to need the page scrollbar
     * satisfies every string test ever written about it.
     *
     * A TILE IS A FIXED-HEIGHT SURFACE. When one card grows, every card below it
     * moves, the browser clamps the grid's scroll offset to the shorter content,
     * and the next repaint snapshots the clamped value: the reader's place is
     * then lost for good. The card carrying five locations and the card carrying
     * one must therefore be exactly the same height, which is why the seed puts
     * both in the same list.
     *
     * There is no bridge here, so every preview request rejects and the cells
     * read "No preview". That is the point rather than a limitation: a
     * placeholder cell must reserve the same space a picture would, or the list
     * reflows the moment a thumbnail lands.
     */
    async imageTabGeometry() {
      await seed("anonymise");
      const s = await store();

      // Forty pictures, so the list genuinely has to scroll. ONE of them appears
      // in five places and one in a single place: those two are the pair the
      // fixed-height rule is measured on.
      const assets = Array.from({ length: 40 }, (_, i) => ({
        id: `ppt/media/image${i}.png`,
        name: `Engagement picture ${i}`,
        format: i % 5 === 0 ? "jpeg" : "png",
        bytes: 26144 + i * 512,
        width: 1200, height: 800,
        companion: "", linked: false,
        occurrences: (i === 0
          ? ["Slide 1", "Slide 4", "Slide 9", "Slide master", "Notes on slide 2"]
          : [`Slide ${i + 1}`]
        ).map((location, ordinal) => ({
          part: "ppt/slides/slide1.xml", ordinal, kind: "picture",
          location, displayCX: 1828800, displayCY: 1219200,
        })),
        // A third of them carry a decision, so the filter counts are real.
        decision: i % 3 === 0 ? { treatment: "box", boxText: "Client logo" } : { treatment: "keep" },
      }));

      s.setState({
        anonymiseTab: "images",
        imageLayout: "tiles",
        imageFilter: "all",
        images: {
          [DOC_NAME]: {
            loading: false, error: null,
            inventory: { applicable: true, assets, warnings: [] },
          },
        },
      });
      await settle();

      const b = document.body;
      const d = document.documentElement;
      const card = document.querySelector("#image-card");
      const list = document.getElementById("image-list");
      if (!card || !list) {
        return { error: "the IMAGE half did not render (#image-card / #image-list missing)" };
      }

      const tiles = [...list.querySelectorAll(".image-tile")];
      if (tiles.length < 2) {
        return { error: `the tiles view rendered ${tiles.length} tiles, so there is no pair to compare` };
      }
      // The seeded pair: the first asset is the one used in five places.
      const many = tiles[0];
      const one = tiles[1];
      const locationText = (tile) =>
        (tile.querySelector(".image-tile-value[title]")?.innerText ?? "").trim();

      const tilesGeometry = {
        tileCount: tiles.length,
        // Rounded, because a fractional layout pixel is not a design failure.
        manyHeight: Math.round(many.getBoundingClientRect().height),
        oneHeight: Math.round(one.getBoundingClientRect().height),
        manyLocation: locationText(many),
        oneLocation: locationText(one),
        tilesListScrolls: list.scrollHeight - list.clientHeight > 1,
        pageScrollsDown: Math.max(b.scrollHeight - b.clientHeight, d.scrollHeight - d.clientHeight),
        pageScrollsAcross: Math.max(b.scrollWidth - b.clientWidth, d.scrollWidth - d.clientWidth),
      };

      // The details view answers the same question about its own rows: they are
      // one line each, so a shared location does not make a taller row.
      s.setState({ imageLayout: "details" });
      await settle();
      const rows = [...document.querySelectorAll("#image-list .grid-row")];
      const detailsList = document.getElementById("image-list");
      const details = {
        rowCount: rows.length,
        manyRowHeight: rows.length ? Math.round(rows[0].getBoundingClientRect().height) : 0,
        oneRowHeight: rows.length > 1 ? Math.round(rows[1].getBoundingClientRect().height) : 0,
        headings: [...document.querySelectorAll("#image-list .col-head")].map((h) => h.innerText.trim()),
        detailsListScrolls: detailsList
          ? detailsList.scrollHeight - detailsList.clientHeight > 1 : false,
        pageScrollsDown: Math.max(b.scrollHeight - b.clientHeight, d.scrollHeight - d.clientHeight),
        pageScrollsAcross: Math.max(b.scrollWidth - b.clientWidth, d.scrollWidth - d.clientWidth),
      };

      // The banner has to stay reachable from the bottom of the list, so it must
      // sit OUTSIDE the scrolling element.
      const banner = card.querySelector(".image-banner");
      const bannerInsideList = !!(banner && list.contains(banner));

      return {
        ...tilesGeometry,
        details,
        bannerInsideList,
        filterChips: [...card.querySelectorAll("[data-imgfilter]")].map((c) => c.innerText.trim()),
        cardInsideViewport: (() => {
          const t = rect(card);
          return t.top >= -1 && t.bottom <= innerHeight + 1;
        })(),
        viewport: { width: innerWidth, height: innerHeight },
      };
    },
  };

  return "installed";
})()
