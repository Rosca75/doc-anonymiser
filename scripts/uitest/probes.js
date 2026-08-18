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
    .replaceAll("+352 621 123 456", "[PHONE_1]");

  const VALUES = [
    { original: "Marie Duval", placeholder: "[PERSON_1]", category: "person_names", count: 21 },
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
          byCategory: { person_names: 61, entity_names: 61, email: 1, phone: 1 },
        }],
        report: {
          level: "medium",
          totalReplacements: 124,
          byCategory: { person_names: 61, entity_names: 61, email: 1, phone: 1 },
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
      categoryCount: 24,
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
     * peer tabs: Smart detection on, Local AI off, and every category checkbox
     * reachable without clicking anything.
     */
    async configureRail() {
      await seed("identify");
      const rail = document.querySelector("#identify-rail");
      if (!rail) return { error: "no #identify-rail rendered on the Identify screen" };
      const toggles = [...rail.querySelectorAll(".route-toggle")];
      const byRoute = (route) => toggles.find((t) => t.dataset.route === route);
      return {
        sections: rail.querySelectorAll(".rail-section").length,
        railTabs: document.querySelectorAll("[data-railtab]").length,
        routes: toggles.map((t) => t.dataset.route),
        smartOn: byRoute("rail-smart")?.checked ?? null,
        localOn: byRoute("rail-local")?.checked ?? null,
        categories: rail.querySelectorAll(".cat-toggle").length,
        // Present in the DOM is not the same as reachable: a checkbox inside a
        // zero-height folded group is not something the user can tick. Measured,
        // not assumed.
        categoriesWithSize: [...rail.querySelectorAll(".cat-toggle")]
          .filter((c) => c.getBoundingClientRect().height > 0).length,
        // The signal control is a tree: one group per signal, its readings one
        // click below. Collapsed the readings are in the DOM at zero height, which
        // a string test reads as "present" and a user reads as absent, so both
        // states are measured. The expanding half is done in its own probe below,
        // because it has to click.
        signalGroups: [...rail.querySelectorAll("#signal-sources .checklist-group")]
          .map((g) => g.dataset.group),
        signalMasters: [...rail.querySelectorAll("#signal-sources .checklist-master")]
          .map((b) => b.dataset.source),
        // The two plain Smart-detection methods share ONE row. "Side by side" is a
        // claim about geometry, so it is answered with geometry: equal tops, and
        // one to the left of the other. Markup order proves neither, since a
        // column-flex parent stacks the same markup.
        methodPairRow: (() => {
          const builtIn = rail.querySelector("#smart-built-in");
          const heuristic = rail.querySelector("#smart-heuristic");
          if (!builtIn || !heuristic) return null;
          const a = builtIn.getBoundingClientRect();
          const b = heuristic.getBoundingClientRect();
          return {
            builtInTop: Math.round(a.top),
            heuristicTop: Math.round(b.top),
            sameRow: Math.abs(a.top - b.top) <= 2,
            heuristicIsToTheRight: b.left > a.right,
            // Neither label may be truncated to nothing by the halving: a pair of
            // ellipses is worse than two rows.
            labelWidths: [...rail.querySelectorAll(".rail-toggle-pair .cat-label")]
              .map((el) => `${(el.textContent ?? "").trim()}: ${el.clientWidth} of ${el.scrollWidth}px`),
            labelsFullyShown: [...rail.querySelectorAll(".rail-toggle-pair .cat-label")]
              .every((el) => el.scrollWidth <= el.clientWidth + 1),
          };
        })(),
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
      // twenty-four category checkboxes and the window is what it is. What is not
      // allowed is a foot the user cannot get to, which is what a paragraph under
      // every control produced.
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
     * signalDerivations() expands a signal row and measures what appears.
     *
     * Collapsed, a signal's readings are in the DOM with zero height: present to a
     * string test and absent to the user. So "expanding shows them" is a claim
     * about geometry, and this answers it with geometry, by clicking the chevron
     * the user clicks and measuring the rows before and after.
     *
     * It also drives the two switches that must NOT be the same control: the
     * chevron folds without ticking, and the master ticks without folding. Two jobs
     * on one element is how a master gets switched by accident.
     */
    async signalDerivations() {
      const s = await store();
      await seed("identify");
      const control = document.querySelector("#signal-sources");
      if (!control) return { error: "no #signal-sources control in the Identify rail" };

      const group = control.querySelector(".checklist-group");
      if (!group) return { error: "no .checklist-group in the signal control" };
      const source = group.dataset.group;

      const rowBoxes = () => [...group.querySelectorAll("input.checklist-box")];
      const visibleRows = () => rowBoxes()
        .filter((b) => b.getBoundingClientRect().height > 0).length;

      const collapsedRows = rowBoxes().length;
      const collapsedVisible = visibleRows();

      const toggle = group.querySelector(".checklist-group-toggle");
      if (!toggle) return { error: "the signal group has no chevron to expand it" };
      toggle.click();
      await settle();

      // The repaint replaced the nodes, so everything below is re-queried.
      const opened = document.querySelector("#signal-sources .checklist-group");
      const openedBoxes = [...opened.querySelectorAll("input.checklist-box")];
      const openedVisible = openedBoxes
        .filter((b) => b.getBoundingClientRect().height > 0).length;

      // Ticking a reading must not fold the group: the checkbox stops the click
      // reaching the head. Measured through the STORE, because a checkbox visually
      // unticking itself proves nothing about what the next run reads.
      const first = openedBoxes[0];
      const derivation = first?.dataset.derivation;
      first?.click();
      await settle();

      const afterTick = document.querySelector("#signal-sources .checklist-group");
      const stillOpen = [...afterTick.querySelectorAll("input.checklist-box")]
        .filter((b) => b.getBoundingClientRect().height > 0).length > 0;
      const stored = s.getState().settings?.signalSuggestionSources?.[source] ?? {};
      const masterAfterTick = afterTick.querySelector(".checklist-master")?.checked ?? null;

      return {
        source,
        collapsedRows,
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
      const sectionLabel = rail.querySelector(".rail-section > .cgroup-body .section-label");

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
     * compareSearch() types a needle present in BOTH Compare panes and reports
     * whether the active hit is actually visible inside its pane.
     *
     * This is the layer that can answer it. A string test proves the hit span
     * was emitted; only a renderer can prove the pane scrolled to it rather than
     * leaving it clipped hundreds of pixels below the fold, which is exactly how
     * the mark-tooltip bug reached a build with a green suite.
     */
    async compareSearch() {
      await seed("anonymise");
      const input = document.querySelector("#compare-search");
      if (!input) {
        return { error: "the Compare search box did not render (#compare-search missing)" };
      }

      // A needle the seeded fixture carries in both panes: the ORIGINAL pane
      // holds the source prose and the ANONYMISED pane the rewritten copy, and
      // both keep the ordinary words between the replacements.
      const NEEDLE = "the";
      input.value = NEEDLE;
      input.dispatchEvent(new Event("input", { bubbles: true }));
      // The search box is debounced so a burst of typing does not repaint per
      // keystroke; wait past that before measuring.
      await settle(400);

      const hits = [...document.querySelectorAll(".find-hit")];
      const active = document.querySelector(".find-hit.active");
      const readout = document.querySelector(".search-readout");
      const panes = {
        original: document.querySelector("#original-pane"),
        anonymised: document.querySelector("#anonymised-pane"),
      };

      const perPane = {};
      for (const [name, pane] of Object.entries(panes)) {
        perPane[name] = pane ? pane.querySelectorAll(".find-hit").length : 0;
      }

      let visible = null;
      if (active) {
        const pane = active.closest(".pane-body");
        if (pane) {
          const a = active.getBoundingClientRect();
          const p = pane.getBoundingClientRect();
          visible = {
            // Inside its pane's box: a hit the pane's own overflow has scrolled
            // out of sight is a hit the user cannot see, whatever the DOM says.
            insidePane: a.top >= p.top - 1 && a.bottom <= p.bottom + 1 &&
              a.left >= p.left - 1 && a.right <= p.right + 1,
            inViewport: a.top >= 0 && a.left >= 0 &&
              a.right <= innerWidth + 1 && a.bottom <= innerHeight + 1,
            hasSize: a.width > 0 && a.height > 0,
            activeRect: rect(active), paneRect: rect(pane),
          };
        }
      }

      // The navigation buttons must be live when there is something to step to.
      const next = document.querySelector(".search-next");
      const prev = document.querySelector(".search-prev");

      return {
        needle: NEEDLE,
        hits: hits.length,
        perPane,
        hasActive: !!active,
        readout: (readout?.innerText ?? "").replace(/\s+/g, " ").trim(),
        nextEnabled: !!next && !next.disabled,
        prevEnabled: !!prev && !prev.disabled,
        visible,
      };
    },
  };

  return "installed";
})()
