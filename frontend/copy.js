// copy.js, the single home for user-visible strings (BUILD-02 Phase 2e).
//
// Keeping copy in one module gives the style guard (copy.test.js) and a
// future i18n pass exactly one place to look. Style rules (BUILD-02 ground
// rule 4): no em dashes, no "+" as a stand-in for "and", no unexplained
// jargon such as "PII" without an example, full sentences.

// The per-step explainer banner (BUILD-02 Phase 2e STEP_BANNERS) is GONE
// (BUILD-05 Phase 2). It was a strip of prose above every screen, repeating on
// each visit what the screen's own controls already said, and it cost the
// screens the vertical space the fixed-height card workspace needs. Its useful
// sentences moved into the card subtitles below, where they sit next to the
// heading they explain and scroll away with nothing.

// WORKFLOW: the step bar under the permanent top menu (BUILD-04 CR7, relaid
// out for BUILD-05 Phase 2). The separate "Anonymisation workflow" title is
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
  export: {
    title: "Export",
    subtitle: "Save the anonymised copies and, if you need it, the re-identification key.",
  },
};

// FOOTER: the permanent strip under every screen (BUILD-05 CR2), matching
// the Claude Design mockup's welcome page. version is the same string as
// wails.json productVersion; there is no bound Go method to read it live
// yet, so it is a plain literal here until that wiring exists.
export const FOOTER = {
  version: "Version 0.1.0",
  localProcessing: "Local processing only",
};

// NAV: navigation copy (BUILD-04 CR16). Going BACK through the wizard
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
  // in-app modal now, not a native confirm (BUILD-05 decision 10), so the
  // question is a title plus a body rather than one cramped line.
  backConfirmTitle(step) {
    return `Reset the ${NAV.stepNames[step] ?? step} step?`;
  },
  backConfirmBody(step) {
    const name = NAV.stepNames[step] ?? step;
    return `Going back clears everything the ${name} step owns, so you start it fresh. ` +
      `Your imported documents and your never anonymise list are kept.`;
  },
  backConfirmLabel: "Go back and reset",
};

// Home page copy (BUILD-02 Phase 2b, rewritten for BUILD-04 CR1, sidebar
// added in BUILD-05 CR1).
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
// BUILD-05 Phase 2 cut it from five entries to four, matching the wizard.
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
    { label: "Identify", body: "Pick a preset and fine-tune the 22 detection categories, then review every suggested value. Nothing is replaced until you accept it." },
    { label: "Anonymise", body: "Run the passes and check the result side by side, with every replacement mapped back to its original." },
    { label: "Export", body: "Save the anonymised copies, the report and the re-identification key." },
  ],
  docsLink: "Read the documentation",
};

// The documentation placeholder page was retired by BUILD-04 CR6: real
// documentation now lives in frontend/docs/index.html and opens in its own
// window, so there is no in-app docs screen and no placeholder string.

// Import screen copy (BUILD-05 Phase 4).
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
};

// Configure step copy (BUILD-02 Phase 6). Plain language: no "PII", no
// abbreviations without an example, full sentences.
export const CONFIGURE = {
  presetHint: "Start from a preset, then adjust the checkboxes if you need to. Changing any checkbox switches the preset to Custom.",
  groupContact: "Contact and account details",
  groupNames: "Names",
  groupThorough: "Only for thorough anonymisation",
  useAILabel: "Use local AI (Ollama)",
  useAIHint: "When enabled, a language model running on this machine can suggest names to replace and double-check the result. Nothing leaves your computer.",
  contextSizeHint: "Higher values let the AI read longer documents at once but use more memory.",
  aiOffTooltip: "Local AI is turned off. Turn it on with the switch on the Local AI section of Configure.",
  allowHint: "Terms in this list survive every pass, even when they also appear as names to replace.",
  // BUILD-04 CR9: the group that surfaces the BUILD-03 recognizers.
  groupTechnical: "Payment, tax and technical identifiers",
  // BUILD-04 CR10: the per-group bulk buttons.
  selectAll: "Select all",
  deselectAll: "Deselect all",
  // BUILD-04 CR9: the detection-confidence control. Plain language, with
  // the two thresholds that actually change something spelled out.
  confidenceTitle: "Detection confidence",
  confidenceLabel: "Minimum confidence",
  confidenceHint: "Every detection carries a score for how certain it is. Anything below the minimum you set here is left alone. Keep it at 0 to replace everything that is found, which is how the application behaves by default.",
  // confidenceScale is GONE (BUILD-05 decision 3). It described a source-tiered
  // rule ("above 80, the values that only the local AI suggested are left
  // alone") that the engine does not implement: the setting is a floor on a
  // score, not a rule about who proposed a value. What the floor actually does
  // at each position is views/identifyrail.js confidenceEffect(), which is
  // tested for exactly that.
};

/**
 * categoryLabels(examples) returns CATEGORY_LABELS with the country-dependent
 * examples replaced (BUILD-05 Phase 5).
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

// Identify RAIL copy (BUILD-05 Phase 5): the four tabs and the Scope tab's
// section labels. The category labels and the confidence copy stay in CONFIGURE
// below, which is where they were and where the parity guard looks.
export const RAIL = {
  tabsLabel: "Configure sections",
  tabSmart: "Smart detection",
  tabLocalAI: "Local AI",
  tabCloudAI: "Cloud AI",

  // BUILD-06: the three routes are switchable sections, not tabs. Scope stopped
  // being a section of its own because it is the scope OF smart detection.
  smartIntro: "Finds names by how they are written, on this machine and without any AI. It runs on the categories you choose below.",
  smartTuning: "Strictness",
  routeOn: "On",
  routeOff: "Off",
  /** routeSwitchLabel(title) is the accessible name of a section's switch. */
  routeSwitchLabel(title) {
    return `Turn ${title} on or off`;
  },

  country: "Document country",
  countryHint: "The phone, VAT and national identification examples follow this country's formats, and the matching national identifiers are switched on. It changes nothing else about how detection works.",
  preset: "Preset",
  whatToAnonymise: "What to anonymise",

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
  noModels: "(no models found)",
  reprobe: "Check again",

  // The Cloud AI placeholder (decision 8). It commits only to the thing that
  // will not change about the feature: nothing leaves the machine until the user
  // has said in writing what may.
  cloudNotYet: "Not available yet",
  cloudBody: "Connecting to a cloud endpoint is not built yet. When it is, this is where you will pick the provider, the model and the endpoint, and confirm in writing what may leave this machine before anything is sent.",
};

// Values step copy (BUILD-04 Phase 5): the smart-detection tuning block
// (CR13) and the suggestions table (CR14/CR15).
export const VALUES = {
  // Smart detection tuning. It moved to the Identify RAIL's own tab
  // (BUILD-05 Phase 5), which RAIL.tabSmart titles, so the block no longer
  // needs a heading of its own.
  smartSettingsHint: "Smart detection guesses which words are names from how they are written, so it always proposes some things that are not names. These settings decide how strict it is. Set them all to zero, and untick the box, to see everything it can find.",
  smartMinLength: "Shortest value",
  smartMinLengthHint: "Suggestions shorter than this many letters are skipped.",
  smartMinOccurrences: "Fewest occurrences",
  smartMinOccurrencesHint: "How often a value must appear before it is suggested. 1 means once is enough.",
  smartCommonWords: "Skip ordinary words",
  smartCommonWordsHint: "Ignores month names, weekdays and common sentence openers, which are capitalised without being names.",
  smartMinConfidence: "Minimum certainty",
  smartMinConfidenceHint: "Higher values keep only the strongest suggestions, such as a name followed by a company form or introduced by a title.",

  // The suggestions table's search box and its two sort tooltips. The column
  // HEADINGS moved to WORKSPACE (BUILD-05 Phase 6), where they are upper-case
  // because they sit in a header strip rather than above a form.
  searchPlaceholder: "search values",
  sortValueHint: "Sort by value, A to Z or Z to A.",
  sortCountHint: "Sort by how often the value occurs.",
  noMatchingSuggestions: "No suggestion matches the current search and type filter.",
};

// The never-anonymise editor (BUILD-05 Phase 6). The list wins over every pass,
// which is why the hint says so rather than describing the control.
export const ALLOWLIST = {
  label: "A term to never anonymise",
  placeholder: "add a term, e.g. CSSF",
  add: "Add",
  importCSV: "Import CSV",
  template: "Template",
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
};

// Identify WORKSPACE copy (BUILD-05 Phase 6): the four tabs, the suggestions
// table, the value cards and the pattern rows.
export const WORKSPACE = {
  tabsLabel: "Identify sections",
  tabLabels: {
    suggestions: "Suggestions",
    values: "My values",
    allow: "Never anonymise",
    patterns: "Patterns",
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
  // The progress caption (BUILD-06). It is assembled from these parts rather
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
  allSources: "ALL SOURCES",
  filterTypeTitle: "Filter by type",
  filterSourceTitle: "Filter by what found the value",
  accept: "Accept",
  reject: "Reject",
  acceptAllShown: "Accept all shown",
  rejectAllShown: "Reject all shown",
  bulkScopeHint: "Applies to the rows shown below, so a search or a filter limits it too.",
  /** acceptedN / rejectedN report a bulk action's result. */
  acceptedN(n) {
    return n === 0 ? "Nothing to accept." : `${n} value${n === 1 ? "" : "s"} accepted.`;
  },
  rejectedN(n) {
    return n === 0 ? "Nothing to reject." : `${n} suggestion${n === 1 ? "" : "s"} rejected.`;
  },
  // The source badge labels. "Pattern" is deliberately absent (decision 9):
  // deterministic matches are applied without review and never become
  // suggestions, so naming a source that cannot appear would promise rows the
  // table can never show.
  sourceLabels: {
    smart: "Smart",
    "local-ai": "Local AI",
  },

  // My values.
  addValueLabel: "A value to replace",
  addValuePlaceholder: "add a value to replace, e.g. Meridian Consulting",
  addValueCategory: "The type of value",
  addValue: "Add value",
  noValues: "No values yet. Accept a suggestion, or add one above.",
  /** valueAlreadyThere(v) explains an add that changed nothing. */
  valueAlreadyThere(v) {
    return `${v} is already in the list.`;
  },
  removeValue: "Remove this value",
  variants: "Variants",
  addVariant: "add",
  addVariantPlaceholder: "another spelling, then Enter",
  removeVariant: "Stop replacing this spelling",
  variantDragHint: "Drag this spelling onto another value to regroup it",
  /** variantMoved(v, target) confirms a regrouping drag. */
  variantMoved(v, target) {
    return `${v} now counts as a spelling of ${target}.`;
  },
  variantsPending: "working out the other spellings...",
  noVariants: "no other spellings found",
  /** variantAlreadyThere(v) explains an add that changed nothing. */
  variantAlreadyThere(v) {
    return `${v} is already one of the spellings.`;
  },
  placeholderLabel: "The replacement value",
  placeholderTooltip: "Edit what this value is replaced with. It takes effect on the next run.",
  placeholderPending: "after the run",
  placeholderPendingTooltip: "Placeholders are assigned when the anonymisation runs, so there is nothing to rename yet.",

  // Patterns.
  patternsHint: "Regular expressions are matched in addition to the categories you selected. A pattern that does not compile is kept but never used.",
  addPattern: "Add pattern",
  addPatternPlaceholder: "add an expression, e.g. INV-\\d{6}",
  patternValid: "valid",
  patternCompiles: "this expression compiles",
  removePattern: "Remove this pattern",
};

// Anonymise screen copy (BUILD-05 Phase 7).
export const ANONYMISE = {
  // The run card.
  run: "RUN",
  runAgain: "Run again",
  runNeedsDocuments: "Import at least one document first.",
  cancel: "Cancel",
  cancelTooltip: "Stop the run. Anything already replaced is discarded.",
  cancelIdleTooltip: "Nothing is running.",
  deepScan: "Deep scan (AI)",
  deepScanTooltip: "An extra AI pass that looks for values the deterministic passes left behind.",
  subtitleRunning: "Working through your documents.",
  subtitleDone: "Check the result side by side. Every replacement maps back to its original.",
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

  // The selected placeholder card.
  selectedTitle: "Selected placeholder",
  closeSelection: "Close",
  replaces: "replaces",
  makeVariantOf: "Make it a variant of",
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
  // The flat value list (BUILD-06). It is the answer to "what did you
  // replace?", which the category totals never gave.
  valuesTitle: "Replaced values",
  valuesKeyWarning: "Shows real values: this is your re-identification key.",
  valuesFilterPlaceholder: "Filter values",
  valuesFilterEmpty: "No replaced value matches this filter.",
  byCategoryTitle: "By category",
  /** reportLevel(level) names the preset the run used. */
  reportLevel(level) {
    return `Ran at the ${level} preset`;
  },
  /** reportLLMPass(text) reports what happened to the AI pass, verbatim from
   *  Go. A run that degraded halfway used to say so only in the JSON export. */
  reportLLMPass(text) {
    return `AI deep scan: ${text}`;
  },
  noValuesInScope: "No values from this category appear in the files in scope.",
  dismissWarning: "Hide this warning",

  // Something missed?
  missedTitle: "Something missed?",
  /** missedSummary(n) is the folded card's read-out. */
  missedSummary(n) {
    return n === 0 ? "add a value" : `${n} waiting`;
  },
  missedHint: "Add the value, then re-run the fast passes. Existing placeholders keep their numbers.",
  missedCategoryLabel: "The type of value",
  missedLabel: "A value the run missed",
  missedPlaceholder: "missed value, e.g. P. Stone",
  addValue: "Add value",
  removePending: "Remove, and stop replacing it",
  /** missedAlreadyThere(v) explains an add that changed nothing. */
  missedAlreadyThere(v) {
    return `${v} is already on the list of values to replace.`;
  },
  fastRerun: "Fast re-run",
  /** fastRerunDone(n) reports what the re-run applied. */
  fastRerunDone(n) {
    if (n === 0) return "Re-ran the fast passes. Existing placeholders kept their numbers.";
    return `Re-ran the fast passes with ${n} added value${n === 1 ? "" : "s"}. ` +
      `Existing placeholders kept their numbers.`;
  },

  // Find and replace.
  rulesTitle: "Find and replace",
  /** rulesSummary(n) is the folded card's read-out. */
  rulesSummary(n) {
    return n === 1 ? "1 rule" : `${n} rules`;
  },
  rulesHint: "These run last, in order, and each rule sees what the previous one produced.",
  ruleFind: "find",
  ruleReplace: "replace with",
  ruleTo: "to",
  caseSensitive: "Case-sensitive",
  exactCase: "exact case",
  anyCase: "any case",
  addRule: "Add rule",
  ruleNeedsFind: "Type the text to find. A rule with nothing to find would do nothing.",
  moveUp: "Run this rule earlier",
  moveDown: "Run this rule later",
  removeRule: "Remove this rule",

  // The Compare card.
  compareDoc: "Which document to compare",
  paneOriginal: "ORIGINAL",
  paneAnonymised: "ANONYMISED",
  compareEmpty: "Run the anonymisation to compare the result with the original.",
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

  // The floating replace-selection panel.
  replaceSelection: "Replace selection",
  replaceWith: "What to replace it with",
  applySelection: "Replace",
  cancelSelection: "Cancel",
  selectionNeedsReplacement: "Type what the selected text should become.",
  /** selectionApplied(find, replace) confirms the new rule. */
  selectionApplied(find, replace) {
    return `${find} is now replaced with ${replace} everywhere, by a find and replace rule.`;
  },

  // The footer.
  continueNeedsRun: "Run the anonymisation first.",
  hintRunning: "Running...",
  hintNotRun: "Run the anonymisation first",
  /** hintReady(n) says what is ready to export. */
  hintReady(n) {
    return `${n} replacement${n === 1 ? "" : "s"} ready to export`;
  },
};

// Export screen copy (BUILD-05 Phase 8).
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

  // Session.
  sessionTitle: "Session",
  sessionSummary: "reuse placeholders",
  sessionHint: "Saves values, allowlist, patterns, rules and the placeholder registry, so a follow-up batch reuses the same placeholders. Contains the key.",
  save: "Save",
  load: "Load",
  sessionSaveTitle: "Save the session file",
  sessionSaveConfirm: "Save session",
  sessionSaveDone: "Session saved. A follow-up batch will reuse these placeholders.",
  sessionLoadDone: "Session loaded: values, allowlist, patterns and rules restored.",

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
  newBatchBody: "This clears the imported documents, the run and its result, the values, the suggestions, the patterns and the find and replace rules. Your settings, your document country and your never anonymise list are kept, and so is the placeholder registry, so a follow-up batch reuses the same placeholders for the same values.",
  newBatchConfirm: "Clear the batch",
  newBatchDone: "Batch cleared. Drop new files on the Import step; your settings were kept.",
};

// Per-category checkbox labels and one-line examples (BUILD-02 Phase 6b).
// Keys match engine category identifiers.
//
// THE DECLARATION SHAPE MATTERS: ../category_parity_test.go matches on
// "\n  key: [", so every entry must stay a two-element array literal opening on
// its own line with exactly two spaces of indent. That guard is what catches a
// recognizer added to the engine and forgotten here.
//
// Three of the examples are country-dependent (phone, vat, matricule). They
// carry a Luxembourg default here and are OVERLAID at render time by
// countries.js examplesFor(), so the rail shows a French number for a French
// document (BUILD-05 Phase 5, decision 2). Overlaying rather than storing five
// variants keeps this table one row per category and keeps the guard working.
export const CATEGORY_LABELS = {
  email: ["Email addresses", "For example jean.muller@example.com"],
  phone: ["Phone numbers", "For example +352 621 123 456"],
  iban: ["Bank account numbers", "IBAN codes such as LU28 0019 4006 4475 0000"],
  vat: ["VAT numbers", "For example LU12345678"],
  matricule: ["National identification numbers", "For example the Luxembourg 13 digit number"],
  url: ["Web addresses", "For example https://example.com/report"],
  entity_names: ["Entity names", "Companies, teams and internal systems you list in the Values step"],
  project_names: ["Project names", "The project names you list in the Values step"],
  person_names: ["Person names", "For example Marie Duval, M. Duval or just Marie"],
  custom_patterns: ["Custom patterns", "The regular expressions you add in the Values step"],
  date: ["Dates", "For example 15 January 2026 or 15/01/2026"],
  organisation_names: ["Organisation names", "Organisations suggested by the AI review"],
  location_names: ["Place names", "Cities and places suggested by the AI review"],
  amount: ["Money amounts", "For example EUR 12,500"],
  // BUILD-04 CR9: the recognizers BUILD-03 built into the engine. They
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
};
