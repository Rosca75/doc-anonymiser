// copy.js, the single home for user-visible strings.
//
// Keeping copy in one module gives the style guard (copy.test.js) and a
// future i18n pass exactly one place to look. Style rules: no em dashes, no "+" as a stand-in for "and", no unexplained
// jargon such as "PII" without an example, full sentences.

// WORKFLOW: the step bar under the permanent top menu (, relaid
// out for). The separate "Anonymisation workflow" title is
// gone; the back link that replaced it is `backToFlow`, and it is what the
// mock-ups show in that slot.
export const WORKFLOW = {
  // The accessible name of the bar. It is no longer rendered as visible text,
  // so it exists only for assistive technology.
  title: "Anonymisation workflow",
  backToFlow: "Anonymise Flow",
  backToFlowTitle: "Back to the start of the flow",
};

// CARDS: the heading and the one explaining sentence for every card that
// carries a screen's main content. The subtitle is where the deleted
// STEP_BANNERS copy went: same sentence, next to the heading it explains.
export const CARDS = {
  documents: {
    title: "Documents",
    subtitle: "Add the documents you want to anonymise. Your files are only read, never changed.",
  },
  preview: {
    title: "Preview",
    caption: "WORKING FORM",
  },
  configure: {
    title: "Configure",
    subtitle: "Choose what to hide, and how hard to look.",
  },
  identify: {
    title: "Identify",
    subtitle: "Tell the app which values to replace. Nothing is replaced until you accept it.",
  },
  run: {
    title: "Run anonymisation",
    subtitle: "Run the passes, then check the result side by side.",
  },
  compare: {
    title: "Compare",
  },
  images: {
    title: "Pictures",
    subtitle: "Decide what happens to each picture. Kept pictures leave the document unchanged.",
  },
  export: {
    title: "Export",
    subtitle: "Save the anonymised copies and, if you need it, the re-identification key.",
  },
};

// FOOTER: the permanent strip under every screen, matching
// the Claude Design mockup's welcome page. version is the same string as
// wails.json productVersion; there is no bound Go method to read it live
// yet, so it is a plain literal here until that wiring exists.
export const FOOTER = {
  version: "Version 0.1.0",
  localProcessing: "Local processing only",
};

// NAV: navigation copy. Going BACK through the wizard
// resets the step being left, so the user is asked first, in words that
// say what will and will not be lost.
export const NAV = {
  // The visible step names, for the footers and the confirmation sentence.
  // Kept here rather than reusing STEP_LABELS because those carry the step
  // number. Cross-checked against WIZARD_STEPS by ../step_parity_test.go.
  stepNames: {
    import: "Import",
    identify: "Identify",
    anonymise: "Anonymise",
    export: "Export",
  },
  /** back(step) is a screen footer's "Back to X" link label. */
  back(step) {
    return `Back to ${NAV.stepNames[step] ?? step}`;
  },
  /** next(step) is a screen footer's primary button label, shouted because
   *  it is the one loud element on the screen. */
  next(step) {
    return `CONTINUE TO ${(NAV.stepNames[step] ?? step).toUpperCase()}`;
  },
  // Going BACK through the wizard resets the step being left, so the user is
  // asked first, in words that say what will and will not be lost. This is an
  // in-app modal now, not a native confirm, so the
  // question is a title plus a body rather than one cramped line.
  backConfirmTitle(step) {
    return `Reset the ${NAV.stepNames[step] ?? step} step?`;
  },
  backConfirmBody(step) {
    const name = NAV.stepNames[step] ?? step;
    return `Going back clears everything the ${name} step owns, so you start it fresh.`;
  },
  backConfirmLabel: "Go back and reset",
};

// Home page copy (, rewritten for, sidebar
// added in).
//
// The body is an ARRAY of three paragraphs, rendered one <p> each by
// views/home.js. The three cover, in order: who stays in control, the two
// ways sensitive information is found, and where the processing happens.
// They replace the former single lede plus three feature panels, which said
// the same three things twice.
//
// `steps` feeds the sidebar next to the hero: a plain-language walk through
// the wizard so a first-time user knows what they are signing up for before
// they click Anonymise documents.
//
//  cut it from five entries to four, matching the wizard.
// Unlike before, the labels here ARE the wizard's own step names: the sidebar
// used to say "Configure / Identify" for steps the wizard called "Values /
// Run", which meant the landing page taught a vocabulary the application then
// did not use. The bodies stay in plain language, longer than the step bar's
// one-word labels can be.
export const HOME = {
  headline: "Anonymise your documents safely",
  body: [
    "doc-anonymiser is a simple yet powerful application that helps you anonymise documents directly from your workstation. It replaces names, personal details and other sensitive information with consistent placeholders, making your documents safer to share or process.",
    "You remain in control throughout the process. Choose from a wide range of predefined patterns or use AI-powered discovery to identify information that may need to be anonymised. You can then review what has been detected and decide which data to replace.",
    "Depending on your security and confidentiality requirements, you can run the entire process locally or connect to an AI endpoint.",
  ],
  stepsTitle: "The four steps",
  steps: [
    { label: "Import", body: "Drop in .docx, .pptx, .xlsx, .pdf, .csv, .md or .txt files. Your originals are only ever read, never changed." },
    { label: "Identify", body: "Pick a preset and fine-tune the 24 detection categories, then review every suggested value. Nothing is replaced until you accept it." },
    { label: "Anonymise", body: "Run the passes and check the result side by side, with every replacement mapped back to its original." },
    { label: "Export", body: "Save the anonymised copies, the report and the re-identification key." },
  ],
  docsLink: "Read the documentation",
};

// The documentation placeholder page was retired by: real
// documentation now lives in frontend/docs/index.html and opens in its own
// window, so there is no in-app docs screen and no placeholder string.

// Import screen copy.
export const IMPORT = {
  addFiles: "ADD FILES",
  dropTitle: "Drag files here to import",
  dropHint: "or click to browse. Accepts .txt, .csv, .md, .docx, .pptx, .xlsx and .pdf. Your originals are only ever read, never changed.",
  experimentalTooltip: "PDF text extraction is experimental. Review the preview carefully.",
  removeTooltip: "Remove from session",
  noSelection: "no document selected",
  previewEmpty: "Select a document on the left to preview its working form.",
  previewTruncated: "Preview truncated to the first 5 000 lines. The full document is still processed and exported.",
  // The footer hint. On an empty screen it says what to do rather than
  // reporting "0 files", which is the difference between a footer that helps
  // and one that states the obvious.
  hintEmpty: "Add at least one document to continue",
  // "Start over" is the clean-sheet escape hatch. Unlike "start a new batch" on
  // Export, which keeps your settings and the placeholder registry, this resets
  // EVERYTHING to defaults, for a completely separate anonymisation on new files
  // that must not inherit anything from the last one.
  startOver: "START OVER",
  startOverTooltip: "Clear everything and reset to defaults",
  startOverTitle: "Start over from a clean sheet?",
  startOverBody: "This removes all imported documents, clears every detected value and the placeholder registry, forgets removed values, and resets your settings, document country, patterns and never anonymise list to their defaults. It cannot be undone. Use it only when you want to begin a completely separate anonymisation.",
  startOverConfirm: "Reset everything",
  startOverDone: "Everything was reset. Add new files on the Import step to begin a fresh anonymisation.",
};

// Configure step copy. Plain language: no "PII", no
// abbreviations without an example, full sentences.
export const CONFIGURE = {
  // The Configure panel keeps VISIBLE LABELS short and moves every explanation
  // into a help tooltip. A paragraph under each control is useful the first time
  // and, on every visit after that, is what pushes the panel taller than the
  // window and buries the controls actually in use. Only DYNAMIC information
  // stays inline: a validation error, the live confidence value, an active count,
  // Ollama's availability, the run status.
  //
  // Every `...Help` key below is tooltip text. Nothing renders it as a paragraph.
  presetHelp: "Start from a preset, then adjust the checkboxes if you need to. Changing any checkbox switches the preset to Custom.",
  groupContact: "Contact and account details",
  // The rail groups by TRIGGER, the user's own model of how a value is found
  // so these are the names of the three ways it happens.
  // groupNames was "Names", which said nothing about where the values came
  // from and sat over a list that also held the user's own regexes.
  groupDetected: "Auto detected values",
  groupDeclared: "Your own patterns",
  groupThorough: "Only for thorough anonymisation",
  // The route's own explanation, plus ONE sentence pointing at the Documentation
  // window. Ollama's own settings decide how fast a scan runs and the app cannot
  // read or change them, so the guidance lives in a page with room for it rather
  // than in a tooltip that would then carry two subjects.
  useAIHelp: "A language model running on this machine reads the documents and suggests Values. Nothing leaves your computer, and nothing it finds is replaced until you accept it. The Documentation window has a note on Ollama settings that affect how fast a scan runs.",
  contextSizeHelp: "Higher values let the model read longer documents at once but use more memory.",
  // The reply-format switch, in OUTCOME terms. The mechanism (a JSON schema
  // constraining which keys the reply must carry) is not what a business user is
  // deciding about; what they are deciding about is a little more recall against
  // roughly twice the wait.
  strictFormatHelp: "Makes the model answer for every category instead of only the ones it thought of. Sometimes finds a little more, and usually takes about twice as long.",
  // The detail level, in OUTCOME terms, and deliberately WITHOUT a promise of
  // speed. Measured on both reference documents, larger slices did not reliably
  // take less time on a model that finds anything: two runs of the same setting
  // varied by more than the two settings varied from each other. What larger
  // slices reliably do is send fewer requests and, on a small model, find
  // nothing, so those are what the sentence says. The request count itself is
  // dynamic and belongs in the read-out beside the control, not here.
  detailLevelHelp: "The local AI reads your document in slices. Smaller slices find the most values and send more requests. Larger slices send fewer requests, and on a small model they can miss values completely. Whether fewer requests is quicker depends on your model and your machine.",
  aiOffTooltip: "Local AI is turned off. Turn it on with the switch on the Local AI section of Configure.",
  allowHint: "Terms in this list survive every pass, even when they also appear as names to replace.",
  // the group that surfaces the recognizers.
  groupTechnical: "Payment, tax and technical identifiers",
  // the per-group bulk buttons.
  selectAll: "Select all",
  deselectAll: "Deselect all",
  // the detection-confidence control. Plain language, with
  // the two thresholds that actually change something spelled out.
  confidenceTitle: "Detection confidence",
  confidenceLabel: "Minimum confidence",
  confidenceHelp: "Every detection carries a score for how certain it is. Anything below the minimum you set here is left alone. Keep it at 0 to replace everything that is found, which is how the application behaves by default.",
  // What the floor does at each position is views/identifyrail.js
  // confidenceEffect(), and it is described there rather than here: the setting
  // is a floor on a SCORE, and copy calling it a rule about who proposed a
  // value would describe something the engine does not do.
};

/**
 * categoryLabels(examples) returns CATEGORY_LABELS with the country-dependent
 * examples replaced.
 *
 * @param {Record<string,string>} examples category key to example string, as
 *   countries.js examplesFor() produces
 * @returns {Record<string,string[]>} a fresh label table, never a mutated one
 */
export function categoryLabels(examples = {}) {
  const out = { ...CATEGORY_LABELS };
  for (const [key, example] of Object.entries(examples)) {
    const existing = out[key];
    if (!existing) continue; // an example for a category with no label is a no-op
    out[key] = [existing[0], `For example ${example}`];
  }
  return out;
}

// Identify RAIL copy: the four tabs and the Scope tab's
// section labels. The category labels and the confidence copy stay in CONFIGURE
// below, which is where they were and where the parity guard looks.
export const RAIL = {
  tabSmart: "Smart detection",
  tabLocalAI: "Local AI",

  // the three routes are switchable sections, not tabs. Scope stopped
  // being a section of its own because it is the scope OF smart detection.
  smartHelp: "Finds Values on this machine, without any AI: application-provided patterns for structured signals, evidence taken from those signals, and rules about how names are written. It uses the categories chosen below.",
  smartTuning: "Discovery strictness",
  smartTuningHelp: "Heuristic discovery guesses which words are names from how they are written, so it always suggests some things that are not names. These settings decide how strict it is. Set them all to zero, and untick the box, to see everything it can find.",

  // Smart detection's three methods, as controls at the top of the section.
  builtInPatterns: "Built-in patterns",
  builtInPatternsHelp: "Application-provided patterns for structured signals: emails, phone numbers, VAT numbers, IBANs and the rest. They MATCH AND REPLACE the signal itself, and they are the only thing here that acts without review.",
  heuristicDiscovery: "Heuristic discovery",
  heuristicDiscoveryHelp: "Finds recurring names from spelling, context and frequency, and suggests them for review.",

  // The signal-based control: a drill-down ON the category row of the signal it
  // reads, opening that signal's individual READINGS. It switches whether a
  // built-in pattern match may be used as EVIDENCE to find related text, and
  // nothing else. This is the label of the button that opens the readings.
  signalSuggestions: "Signal-based suggestions",
  signalSuggestionsHelp: "A matched signal can also be evidence about text written elsewhere: an email address names a person and an organisation, and both may appear in prose in another file. Those become Suggestions you accept or reject. Clearing a reading here stops those suggestions and does NOT stop the signal itself being anonymised, which is governed by Built-in patterns and the signal's own category.",
  // One label per engine signal source, enforced by ../detection_parity_test.go.
  // The control is built from the identifier list, so an unlabelled source would
  // render as a checkbox named after a JSON key.
  signalSourceLabel: {
    email: "Email addresses",
    url: "Web addresses",
  },
  // One label, one "where it reads it from" and one explanation per engine
  // DERIVATION, enforced by the same guard. The label says WHAT is suggested and
  // the detail says WHERE the evidence comes from, because the two together are
  // what the user is deciding between.
  signalDerivationLabel: {
    "email.person": "Person names",
    "email.organisation": "Organisation names",
    "url.organisation": "Organisation names",
  },
  signalDerivationFinds: {
    "email.person": "from the part before the @",
    "email.organisation": "from the domain",
    "url.organisation": "from the website domain",
  },
  signalDerivationHelp: {
    "email.person": "Reads the part before the @ as a person's name, and suggests that name where it appears in prose elsewhere in the batch. Role mailboxes such as info@ and single-token handles derive nothing. Switching this off stops those suggestions and does not stop the address itself being anonymised.",
    "email.organisation": "Reads the domain as an organisation's name, and suggests that name where it appears in prose elsewhere in the batch. Public mail providers and public-suffix labels derive nothing. Switching this off stops those suggestions and does not stop the address itself being anonymised.",
    "url.organisation": "Reads a website's domain as an organisation's name, and suggests that name where it appears in prose elsewhere in the batch. A document that carries no email address often still prints its parties' websites, and the domain is frequently the short form of the name. The page path derives nothing. Switching this off stops those suggestions and does not stop the website itself being anonymised.",
  },
  signalSourcesOff: "Off",
  /**
   * signalDerivedFrom(sourceLabel) heads the opened drill-down, naming the signal
   * the readings under it are read FROM. The panel hangs off the signal's own row,
   * so the heading confirms which row opened rather than introducing the feature
   * again: that explanation is one hover away, in signalSuggestionsHelp.
   */
  signalDerivedFrom(sourceLabel) {
    return `Suggestions derived from ${String(sourceLabel).toLowerCase()}`;
  },
  /**
   * signalDerivationCount(n) is the opened panel's read-out: how many of this
   * signal's readings are on. A COUNT rather than their names, because the names
   * are the rows immediately below it, and repeating them there says nothing the
   * user cannot already see. Zero reads as "Off", the one state worth naming: it
   * means this signal derives nothing at all.
   */
  signalDerivationCount(n) {
    if (n === 0) return this.signalSourcesOff;
    return `${n} active`;
  },
  routeOn: "On",
  routeOff: "Off",
  /** routeSwitchLabel(title) is the accessible name of a section's switch. */
  routeSwitchLabel(title) {
    return `Turn ${title} on or off`;
  },

  country: "Document country",
  countryHelp: "The phone, VAT and national identification examples follow this country's formats, and the matching national identifiers are switched on. It changes nothing else about how detection works.",
  preset: "Preset",
  categories: "Categories",
  categoriesHelp: "The structured signals built-in pattern matching looks for. They are matched and replaced directly, without review. Switching Built-in patterns off leaves the selection intact and skips the pass.",

  //  split the category list in two blocks: the regex-triggered
  // patterns (found by shape) and the entity categories (found by name). Both
  // blocks live in the Smart detection section because the category selection is
  // the ONE scope the whole pipeline reads (CLAUDE.md §5): rendering a second
  // copy of the same checkboxes inside Local AI would give one setting two
  // controls, and the second copy would be folded shut and unreachable anyway.
  valuesAuto: "Auto detected values",
  valuesAutoHelp: "These categories are used by every detection route you switch on.",
  // What the Local AI section says INSTEAD of a second copy of the checkboxes.
  localValuesHelp: "Local AI looks for the same categories chosen under Smart detection above. Switching this route on adds a model pass over them; it does not change what is selected.",

  /** activeCount(n, total) is the rail heading's read-out. */
  activeCount(n, total) {
    return `${n} of ${total} categories on`;
  },

  ollamaDetected: "Ollama detected",
  ollamaMissing: "Ollama not detected",
  hostLocked: "The host is locked to 127.0.0.1; only the port can be changed.",
  port: "Port",
  model: "Model",
  contextSize: "Context",
  // Short, as every rail label is: the explanation is the tooltip beside it.
  strictFormat: "Answer every category",
  // The slice-size dial. The label names the QUESTION and the options name what
  // each answer DOES, so neither has to carry the explanation the tooltip holds.
  //
  // The options name the slice size rather than a speed. "Faster" is the engine's
  // identifier for the larger slices, and it would be a promise here: measured on
  // both reference documents, larger slices were not reliably quicker, while they
  // reliably send fewer requests. A label states what a control does.
  detailLevel: "Detail",
  detailLevelOptions: {
    thorough: "Smaller slices",
    faster: "Larger slices",
  },

  /**
   * scanEstimate(requests) is the cost of the current scope and detail level,
   * shown BEFORE the user pays it. It is dynamic, so it stays inline as a
   * read-out rather than going in the tooltip.
   *
   * It names requests rather than a time, because how long a request takes
   * depends on the model and the machine; what the last scan actually cost is
   * lastScan's job, and the two read side by side.
   */
  scanEstimate: (requests) =>
    `This scope needs ${requests} request${requests === 1 ? "" : "s"}.`,
  noModels: "(no models found)",
  reprobe: "Check again",

  /**
   * lastScan(requests, secondsEach, silent, truncated) reports what the local AI
   * did on the last run, measured on this machine and this document.
   *
   * The silent count is only mentioned when there IS one: a scan where most
   * requests find nothing is normal, so the clause exists to explain a
   * disappointing result rather than to worry the reader about a good one. When
   * every request came back empty, saying so is the whole point, because
   * "0 values found" otherwise reads as a clean document.
   *
   * The truncated count sits BESIDE it and is never folded into it, because the
   * two say opposite things: a silent request found nothing, a cut-off one
   * found more than it was allowed to finish listing. Only the second means
   * values may be missing from pages that did return some.
   */
  lastScan: (requests, secondsEach, silent, truncated = 0) => {
    const each = secondsEach >= 10
      ? `${Math.round(secondsEach)}s`
      : `${(Math.round(secondsEach * 10) / 10)}s`;
    let out = `Last scan: ${requests} request${requests === 1 ? "" : "s"}, about ${each} each.`;
    if (silent > 0) {
      out += silent === requests
        ? " The model returned nothing for any of them."
        : ` ${silent} returned nothing.`;
    }
    if (truncated > 0) {
      out += ` ${truncated} ran out of room, so some values may be missing.`;
    }
    return out;
  },

  // Local-AI SCAN SCOPE. Handing a whole document to a small local model is too
  // much, so the user can aim the scan at one document and a range of its own
  // units (pages, slides, rows or lines). This scope applies to the Local AI
  // route only; Smart detection always reads everything because it is cheap.
  scopeHeading: "Scan scope",
  scopeHelp: "The local AI reads only what you point it at. Scanning one document, or a few pages of one, keeps a small model focused and the pass quick.",
  scopeAllDocs: "All documents (whole)",
  scopeDoc: "Document",
  scopeEntireDoc: "Entire document",
  scopeSpecificPages: "Specific pages",
  scopePagesPlaceholder: "14, 12-15, 18-20",
  /** scopeUnitWord(unit) is the singular unit noun for the range labels. */
  scopeUnitWord(unit) {
    return unit || "unit";
  },
  /** scopePagesLabel(unit) labels the free-text page set for the unit at hand. */
  scopePagesLabel(unit) {
    const u = unit || "unit";
    return `Which ${u}s`;
  },
  /**
   * scopeReadout(n, unit) says how many units the current spec resolves to, so
   * the user sees the size of what they are about to hand the model before they
   * run it. Zero reads as "nothing yet" rather than a bare "0".
   */
  scopeReadout(n, unit) {
    const u = unit || "unit";
    if (!n) return `No ${u}s selected yet`;
    return `${n} ${u}${n === 1 ? "" : "s"} selected`;
  },
  /**
   * scopePagesError(token) names the first token that is neither a number nor a
   * range, so the fix is obvious. Stated as a present-tense rule, not a warning
   * about a wide range: a malformed spec is a mistake, not a cost.
   */
  scopePagesError(token) {
    return `"${token}" is not a page or a range like 12-15.`;
  },
  /** scopeDocOption(name, count, unit) is one entry in the document dropdown. */
  scopeDocOption(name, count, unit) {
    const u = unit || "unit";
    return `${name} (${count} ${u}${count === 1 ? "" : "s"})`;
  },

  // The Load profile section: a plain (switch-less) section at the foot of the
  // rail. Load restores a saved profile; Save writes one, but only once a run
  // has produced a registry worth preserving.
  profileTitle: "Load profile",
  profileHelp: "Reuse a saved setup: Values, the never anonymise list, patterns and the placeholder registry, so a follow-up batch reuses the same placeholders.",
  profileLoad: "Load",
  profileSave: "Save",
  profileSaveDisabled: "Run the anonymisation once before saving a profile, a profile carries the placeholder registry.",
  profileLoadDone: "Profile loaded: Values, allowlist and patterns restored.",
  profileSaveDone: "Profile saved. A follow-up batch will reuse these placeholders.",
};

// Values step copy: the smart-detection tuning block
//  and the suggestions table.
export const VALUES = {
  // Smart detection tuning. It moved to the Identify RAIL's own tab
  // which RAIL.tabSmart titles, so the block no longer
  // needs a heading of its own.
  smartMinLength: "Shortest value",
  smartMinLengthHelp: "Suggestions shorter than this many letters are skipped.",
  smartMinOccurrences: "Fewest occurrences",
  smartMinOccurrencesHelp: "How often a value must appear before it is suggested. 1 means once is enough.",
  smartCommonWords: "Skip ordinary words",
  smartCommonWordsHelp: "Ignores month names, weekdays and common sentence openers, which are capitalised without being names.",
  smartMinConfidence: "Minimum certainty",
  smartMinConfidenceHelp: "Higher values keep only the strongest suggestions, such as a name followed by a company form or introduced by a title.",
  smartStrictness: "How much to trust",
  smartStrictnessHelp: "Strict keeps only suggestions with strong evidence, such as a company form, a title or a matching email address. Lenient also shows the weakest guesses, which pairs well with a low minimum certainty. Balanced is the default.",
  smartStrictnessLenient: "Lenient: show weak guesses too",
  smartStrictnessBalanced: "Balanced (recommended)",
  smartStrictnessStrict: "Strict: strong evidence only",

  // The suggestions table's search box and its two sort tooltips. The column
  // HEADINGS moved to WORKSPACE, where they are upper-case
  // because they sit in a header strip rather than above a form.
  searchPlaceholder: "search values",
  // The ✕ inside every search field. One string for all three, because it is one
  // control: a field the user can fill and cannot empty in one gesture is the
  // same oversight repeated. It is deliberately NOT the values toolbar's
  // "Clear all", which deletes every Value; this empties a text field and
  // carries no label, so the two cannot be confused on screen.
  clearSearch: "Clear the search",
  sortValueHint: "Sort by value, A to Z or Z to A.",
  sortCountHint: "Sort by how often the value occurs.",
  noMatchingSuggestions: "No suggestion matches the current search and type filter.",
};

// The never-anonymise editor. The list wins over every pass,
// which is why the hint says so rather than describing the control.
export const ALLOWLIST = {
  label: "A term to never anonymise",
  placeholder: "add a term, e.g. CSSF",
  add: "Add",
  importCSV: "Import CSV",
  template: "Template",
  clearAll: "Clear all",
  clearAllTitle: "Remove every term from this list",
  /** clearAllConfirm(n) is the in-app confirm before emptying the list. */
  clearAllConfirm(n) {
    return `Remove all ${n} term${n === 1 ? "" : "s"} from the never-anonymise list? Nothing will be protected from the passes afterwards, but you can add terms again at any time.`;
  },
  /** clearedN(n) reports the result of Clear all. */
  clearedN(n) {
    return `${n} term${n === 1 ? "" : "s"} removed from the never-anonymise list.`;
  },
  remove: "Remove from this list",
  empty: "The list is empty, so nothing is protected from the passes.",
  /** alreadyThere(t) explains an add that changed nothing. */
  alreadyThere(t) {
    return `${t} is already on the list.`;
  },
  /** imported(read, added) answers the question a user has after importing a
   *  list they already partly had. */
  imported(read, added) {
    return `${read} term${read === 1 ? "" : "s"} read, ${added} new.`;
  },
  templateSaved: "Template saved. Fill in one term per row and import it back.",

  // The terms the DOCUMENTS define about themselves. They are shown here, and
  // not merged into the list above, because they are a different kind of entry:
  // the user typed the list above, and the application read these out of a file.
  // Showing them is the point. A suppression the user cannot see is one they
  // cannot lift, and this is the largest thing standing between a review list
  // and a usable one.
  definedTitle: "Terms your documents define",
  definedHint: "A contract that introduces a phrase as its own vocabulary is telling you the phrase is not a client identity, so these are not suggested. Remove any entry to have it suggested again.",
  definedEmpty: "None yet. Run detection and any phrase your documents define will be listed here.",
  /** definedIdiom(idiom) names the drafting shape that introduced a term. */
  definedIdiom(idiom) {
    if (idiom === "means") return "defined with \u201cmeans\u201d";
    if (idiom === "parenthetical") return "defined in brackets";
    return "defined by the document";
  },
  definedRemove: "Stop suppressing this term",
  /** definedForgotten(t) reports the result of removing one. */
  definedForgotten(t) {
    return `${t} can be suggested again.`;
  },
};

// Identify WORKSPACE copy: the four tabs, the suggestions
// table, the value cards and the pattern rows.
export const WORKSPACE = {
  tabsLabel: "Identify sections",
  tabLabels: {
    suggestions: "Suggestions",
    values: "My values",
    allow: "Never anonymise",
    // The two pattern tabs are named after WHO wrote the pattern, because that
    // is the whole difference between them: the built-in ones ship with the
    // application and are read-only, the custom ones are the user's own.
    builtin: "Built-in patterns",
    patterns: "Custom patterns",
  },

  /** subtitle(waiting, accepted) is the live count beside the heading. */
  subtitle(waiting, accepted) {
    const w = `${waiting} suggestion${waiting === 1 ? "" : "s"} waiting`;
    const a = `${accepted} value${accepted === 1 ? "" : "s"} accepted`;
    return `${w}, ${a}`;
  },

  // Run detection. One button now, so its tooltip has to say what the run will
  // actually include, which depends on whether the local AI can run.
  runDetection: "Run detection",
  runOffline: "Reads every imported document and suggests values, without any AI.",
  runWithAI: "Reads every imported document twice: the offline pass, then the local AI. Nothing leaves your computer.",
  runNeedsRoute: "No detection route is on. Turn on Smart detection or Local AI in Configure.",
  runNeedsDocuments: "Import at least one document first.",
  cancel: "Cancel",
  // The progress caption. It is assembled from these parts rather
  // than written as one sentence, because a run that feels stuck raises three
  // separate questions: which route is this, where in the batch is it, and
  // where inside this file.
  /** phaseName(phase) turns an engine route token into words. */
  phaseName(phase) {
    if (phase === "ai") return "Local AI";
    if (phase === "smart") return "Smart detection";
    return "Starting";
  },
  /** fileOf(file, index, total) is the position in the batch. */
  fileOf(file, index, total) {
    return `${file} (${index} of ${total})`;
  },
  /** chunkOf(index, total) is the position inside one chunked AI scan. A long
   *  document used to sit on an unchanging caption for minutes. */
  chunkOf(index, total) {
    return `part ${index} of ${total}`;
  },
  /** elapsed(seconds) is how long the run has been going. */
  elapsed(seconds) {
    if (seconds < 60) return `${seconds}s`;
    return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  },
  /** detectionDone(n) reports the run's result. */
  detectionDone(n) {
    if (n === 0) return "Detection finished. Nothing new to review.";
    return `Detection finished. ${n} new suggestion${n === 1 ? "" : "s"} to review.`;
  },
  /** detectionCancelled(n) reports a run the user stopped. Partial findings
   *  are kept, so the sentence says what was kept rather than just "stopped". */
  detectionCancelled(n) {
    if (n === 0) return "Detection cancelled. Nothing was added.";
    return `Detection cancelled. ${n} suggestion${n === 1 ? "" : "s"} found before it stopped ${n === 1 ? "was" : "were"} kept.`;
  },
  /** skippedNotice(name, reason) names a file a route could not read, and why.
   *  Go writes the reason, because Go is what knows the limit. */
  skippedNotice(name, reason) {
    return `${name}: ${reason}`;
  },

  // The suggestions table.
  reviewHint: "Nothing is replaced until you accept it. The bulk buttons apply to the rows shown below, so a search or a filter limits them too.",
  colValue: "VALUE",
  colCount: "COUNT",
  colActions: "ACTIONS",
  allTypes: "ALL TYPES",
  allMethods: "ALL METHODS",
  filterTypeTitle: "Filter by type",
  filterMethodTitle: "Filter by which discovery method found it",
  retypeSuggestionTitle: "Change the type before accepting",
  accept: "Accept",
  reject: "Reject",
  acceptAllShown: "Accept all shown",
  rejectAllShown: "Reject all shown",
  bulkScopeHint: "Applies to the rows shown below, so a search or a filter limits it too.",
  /** acceptedN / rejectedN report a bulk action's result. */
  acceptedN(n) {
    return n === 0 ? "Nothing to accept." : `${n} value${n === 1 ? "" : "s"} accepted.`;
  },
  /** readyToReplace(n) is the Identify footer's sentence when the review is
   *  done: it counts ACCEPTED values, because that is what the next step acts
   *  on. Zero is a real answer, not an empty one. */
  readyToReplace(n) {
    return `${n} value${n === 1 ? "" : "s"} ready to replace`;
  },
  /** reviewGate(n) is the Identify footer's sentence while suggestions are
   *  still waiting, and the tooltip on the disabled CONTINUE. It is the REASON
   *  the move is refused (state.js canGoTo rule 2), so it names the one action
   *  that clears the gate rather than only counting what is left. The bulk
   *  button acts on the rows in view, so the sentence says so: a search or a
   *  filter would otherwise leave suggestions behind and the gate shut with no
   *  visible cause. */
  reviewGate(n) {
    return `${n} suggestion${n === 1 ? "" : "s"} still waiting. ` +
      `Accept or reject each one to continue, or clear any filter and use "Reject all shown".`;
  },
  rejectedN(n) {
    return n === 0 ? "Nothing to reject." : `${n} suggestion${n === 1 ? "" : "s"} rejected.`;
  },
  // WHICH METHODS found a Suggestion or Value. Shown as a set, because two
  // methods agreeing is worth seeing: the user judging a Suggestion is deciding
  // how much to trust it, and "two methods found this" is a different position
  // from either alone. One label per engine discovery method, enforced by
  // ../detection_parity_test.go.
  //
  // Built-in and custom pattern matching are deliberately absent: they produce
  // direct matches applied without review, never Suggestions, so naming them
  // would promise rows the table can never show.
  methodLabel: {
    manual: "You",
    signal: "From a signal",
    heuristic: "Smart detection",
    local_ai: "Local AI",
  },
  methodTitle: "Which discovery method found this",

  // The names of the winning claim in an intersection warning, one per engine
  // MATCH CLASS, enforced by ../detection_parity_test.go. The warning names a
  // method, never an internal rank: a rule the user cannot read the inputs of is
  // indistinguishable from randomness.
  matchClassLabel: {
    built_in_pattern: "a built-in pattern",
    user_defined: "something you declared",
    smart_discovered: "Smart detection",
    local_ai_discovered: "Local AI",
  },

  // WHY a discovery method produced a row, one entry per engine evidence kind,
  // enforced by ../detection_parity_test.go. The engine returns evidence
  // STRUCTURED and the sentence is assembled here, because an engine returning
  // prose makes the copy a contract nobody can check.
  evidenceKindLabel: {
    email_local_part: "an email address naming this person",
    email_domain: "an email domain naming this organisation",
    website_domain: "a website domain naming this organisation",
  },
  evidenceTitle: "Why this was suggested",
  /**
   * evidenceSentence(e) turns one structured piece of evidence into a sentence.
   *
   * The signal text is included because it is the thing the user can check: "an
   * email address naming this person" is a claim, and "pierre.dupont@tpps.com" is
   * the evidence for it. The document list is included when present, so the
   * sentence points somewhere.
   */
  evidenceSentence(e) {
    const kind = this.evidenceKindLabel[e?.kind];
    if (!kind) return "";
    let out = `Found from ${kind}`;
    if (e.signalText) out += ` (${e.signalText})`;
    const docs = e.documents ?? [];
    if (docs.length > 0) out += ` in ${docs.join(", ")}`;
    return `${out}.`;
  },
  /**
   * relatedValues(others) names the Suggestions or Values that share evidence
   * with this one. Shared evidence makes them RELATED, never one Value: two
   * country branches of one group genuinely differ, so only the user can say they
   * are the same thing, which is why this is a note and not a fold.
   */
  relatedValues(others) {
    return `Shares evidence with ${others.join(", ")}. Group them only if they are the same thing.`;
  },

  // Intersections: two routes claim the same text. The precedence rule always
  // decides, so these are WARNINGS that explain the decision, never refusals.
  // The route names come from originLabel above, so a message names a route in
  // the same words the chip on the card uses.
  /**
   * intersectedText(value, matchedTexts) names what actually sat inside the
   * winner. Usually that IS the value's own text and the sentence says so once;
   * when the covered occurrences are spellings with different casing or shape (a
   * lowercase spelling inside an email, or the "pierre"/"dupont" fragments of a
   * person name), naming them instead avoids implying the exact quoted string was
   * found verbatim where it was not.
   */
  intersectedText(value, matchedTexts) {
    const list = (matchedTexts ?? []).filter((t) => t && t !== value);
    if (list.length === 0) return `"${value}"`;
    const quoted = list.map((t) => `"${t}"`).join(", ");
    return `${quoted} (${list.length === 1 ? "a spelling" : "spellings"} of "${value}")`;
  },
  /**
   * intersectionAll(value, winner, route, matchedTexts) is the only intersection
   * sentence there is: the value is never replaced under its own type, because a
   * higher-priority match covers every occurrence.
   *
   * There is no milder count sentence beside it, on purpose. A value covered in
   * some places and free in others keeps its own placeholder everywhere else, and
   * the covered occurrences are redacted by the winner, so the sentence would name
   * no leak and offer no action. The engine reports only full coverage.
   */
  intersectionAll(value, winner, route, matchedTexts) {
    return `Every occurrence of ${this.intersectedText(value, matchedTexts)} is also matched by ${route} as "${winner}", which takes priority. This value is not replaced under its own type.`;
  },
  intersectionOrder: "Priority order: built-in patterns, then your own Values and patterns, then Smart detection, then Local AI.",
  intersectionFix: "If this value should win instead, switch off the type that covers it, narrow the pattern, or add the covering term to Never anonymise.",
  intersectionAllowWinner: "Never anonymise the covering term",
  /** intersectionAllowed(term) confirms the covering term is now protected. */
  intersectionAllowed(term) {
    return `"${term}" is on the never anonymise list, so nothing replaces it now.`;
  },

  // My values.
  addValueLabel: "A value to replace",
  /** valueMatches(count, documents) is the live read-out under the add row.
   *  A value that matches nothing is almost always a typo, and saying so
   *  before the run is the cheapest correction there is. */
  valueMatches(count, documents) {
    if (count === 0) return "Not found in any imported document. Check the spelling.";
    return `Found ${count} time${count === 1 ? "" : "s"} in ${documents} document${documents === 1 ? "" : "s"}.`;
  },
  addValuePlaceholder: "add a value to replace, e.g. Meridian Consulting",
  addValueCategory: "The type of value",
  addValue: "Add value",
  noValues: "No values yet. Accept a suggestion, or add one above.",
  /** valueAlreadyThere(v) explains an add that changed nothing. */
  valueAlreadyThere(v) {
    return `${v} is already in the list.`;
  },
  removeValue: "Remove this value",
  derivedSpellings: "Spellings",
  addSpelling: "add",
  // The card's status icon. One glyph, two tones, and the accessible name is
  // where the difference is stated: a conflict must be fixed before the run,
  // a warning is something to know about a run that will go ahead.
  cardConflictLabel: "This value would refuse the run. Open for the reason and the fixes.",
  cardWarningLabel: "This value overlaps another detection. Open for the reason and the options.",
  cardInfoLabel: "Why this value is here",
  /**
   * moreSpellings(n) is the overflow control on the compact card.
   *
   * The card shows the spellings that fit on one line and nothing more, because a
   * chip row that grows with the data makes the card's height depend on it, and a
   * list of cards that resize under the pointer loses the reader's place. The rest
   * are not hidden, they are one click away in the popup that owns the full list.
   */
  moreSpellings(n) {
    return `+${n} more`;
  },
  moreSpellingsTitle: "Show every spelling, and add, edit, delete or regroup them",
  spellingDragHint: "Drag this spelling onto another Value to regroup it",
  /** removeSpelling titles the small delete control on a visible spelling chip.
   * Deleting here curates the value: the remaining chips become its whole list. */
  removeSpelling: "Remove this spelling",
  /** spellingMoved(v, target) confirms a regrouping drag. */
  spellingMoved(v, target) {
    return `${v} now counts as a spelling of ${target}.`;
  },
  spellingsPending: "working out the other spellings...",
  noSpellings: "no other spellings found",
  /**
   * spellingDeleted(v) confirms a spelling was dropped from one value, and
   * points at the tab that IS for negative rules. Deleting a spelling here
   * stops it belonging to THIS value; it does not stop it being replaced by
   * something else, and the difference is not guessable from the button.
   */
  spellingDeleted(v) {
    return `Removed the spelling "${v}" from this value. To stop it being replaced by anything at all, add it to Never anonymise.`;
  },
  /**
   * alsoSpelled(spellings) names the longer forms folded into one suggestion.
   * Accepting the row accepts them too, so the row has to say which.
   */
  alsoSpelled(spellings) {
    return `also spelled ${spellings.join(", ")}`;
  },
  /**
   * foldedIntoValue(added, main) explains that a new value joined an existing
   * one as a spelling rather than becoming a value of its own. Said out loud
   * because a silent fold is indistinguishable from the button not working.
   */
  foldedIntoValue(added, main) {
    return `Added as a spelling of "${main}", which is the shorter form.`;
  },
  /** spellingAlreadyThere(v) explains an add that changed nothing. */
  spellingAlreadyThere(v) {
    return `${v} is already one of the spellings.`;
  },

  // My values: filters, editing, grouping, conflicts and clearing.
  // The tab is two blocks under two captions, because narrowing the list and
  // adding to it are different jobs: FILTERS holds the search and the type
  // filter, VALUES holds the add row and the bulk clear.
  valuesFiltersHeading: "Filters",
  valuesHeading: "Values",
  valuesSearchPlaceholder: "search values and spellings",
  valuesSearchLabel: "Filter values by name or spelling",
  valuesAllTypes: "All types",
  valuesFilterTypeTitle: "Show only one type",
  noValuesMatch: "No value matches the current search and type filter.",
  clearAll: "Clear all",
  clearAllTitle: "Remove every value from this list",
  /** clearAllConfirm(n) is the in-app confirm before emptying the list. */
  clearAllConfirm(n) {
    return `Remove all ${n} value${n === 1 ? "" : "s"} from the list? Their spellings go with them. This does not touch the never-anonymise list or the suggestions.`;
  },
  // Picking cards with Ctrl+click narrows the bulk clear to what was picked, so
  // the one button has to SAY which of the two it will do. A button that reads
  // "Clear all" while a selection is showing would remove values the user just
  // took the trouble to exclude.
  clearSelected: "Clear selected",
  clearSelectedTitle: "Remove the selected values from this list",
  selectCardHint: "Ctrl+click a card to select it",
  /** clearSelectedConfirm(n) is the in-app confirm before removing the picked
   *  cards. It names the count, because the selection scrolls out of sight. */
  clearSelectedConfirm(n) {
    return `Remove the ${n} selected value${n === 1 ? "" : "s"} from the list? Their spellings go with them. This does not touch the never-anonymise list or the suggestions.`;
  },
  /** clearedN(n) reports the result of Clear all. */
  clearedN(n) {
    return n === 0 ? "The list was already empty." : `${n} value${n === 1 ? "" : "s"} removed.`;
  },
  // The spellings popup: the one surface that owns a Value's whole spelling list.
  // The compact card shows a preview and this shows everything, so every gesture
  // that used to be a per-chip control on the card lives here.
  /** spellingsPopupTitle(mainText) heads the popup with the value it belongs to. */
  spellingsPopupTitle(mainText) {
    return `Spellings for ${mainText}`;
  },
  /** spellingsPopupCount(n) counts the WHOLE list, never the filtered view: the
   *  search narrows what is shown, it does not remove anything. */
  spellingsPopupCount(n) {
    return `${n} spelling${n === 1 ? "" : "s"}`;
  },
  spellingsPopupAddPlaceholder: "a new spelling",
  spellingsPopupAddLabel: "Add a spelling to this value",
  spellingsPopupAdd: "Add",
  spellingsPopupSearchPlaceholder: "search spellings",
  spellingsPopupSearchLabel: "Filter the spellings shown",
  spellingsPopupNoMatch: "No spelling matches this search.",
  spellingsPopupEmpty: "This value has no other spellings yet. Add one above.",
  spellingsPopupClose: "Close",
  // Said in the popup because the popup has no OK button and its absence is the
  // thing to explain: every action here has already happened.
  spellingsPopupLive: "Changes are reflected immediately in the compact card.",
  spellingsPopupMainRow: "Main text",
  // The main text is shown so the list is the whole family rather than the
  // family minus its head, but it is not a spelling and the two actions that
  // apply to spellings do not apply to it.
  spellingsPopupMainNotDeletable: "This is the value's main text, not a spelling. Rename it on the card, or remove the whole value.",
  spellingsPopupDelete: "Delete",
  spellingsPopupDeleteTitle: "Stop replacing this spelling",
  spellingsPopupMove: "Move to",
  spellingsPopupMoveTitle: "Make this a spelling of another value instead",
  spellingsPopupMoveHeading: "Move this spelling",
  /** spellingsPopupMoveBody(spelling) says what the pick will do. */
  spellingsPopupMoveBody(spelling) {
    return `"${spelling}" stops being a spelling of this value and becomes a spelling of the one you pick, so it takes that value's placeholder.`;
  },
  spellingsPopupMoveNone: "There is no other value to move it to.",
  spellingsPopupEditTitle: "Edit this spelling",

  editValueTitle: "Rename this value",
  editValuePlaceholder: "the value, then Enter",
  editVariantPlaceholder: "the spelling, then Enter",
  changeTypeLabel: "Change the type of this value",
  /** valueRenamedDuplicate(v) explains a rename refused because the type
   *  already holds that name. */
  valueRenamedDuplicate(v) {
    return `${v} is already a value of this type. Use "Group with" to merge them instead.`;
  },
  /** typeChangeDuplicate(v) explains a type change refused for the same reason. */
  typeChangeDuplicate(v) {
    return `${v} already exists under that type. Use "Group with" to merge them instead.`;
  },
  /**
   * cardIdentityLost is what a value card says when its own markup no longer
   * carries the category and main text its actions act on. It names a recovery
   * the user can perform, because the alternative is a row of buttons that
   * appear enabled and change nothing.
   */
  cardIdentityLost: "This value card lost its identity, so its actions are disabled. Leave the step and come back to rebuild the list.",
  /**
   * cardIdentityLost is what a value card says when its own markup no longer
   * carries the category and main text its actions act on. It names a recovery
   * the user can perform, because the alternative is a row of buttons that
   * appear enabled and change nothing.
   */
  cardIdentityLost: "This value card lost its identity, so its actions are disabled. Leave the step and come back to rebuild the list.",

  // Group with: fold other values (and their spellings) into this one.
  groupWith: "Group with",
  groupWithTitle: "Merge other values into this one",
  groupWithHeading: "Merge into this value:",
  groupWithHint: "The values you tick become spellings of this one, and one placeholder then covers them all.",
  groupApply: "Group selected",
  groupCancel: "Cancel",
  groupNone: "There are no other values to group with yet.",
  // The pick-one that runs after Apply: the participating values become one,
  // and the user chooses WHICH keeps its placeholder in the output.
  groupMainTitle: "Choose the main value",
  groupMainBody: "The other values become spellings of the one you pick. Its placeholder is the one that will appear in the output.",
  /** groupedN(n, target) confirms a merge. */
  groupedN(n, target) {
    return `${n} value${n === 1 ? "" : "s"} merged into ${target}.`;
  },

  // Solve conflicts: the options offered per conflict kind.
  conflict: "Conflict",
  solveConflicts: "Solve conflicts",
  solveConflictsTitle: "Ways to resolve the conflicts on this value",
  solveHeading: "This value blocks the run. Choose a fix:",
  solveClose: "Close",
  /** conflictAmbiguity(value, otherType) is the wording for the same name under
   *  two types, and conflictCollision / conflictAllowlist their siblings. The
   *  view passes human type LABELS, not engine keys. */
  conflictAmbiguity(value, otherType) {
    return `${value} is also a value under ${otherType}, and one value can only have one replacement.`;
  },
  conflictCollision(spelling, otherValue) {
    return `${spelling} is also a spelling of ${otherValue}, so an occurrence of it belongs to neither.`;
  },
  conflictAllowlist(value) {
    return `${value} is also on the never-anonymise list, which always wins, so it would never be replaced.`;
  },
  // The concrete resolve actions, one line each.
  solveRemoveThis: "Remove this value",
  solveDropVariant: "Stop replacing this spelling here",
  solveGroupOther: "Merge the two values into one",
  /** solveGroupOtherLabel(other) names the other value in the merge action. */
  solveGroupOtherLabel(other) {
    return `Merge with ${other}`;
  },
  solveRemoveFromAllowlist: "Remove it from the never-anonymise list",

  // Patterns.
  // --- Built-in patterns tab (read-only) ---
  //
  // The tab answers one question: what did the signal categories I switched on
  // actually match. Every string here is written to keep that question separate
  // from the review gate: these are DIRECT matches, so there is nothing to
  // accept and no way to reject one from here, and the copy says where the
  // levers are instead of implying there are levers on the rows.
  builtInHint: "These are the matches the built-in patterns found the last time you ran detection. They are applied without review, so there is nothing to accept here. To change what is found, tick or untick the categories under Smart detection and run detection again.",
  builtInNeverRan: "Run detection to see what the built-in patterns match in your files.",
  builtInSwitchedOff: "Built-in patterns were switched off when detection last ran, so no signal was matched. Turn Built-in patterns on under Smart detection and run detection again.",
  builtInNoCategories: "None of the selected signal categories applies to the document country you chose, so no built-in pattern ran. Choose a category under Smart detection, or change the document country.",
  builtInNoMatchesAtAll: "The built-in patterns matched nothing in these files.",
  /** builtInNoneInCategory is the empty line under a category that ran and found nothing. */
  builtInNoneInCategory: "Nothing matched.",
  /** builtInSummary(values, categories) is the count line above the list. */
  builtInSummary(values, categories) {
    const v = `${values} match${values === 1 ? "" : "es"}`;
    const c = `${categories} categor${categories === 1 ? "y" : "ies"}`;
    return `${v} across ${c}`;
  },
  /** builtInOccurrences(count, documents) is one row's "how often, where" note. */
  builtInOccurrences(count, documents) {
    const c = `${count} occurrence${count === 1 ? "" : "s"}`;
    const d = `${documents} file${documents === 1 ? "" : "s"}`;
    return `${c} in ${d}`;
  },
  /** builtInInFiles(names) names the files a match occurs in, for the row title. */
  builtInInFiles(names) {
    return `Found in ${names.join(", ")}`;
  },
  /**
   * builtInLowConfidence(confidence) is the badge on a match whose corroborating
   * checksum failed. It is shown rather than hidden: a bank identifier that does
   * not check out is still replaced, and a mistyped or synthetic one is exactly
   * what a template document holds.
   */
  builtInLowConfidence(confidence) {
    return `Confidence ${confidence.toFixed(2)}, a corroborating check did not pass`;
  },
  builtInLowConfidenceBadge: "Check failed",

  patternsHint: "Regular expressions are matched in addition to the categories you selected. A pattern that does not compile is kept but never used.",
  addPattern: "Add pattern",
  addPatternPlaceholder: "add an expression, e.g. INV-\\d{6}",
  patternValid: "valid",
  patternCompiles: "this expression compiles",
  patternNoMatches: "This expression compiles but matches nothing in the imported documents.",
  /** patternSamples(samples) shows what a regex actually catches, which is the
   *  question "it compiles" never answered. */
  patternSamples(samples) {
    const shown = samples.slice(0, 5).join(", ");
    const more = samples.length > 5 ? `, and ${samples.length - 5} more` : "";
    return `Matches ${shown}${more}.`;
  },
  removePattern: "Remove this pattern",

  // Worked examples. A regular expression is a foreign language to most users,
  // so the Patterns tab ships a short catalogue: eight ready-made expressions
  // that each teach one building block (a literal prefix, a run of digits, an
  // optional separator, case-insensitivity, a word boundary, a repeated group).
  // Clicking one drops it into the add box, where the live feedback then shows
  // exactly what it catches, so a non-expert can copy the nearest example and
  // change a letter rather than write an expression from nothing.
  patternExamplesLabel: "Examples you can start from",
  patternExamplesHint: "Click an example to drop it in the box above, then tweak it. Each one shows a different building block and one value it matches.",
  /** patternExamples: the eight starter expressions. Kept RE2-safe (Go's
   *  regexp engine, no backreferences or lookahead) because the same string is
   *  compiled by the backend when it is added. Each carries a `sample`: one
   *  concrete string the expression actually matches, so the user sees a
   *  working example rather than only a description. The frontend test compiles
   *  every expr and asserts its sample matches, so a sample can never drift out
   *  of step with the pattern beside it. */
  patternExamples: [
    { expr: "INV-\\d{6}", label: "Invoice numbers", sample: "INV-004321" },
    { expr: "PO-\\d{4,6}", label: "Purchase orders of four to six digits", sample: "PO-88213" },
    { expr: "EMP[ -]?\\d+", label: "Employee numbers with an optional space or dash", sample: "EMP-42" },
    { expr: "(?i)ref[:\\s-]?[a-z0-9]{5,}", label: "Reference codes in any letter case", sample: "REF-8DK21" },
    { expr: "\\b[A-Z]{2,4}-\\d{2,}\\b", label: "Two to four capitals then a number", sample: "AB-1234" },
    { expr: "\\d{4}-\\d{2}-\\d{2}", label: "Dates written year-month-day", sample: "2026-08-12" },
    { expr: "\\b\\d{1,3}(\\.\\d{1,3}){3}\\b", label: "IP addresses", sample: "192.168.0.1" },
    { expr: "[\\w.+-]+@[\\w-]+\\.[\\w.]+", label: "Email-style addresses", sample: "name@company.com" },
  ],
  /** matchesSample(sample) prefixes the concrete match with plain words, so the
   *  mono string beside a description reads as "here is one it catches". */
  matchesSample(sample) {
    return `matches ${sample}`;
  },
  /** useExample(expr) is the per-row action label for assistive tech. */
  useExample(expr) {
    return `Use the example ${expr}`;
  },
};

// Anonymise screen copy.
export const ANONYMISE = {
  // The run card.
  run: "RUN",
  runAgain: "Run again",
  runNeedsDocuments: "Import at least one document first.",
  cancel: "Cancel",
  cancelTooltip: "Stop the run. Anything already replaced is discarded.",
  cancelIdleTooltip: "Nothing is running.",
  subtitleRunning: "Working through your documents.",
  subtitleDone: "Check the result side by side. Every replacement maps back to its original.",
  subtitleBlocked: "The run was refused before any text was changed. Fix the conflict, then run again.",
  /** subtitleIdle(n) says what is waiting before the first run. */
  subtitleIdle(n) {
    if (n === 0) return "No documents imported yet.";
    return `${n} document${n === 1 ? "" : "s"} ready. Nothing has been replaced yet.`;
  },
  /** progress(stage, file, index, total) is the progress bar's caption. */
  progress(stage, file, index, total) {
    return `${stage}: ${file} (${index}/${total})`;
  },
  progressStarting: "starting...",
  statReplacements: "REPLACEMENTS",
  statDocuments: "DOCUMENTS",
  statCategories: "CATEGORIES",
  statDuration: "DURATION",

  // The refused-run panel. A blocking conflict aborts the run before pass 1, so
  // nothing was replaced and there is nothing to compare: the panel is the only
  // thing on the screen that says why, and how to fix it.
  blockedTitle: "The run was refused",
  blockedIntro: "Nothing was replaced. Two values would fight over the same text, which would make the re-identification key ambiguous. Fix each conflict below, then run again.",
  blockedFixLabel: "How to fix it",
  // The per-conflict actions. A screen that can CREATE a blocking conflict has to
  // be able to clear it: the only other route to a fix is the Identify step, and
  // going there discards the registry, which would make the wizard punish a typo.
  /** blockedDeleteValue(v) NAMES the value it removes, because a conflict has two
   *  sides and two identical buttons would be a coin toss. */
  blockedDeleteValue(v) {
    return `Delete ${v}`;
  },
  blockedRemoveAllowTerm: "Remove the term from Never anonymise",
  /** blockedValueDeleted / blockedAllowTermRemoved report the outcome. Neither
   *  re-runs: clearing a conflict and deciding to run again are two decisions. */
  blockedValueDeleted(v) {
    return `${v} is no longer a value to replace. Run again.`;
  },
  blockedAllowTermRemoved(t) {
    return `${t} is off the never anonymise list. Run again.`;
  },

  // The selected placeholder card.
  selectedTitle: "Selected placeholder",
  closeSelection: "Close",
  replaces: "replaces",
  makeVariantOf: "Make it a spelling of",
  reassignPlaceholder: "type an existing value",
  reassignHint: "Reassigning runs the fast deterministic passes again. There is no AI re-scan, and existing placeholders keep their numbers.",
  /** reassignDone / reassignRefused report the outcome. */
  reassignDone(original, target) {
    return `${original} now counts as a spelling of ${target}.`;
  },
  reassignRefused(original, target) {
    return `${original} could not be attached to ${target}. Pick a different value.`;
  },

  // The report card.
  reportTitle: "Report",
  /** reportSummary(n, values) is the folded card's right-hand read-out. It
   *  counts VALUES as well as replacements: "48 replacements" alone does not
   *  tell a user whether there is a list of them to look at. */
  reportSummary(n, values) {
    const r = `${n} replacement${n === 1 ? "" : "s"}`;
    if (values === undefined) return r;
    return `${r}, ${values} value${values === 1 ? "" : "s"}`;
  },
  scopeLabel: "Which files the report covers",
  scopeAll: "All files",
  reportEmpty: "Nothing was replaced in the files in scope.",
  valuePlaceholder: "Value / placeholder",
  occurrences: "Occur.",
  // The flat value list. It is the answer to "what did you
  // replace?", which the category totals never gave.
  valuesTitle: "Replaced values",
  /** valuesSummary(n, removed) is the folded card's read-out. The removed count
   *  is shown only when there is one: "0 removed" invites a search for a list
   *  that is not there. */
  valuesSummary(n, removed) {
    const values = `${n} value${n === 1 ? "" : "s"}`;
    return removed ? `${values}, ${removed} removed` : values;
  },
  valuesKeyWarning: "Shows real values: this is your re-identification key.",
  placeholderLabel: "The replacement value",
  placeholderTooltip: "Edit what this value is replaced with. It takes effect on the next run, not on the text already shown.",
  removeValue: "Remove, and stop replacing it",
  /** valueRemoved(v) confirms a removal and says how far it reaches. */
  valueRemoved(v) {
    return `${v} will not be replaced any more, in this run or the next.`;
  },
  restoreValue: "Restore",
  valueRestored: "The value is being replaced again, with a new placeholder.",
  removedTitle: "Removed values",
  removedHint: "A restored value comes back with a NEW placeholder, because the old one may already be in a document you exported.",
  valuesFilterPlaceholder: "Filter values",
  valuesFilterEmpty: "No replaced value matches this filter.",
  byCategoryTitle: "By category",
  /** reportLevel(level) names the preset the run used. */
  reportLevel(level) {
    return `Ran at the ${level} preset`;
  },
  noValuesInScope: "No values from this category appear in the files in scope.",
  dismissWarning: "Hide this warning",

  // Add missed Value.
  missedTitle: "Add missed Value",
  /** missedSummary(n) is the folded card's read-out: how many values the next
   *  run will look for. */
  missedSummary(n) {
    return n === 0 ? "add a value" : `${n} value${n === 1 ? "" : "s"} to replace`;
  },
  missedHint: "Declare the Value, then re-run the fast passes. It gets a category, a placeholder and a re-identification entry like any other Value. Existing placeholders keep their numbers.",
  missedCategoryLabel: "The type of Value",
  missedLabel: "A Value the run missed",
  missedPlaceholder: "missed Value, e.g. P. Stone",
  addValue: "Add Value",
  /** missedAlreadyThere(v) explains an add that changed nothing. */
  missedAlreadyThere(v) {
    return `${v} is already on the list of values to replace.`;
  },
  fastRerun: "Fast re-run",
  /** fastRerunDone(n) reports what the re-run applied. */
  fastRerunDone(n) {
    return `Re-ran the fast passes over ${n} value${n === 1 ? "" : "s"}. ` +
      `Existing placeholders kept their numbers.`;
  },

  // The Compare card.
  compareDoc: "Which document to compare",
  paneOriginal: "ORIGINAL",
  paneAnonymised: "ANONYMISED",

  // The Compare search. There is one search bar PER pane, aligned right in the
  // pane's own caption, so each searches only the pane it sits on: its own
  // needle, its own cursor, its own readout. The readout does not name a pane
  // because the bar is already on it.
  searchLabelOriginal: "Search the original preview",
  searchLabelAnonymised: "Search the anonymised preview",
  searchPlaceholder: "Find in this preview",
  searchNext: "Next occurrence",
  searchPrev: "Previous occurrence",
  searchNone: "No match in this preview.",
  /** searchCount(index, total) is the readout, e.g. "3 of 8". */
  searchCount(index, total) {
    return `${index} of ${total}`;
  },
  /** searchCapped(max) says the highlight stopped rather than froze. */
  searchCapped(max) {
    return `Showing the first ${max} matches. Narrow the search to see fewer.`;
  },
  compareEmpty: "Run the anonymisation to compare the result with the original.",
  compareBlocked: "The run was refused, so there is nothing to compare. Resolve the conflict on the left, then run again.",
  /** tooltipTimes(n) is the second line of a mark's hover tooltip. */
  tooltipTimes(n) {
    return `replaced ${n} time${n === 1 ? "" : "s"} in this document`;
  },
  // Shown in the ORIGINAL pane when the source text is not available: the
  // document was removed from the import list while its result stayed on
  // screen. The pane says so rather than showing the anonymised text, which
  // is the one thing the ORIGINAL pane must never contain.
  originalUnavailable: "The source text is not available: this document was removed from the import list. Re-import it to compare.",
  /** replacementsInDocument(n) is the Compare card's read-out. */
  replacementsInDocument(n) {
    return `${n} replacement${n === 1 ? "" : "s"} in this document`;
  },

  // The floating selection panel. Selecting text in either pane offers to copy
  // it or to replace it, and replacing has two outcomes that differ in WHAT
  // ENDS UP IN THE RE-IDENTIFICATION KEY. That difference is not guessable from
  // the labels, which is why each mode carries a hint. Both outcomes go through
  // the Value model: there is no way from here to rewrite text without the key
  // recording it.
  selectionTitle: "Selected text",
  selectionCopy: "Copy",
  selectionReplace: "Replace",
  selectionModeVariant: "Make it a spelling of an existing Value",
  selectionModeValue: "Add it as a new Value",
  selectionModeVariantHint: "The text is replaced with that Value's placeholder, so both spellings share one number.",
  selectionModeValueHint: "The text becomes a Value of its own, with its own placeholder.",
  selectionTargetLabel: "Which Value it is a spelling of",
  selectionTargetPlaceholder: "start typing a Value",
  selectionTypeLabel: "Type",
  applySelection: "Apply",
  cancelSelection: "Cancel",
  selectionBack: "Back",
  selectionNeedsTarget: "Choose the Value this is a spelling of.",
  selectionUnknownTarget: "That Value is not in the list. Pick one of the suggestions.",
  selectionCopied: "Copied to the clipboard.",
  /** selectionBecameVariant(text, main) confirms the spelling mode. */
  selectionBecameVariant(text, main) {
    return `${text} now counts as a spelling of ${main}, so both share one placeholder.`;
  },
  /** selectionBecameValue(text) confirms the new-Value mode. */
  selectionBecameValue(text) {
    return `${text} is now a Value of its own, with its own placeholder.`;
  },
  /** selectionAlreadyThere(text) explains an Apply that changed nothing: the
   *  exact text was already declared under the chosen type. */
  selectionAlreadyThere(text) {
    return `${text} is already a Value under that type.`;
  },
  /** declaredValueNotFound(text) warns that a declared Value applied but
   *  matched no occurrence in the imported documents, instead of the success
   *  notice a run otherwise shows. */
  declaredValueNotFound(text) {
    return `"${text}" was added as a Value, but no occurrence of it was found in the imported documents.`;
  },

  // The footer.
  continueNeedsRun: "Run the anonymisation first.",
  continueBlocked: "Resolve the conflict before you continue.",
  hintRunning: "Running...",
  hintNotRun: "Run the anonymisation first",
  /** hintReady(n) says what is ready to export. */
  hintReady(n) {
    return `${n} replacement${n === 1 ? "" : "s"} ready to export`;
  },
};

// The Anonymise step's IMAGE half: every string on the picture review surface.
//
// It is a block of its own rather than more entries in ANONYMISE because it is a
// whole screen's worth of copy, and a screen's strings are easier to review
// together than scattered through the text half's.
export const IMAGES = {
  // The two halves of step 3, as the tab bar names them.
  tabText: "TEXT",
  tabImage: "IMAGE",
  tabImageTitle: "Every picture in the selected document, and what happens to it on export.",

  // The banner.
  documentLabel: "Document",
  filterLabel: "Show",
  layoutLabel: "View",
  layoutDetails: "Details",
  layoutTiles: "Tiles",
  /** filterChip(id, count) labels one filter chip with its live count, so the
   *  banner says how many rows each choice would show BEFORE it is pressed. */
  filterChip(id, count) {
    const labels = { all: "All", kept: "Kept", anonymised: "Anonymised" };
    return `${labels[id] ?? id} (${count})`;
  },

  // The details list's seven columns, in order.
  colPreview: "Preview",
  colName: "Name",
  colFormat: "Format",
  colDimensions: "Dimensions",
  colSize: "Size",
  colLocation: "Location",
  colStatus: "Status",

  // The two direct answers, on a row and on a tile.
  keep: "Keep",
  anonymise: "Anonymise",
  keepTitle: "Leave this picture exactly as it is.",
  anonymiseTitle: "Choose how this picture is replaced on export.",

  // The status a decision reads as. Kept is the one status that is not a
  // treatment: it is where every picture starts.
  statusLabel: {
    keep: "Kept",
    box: "Boxed",
    blur: "Blurred",
    remove: "Removed",
  },

  // What a picture's BYTES turned out to be. Go sniffs the format from the
  // content, so a part named .png holding JPEG bytes reads as JPG here.
  // "other" is the fallback for a format this application cannot redraw and
  // whose extension says nothing useful.
  formatLabel: {
    png: "PNG",
    jpeg: "JPG",
    svg: "SVG",
    other: "Other",
  },

  // What ENCLOSES the picture, said only where it is not a plain picture
  // element: removing a background or a shape fill has no element to delete, so
  // it means overwriting the bytes, and that is worth knowing before deciding.
  kindLabel: {
    picture: "Picture",
    fill: "Shape fill",
    background: "Background",
  },

  /** dimensions(w, h) is the pixel size, or nothing at all. A picture whose
   *  header could not be read has no size, and "0 x 0" would state a fact that
   *  is not true. */
  dimensions(w, h) {
    if (!w || !h) return "";
    return `${w} x ${h}`;
  },
  /** displaySize(cx, cy) is the frame the picture is DRAWN in, in centimetres,
   *  for the cell's tooltip. The source states it in English Metric Units
   *  (914400 per inch), and a fill or a background often states nothing. */
  displaySize(cx, cy) {
    if (!cx || !cy) return "";
    const cm = (emu) => (emu / 360000).toFixed(1);
    return `Drawn at ${cm(cx)} x ${cm(cy)} cm`;
  },
  /** fileSize(bytes) is one decimal above a megabyte and none below, because a
   *  tenth of a kilobyte is noise and a tenth of a megabyte is not. */
  fileSize(bytes) {
    const n = Number(bytes) || 0;
    if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
    if (n >= 1024) return `${Math.round(n / 1024)} KB`;
    return `${n} B`;
  },
  /** moreLocations(n) is the overflow marker on the Location cell. One decision
   *  covers every place a picture appears, and this is where that becomes
   *  visible. */
  moreLocations(n) {
    return `+${n} more`;
  },
  /** appearsIn(n) says how far one decision reaches, on the tile and in the
   *  treatment panel. */
  appearsIn(n) {
    return n === 1 ? "Appears in 1 place" : `Appears in ${n} places`;
  },
  unknownLocation: "Location not recorded",

  // The list's own states.
  loading: "Reading the pictures in this document...",
  empty: "This document has no pictures.",
  noneMatchFilter: "No picture matches this filter.",
  /** scanFailed(message) shows Go's own reason, which names what failed and how
   *  to fix it, rather than a sentence of ours that knows less. */
  scanFailed(message) {
    return `The pictures in this document could not be listed. ${message}`;
  },
  previewUnavailable: "No preview",

  // Why a document has no image review at all, by the reason CODE Go answers
  // with. The tab is never hidden: a tab that appears and disappears as the
  // user changes file reads as a bug, and this sentence is the answer to the
  // question a missing tab would raise.
  reason: {
    pdf_images_removed: "PDF export rebuilds the document as text, so every image in a PDF is already removed from the exported file. There is nothing to review here.",
    format_not_supported: "Image review is available for Word and PowerPoint files. This document has no images to review.",
  },

  // Per-document notes, by the warning CODE.
  warning: {
    unreadable_part: "At least one picture could not be read. It is still listed, and it can be removed.",
    linked_images: "At least one picture is linked from outside this document, so there are no bytes here to change. It can be removed.",
  },

  // The treatment panel.
  panelTitle: "Anonymise this picture",
  panelClose: "Cancel",
  panelApply: "Apply",
  panelPreviewLabel: "Preview",
  panelPreviewHint: "This is what the export will write, scaled down.",
  panelPreviewLoading: "Rendering the preview...",
  panelTreatmentLabel: "Replace it with",
  treatmentLabel: {
    keep: "Keep it",
    box: "Replace with a box",
    blur: "Blur",
    remove: "Remove",
  },
  // Why a treatment is not on offer for this picture, by the reason code
  // state.js treatmentBlockedReason answers with. Each names the way out, so a
  // disabled chip is never mute.
  blocked: {
    linked: "This picture is linked from outside the document, so there are no bytes here to change. It can be removed, or kept.",
    format: "This application cannot redraw this picture format. It can be removed, or kept.",
    svg_blur: "An SVG picture cannot be blurred: a blur filter leaves the original shapes and text inside the file. Use Replace with a box, or Remove.",
  },

  boxTextLabel: "Text in the box",
  boxTextPlaceholder: "for example, Client logo removed",
  boxTextHint: "Drawn with a built-in font, so accents are simplified and unusual characters become a question mark.",
  /** boxTextCount(n, max) is the live character counter. */
  boxTextCount(n, max) {
    return `${n}/${max}`;
  },

  blurLabel: "Blur strength",
  blurCaption: "Blur removes detail. It is not a guarantee: at a low strength, large text can still be read. Check the preview.",

  // The outcome notices.
  /** decisionApplied(treatment) confirms what the picture will become. */
  decisionApplied(treatment) {
    const labels = { keep: "kept", box: "replaced with a box", blur: "blurred", remove: "removed" };
    return `This picture will be ${labels[treatment] ?? treatment} in the exported file.`;
  },
  /** keptOne() confirms the one-click Keep. */
  kept: "This picture will be left exactly as it is.",
};

// Export screen copy.
export const EXPORT = {
  /** subtitle(total, docs) is the Export card's read-out. */
  subtitle(total, docs) {
    if (docs === 0) return "Nothing to export yet.";
    return `${total} replacement${total === 1 ? "" : "s"} across ` +
      `${docs} document${docs === 1 ? "" : "s"}. Choose what leaves this machine.`;
  },
  needsRun: "Run the anonymisation first.",

  destination: "Destination folder",
  destinationPlaceholder: "choose a folder for the batch",
  browse: "Browse",
  zip: "EXPORT ALL AS ZIP",
  zipTooltip: "Writes one zip into the destination folder, with no further dialog. The zip holds the anonymised documents only, never the key.",
  zipNeedsFolder: "Choose a destination folder first, with Browse.",
  /** zipDone(path) names the file that was written. */
  zipDone(path) {
    return `Batch exported to ${path}.`;
  },
  anonSuffixHint: "Every file saves with an _anon suffix. Your source files are never changed.",

  // Value mapping: the re-identification key.
  mappingTitle: "Value mapping",
  keyTag: "RE-IDENTIFICATION KEY",
  mappingHint: "Maps every placeholder back to its original value. Handle it like the originals themselves.",
  keyWarningBody: "This file contains the re-identification key: anyone holding it can map every placeholder back to the real value. Store it as carefully as the original documents.",
  mappingCsvTitle: "Export the value mapping as CSV",
  mappingCsvConfirm: "Export CSV",
  mappingCsvDone: "Value mapping exported as CSV. Keep it with the originals.",
  mappingJsonTitle: "Export the value mapping as JSON",
  mappingJsonConfirm: "Export JSON",
  mappingJsonDone: "Value mapping exported as JSON. Keep it with the originals.",

  // Report: contains no original values, so no warning and no tint.
  reportTitle: "Report",
  reportHint: "Counts per category, the settings used and any skipped files. Contains no original values.",
  markdown: "Markdown",
  /** reportSummary(n) is the folded card's read-out. */
  reportSummary(n) {
    return `${n} categor${n === 1 ? "y" : "ies"}`;
  },
  reportJsonDone: "Report exported as JSON.",
  reportMdDone: "Report exported as Markdown.",

  // The document list.
  documentsTitle: "Documents",
  /** documentsSummary(total) is the read-out beside the heading. */
  documentsSummary(total) {
    return `${total} replacement${total === 1 ? "" : "s"}, _anon suffix`;
  },
  oneAtATime: "SAVE ONE FILE AT A TIME",
  noResults: "No results yet. Run the anonymisation first.",
  loadingFormats: "reading the formats...",
  /** rowMeta(replacements, properties) is one row's second line. */
  rowMeta(replacements, properties) {
    const base = `${replacements} replacement${replacements === 1 ? "" : "s"}`;
    if (!properties) return base;
    return `${base}, ${properties} propert${properties === 1 ? "y" : "ies"}`;
  },
  /** sameFormatLabel(ext) labels a same-format button. */
  sameFormatLabel(ext) {
    return `.${ext} (same format)`;
  },
  nativeCaption: "Keeps the original layout. Your source file is not changed.",
  pdfCaption: "Experimental: a simplified layout, not a copy of the original design. Your source file is not changed.",
  plainCaption: "A plain export of the anonymised text.",
  copyTooltip: "Copy the anonymised text",
  /** savedPlain / copied report a per-document action. */
  savedPlain(name, ext) {
    return `${name} saved as .${ext}.`;
  },
  copied(name) {
    return `${name} copied to the clipboard as anonymised text.`;
  },

  // The properties review.
  /** reviewTitle(doc) names the file under review. */
  reviewTitle(doc) {
    return `Properties review: ${doc}`;
  },
  reviewHint: "These properties travel inside the file, so a document whose text is anonymised but whose Author field still names the person is not anonymised. Edit a value, or keep the original. Nothing is rewritten without your review.",
  /**
   * imagesChanged(summary) is what this copy does to the document's pictures.
   *
   * It is stated on the review panel, one line above the button that writes the
   * file, because this is the last moment the decision can be changed. The
   * treatments are broken out rather than totalled: "3 images will be changed"
   * alone does not say whether the client logo was boxed or removed.
   */
  imagesChanged(summary) {
    const parts = [];
    if (summary?.boxed) parts.push(`${summary.boxed} boxed`);
    if (summary?.blurred) parts.push(`${summary.blurred} blurred`);
    if (summary?.removed) parts.push(`${summary.removed} removed`);
    const n = (summary?.boxed ?? 0) + (summary?.blurred ?? 0) + (summary?.removed ?? 0);
    return `${n} image${n === 1 ? "" : "s"} will be changed in this copy (${parts.join(", ")}).`;
  },
  /**
   * imagesAllKept(n) is the sentence that matters more than the other one: a
   * user who never opened the IMAGE tab is told, at the moment it counts, that
   * the pictures are leaving this machine exactly as they arrived.
   */
  imagesAllKept(n) {
    if (n === 1) return "This copy keeps the document's one image, exactly as it is.";
    return `This copy keeps all ${n} of the document's images, exactly as they are.`;
  },
  property: "Property",
  current: "Current",
  willBecome: "Will become",
  keepOriginal: "keep original",
  keepOriginalTooltip: "Put the original value back in this field",
  unchanged: "unchanged",
  noProperties: "This file carries no document properties to review.",
  fileName: "File name",
  close: "Close",
  /** exportCopy(ext) labels the review's own export button. */
  exportCopy(ext) {
    return `Export .${ext} copy`;
  },
  /** reviewAlreadyOpen(doc) explains a second click on the same button. */
  reviewAlreadyOpen(doc) {
    return `The properties review for ${doc} is already open below.`;
  },
  /** sameFormatDone(filename) confirms a same-format write. */
  sameFormatDone(filename) {
    return `${filename} written. It looks exactly like the original, so store it accordingly. Your source file was not changed.`;
  },

  // The footer.
  finishHint: "Exports are written only when you press a save button",
  newBatch: "START A NEW BATCH",
  newBatchTooltip: "Clear this batch and keep your settings",
  newBatchTitle: "Start a new batch?",
  newBatchBody: "This clears the imported documents, the run and its result, the Values, the Suggestions and the patterns. Your settings, your document country and your never anonymise list are kept, and so is the placeholder registry, so a follow-up batch reuses the same placeholders for the same Values.",
  newBatchConfirm: "Clear the batch",
  newBatchDone: "Batch cleared. Drop new files on the Import step; your settings were kept.",
};

// Per-category checkbox labels and one-line examples.
// Keys match engine category identifiers.
//
// THE DECLARATION SHAPE MATTERS: ../category_parity_test.go matches on
// "\n key: [", so every entry must stay a two-element array literal opening on
// its own line with exactly two spaces of indent. That guard is what catches a
// recognizer added to the engine and forgotten here.
//
// Three of the examples are country-dependent (phone, vat, matricule). They
// carry a Luxembourg default here and are OVERLAID at render time by
// countries.js examplesFor(), so the rail shows a French number for a French
// document. Overlaying rather than storing five
// derivedSpellings keeps this table one row per category and keeps the guard working.
export const CATEGORY_LABELS = {
  email: ["Email addresses", "For example jean.muller@example.com"],
  phone: ["Phone numbers", "For example +352 621 123 456"],
  iban: ["Bank account numbers", "IBAN codes such as LU28 0019 4006 4475 0000"],
  vat: ["VAT numbers", "For example LU12345678"],
  matricule: ["National identification numbers", "For example the Luxembourg 13 digit number"],
  url: ["Web addresses", "For example https://example.com/report"],
  entity_names: ["Entity names", "Companies, teams and internal systems, for example Alpine Trust S.A."],
  project_names: ["Project names", "Engagement and workstream names, for example Project Atlas or ATLAS-2024"],
  product_names: ["Product names", "Named products and platforms, for example Meridian Suite"],
  brand_names: ["Brand names", "Trade names, found by the AI review or added by you"],
  person_names: ["Person names", "For example Marie Duval, M. Duval or just Marie"],
  identifier_names: ["Reference codes", "Contract, invoice and case codes, for example INV-88213"],
  other_names: ["Other names", "A name that fits none of the others, found by the AI review or added by you"],
  custom_patterns: ["Custom patterns", "The regular expressions you add on the Identify step"],
  date: ["Dates", "For example 15 January 2026 or 15/01/2026"],
  amount: ["Money amounts", "For example EUR 12,500"],
  // the recognizers built into the engine. They
  // were detecting all along; these labels are what finally let a user
  // see and switch them.
  credit_card: ["Payment card numbers", "For example 4111 1111 1111 1111, checked against its own check digit"],
  uk_nhs: ["UK health service numbers", "The 10 digit NHS number, for example 943 476 5919"],
  ip_address: ["Network addresses", "For example 192.168.1.24, and the longer IPv6 form"],
  mac_address: ["Device hardware addresses", "For example 3C:22:FB:1A:9E:04"],
  crypto: ["Cryptocurrency addresses", "Bitcoin addresses, for example bc1qar0srrr7xfkvy5l643lydnw9re59gtzz"],
  database_uri: ["Database connection strings", "For example postgres://user:password@host/db, which carries a password"],
  de_steuer_id: ["German tax identification numbers", "The 11 digit Steueridentifikationsnummer"],
  es_nif: ["Spanish tax numbers", "The NIF, 8 digits and a letter, for example 12345678Z"],
  bic: ["Bank identifier codes", "The BIC or SWIFT code beside an account, for example BGLLLULL"],
  postal_code: ["Postal codes", "The Luxembourg form, for example L-1855"],
  address: ["Street addresses", "For example 12, rue des Tilleuls"],
  country_names: ["Country names", "A country or jurisdiction, added by you or found by the AI review"],
  nationality_names: ["Nationalities", "For example Française, added by you or found by the AI review"],
  business_sector_names: ["Business sectors", "An industry or line of business, for example Transport"],
};
