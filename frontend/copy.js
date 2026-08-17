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
    return `Going back clears everything the ${name} step owns, so you start it fresh. ` +
      `Your imported documents and your never anonymise list are kept.`;
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
  presetHint: "Start from a preset, then adjust the checkboxes if you need to. Changing any checkbox switches the preset to Custom.",
  groupContact: "Contact and account details",
  // The rail groups by TRIGGER, the user's own model of how a value is found
  // so these are the names of the three ways it happens.
  // groupNames was "Names", which said nothing about where the values came
  // from and sat over a list that also held the user's own regexes.
  groupDetected: "Auto detected values",
  groupDeclared: "Your own patterns",
  groupThorough: "Only for thorough anonymisation",
  useAIHint: "When enabled, a language model running on this machine can suggest names to replace and double-check the result. Nothing leaves your computer.",
  contextSizeHint: "Higher values let the AI read longer documents at once but use more memory.",
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
  confidenceHint: "Every detection carries a score for how certain it is. Anything below the minimum you set here is left alone. Keep it at 0 to replace everything that is found, which is how the application behaves by default.",
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
  tabCloudAI: "Cloud AI",

  // the three routes are switchable sections, not tabs. Scope stopped
  // being a section of its own because it is the scope OF smart detection.
  smartIntro: "Finds names by how they are written, on this machine and without any AI. It runs on the categories you choose below.",
  smartTuning: "Strictness",
  // The two independent halves the Smart detection route splits into, rendered
  // as toggles at the top of the section.
  nativeDetect: "Native detection (signals)",
  nativeDetectHint: "Regex signals such as emails, VAT numbers and IBANs.",
  autoDetect: "Auto detection (word frequency)",
  autoDetectHint: "Finds recurring names by word frequency.",
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

  //  split the category list in two blocks: the regex-triggered
  // patterns (found by shape) and the entity categories (found by name). Both
  // blocks live in the Smart detection section because the category selection is
  // the ONE scope the whole pipeline reads (CLAUDE.md §5): rendering a second
  // copy of the same checkboxes inside Local AI would give one setting two
  // controls, and the second copy would be folded shut and unreachable anyway.
  valuesAuto: "Values to detect automatically",
  valuesAutoHint: "These categories are used by every detection route you switch on.",
  // What the Local AI section says INSTEAD of a second copy of the checkboxes.
  localValuesHint: "Local AI looks for the same value categories chosen under Smart detection above. Switching this route on adds a model pass over them, it does not change what is selected.",

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

  // Local-AI SCAN SCOPE. Handing a whole document to a small local model is too
  // much, so the user can aim the scan at one document and a range of its own
  // units (pages, slides, rows or lines). This scope applies to the Local AI
  // route only; Smart detection always reads everything because it is cheap.
  scopeHeading: "What to scan",
  scopeIntro: "The local AI reads only what you point it at. Scanning one document, or a few pages of one, keeps a small model focused and the pass quick.",
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

  // The Cloud AI placeholder. It commits only to the thing that
  // will not change about the feature: nothing leaves the machine until the user
  // has said in writing what may.
  cloudNotYet: "Not available yet",
  cloudBody: "Connecting to a cloud endpoint is not built yet. When it is, this is where you will pick the provider, the model and the endpoint, and confirm in writing what may leave this machine before anything is sent.",

  // The Load profile section: a plain (switch-less) section at the foot of the
  // rail. Load restores a saved profile; Save writes one, but only once a run
  // has produced a registry worth preserving.
  profileTitle: "Load profile",
  profileHint: "Reuse a saved setup: values, allowlist, patterns, rules and the placeholder registry, so a follow-up batch reuses the same placeholders.",
  profileLoad: "Load",
  profileSave: "Save",
  profileSaveDisabled: "Run detection once before saving a profile.",
  profileLoadDone: "Profile loaded: values, allowlist, patterns and rules restored.",
  profileSaveDone: "Profile saved. A follow-up batch will reuse these placeholders.",
};

// Values step copy: the smart-detection tuning block
//  and the suggestions table.
export const VALUES = {
  // Smart detection tuning. It moved to the Identify RAIL's own tab
  // which RAIL.tabSmart titles, so the block no longer
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
  smartStrictness: "How much to trust",
  smartStrictnessHint: "Strict keeps only suggestions with strong evidence, such as a company form, a title or a matching email address. Lenient also shows the weakest guesses, which pairs well with a low minimum certainty. Balanced is the default.",
  smartStrictnessLenient: "Lenient: show weak guesses too",
  smartStrictnessBalanced: "Balanced (recommended)",
  smartStrictnessStrict: "Strict: strong evidence only",

  // The suggestions table's search box and its two sort tooltips. The column
  // HEADINGS moved to WORKSPACE, where they are upper-case
  // because they sit in a header strip rather than above a form.
  searchPlaceholder: "search values",
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
};

// Identify WORKSPACE copy: the four tabs, the suggestions
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
  allSources: "ALL SOURCES",
  filterTypeTitle: "Filter by type",
  filterSourceTitle: "Filter by what found the value",
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
  // The source badge labels. "Pattern" is deliberately absent:
  // deterministic matches are applied without review and never become
  // suggestions, so naming a source that cannot appear would promise rows the
  // table can never show.
  sourceLabels: {
    smart: "Smart",
    "local-ai": "Local AI",
  },

  // The ROUTE a value came from, shown on its card. A precedence rule the user
  // cannot see the inputs of is indistinguishable from randomness, which is how
  // the old behaviour came to be reported. One label per engine origin,
  // enforced by ../origin_parity_test.go.
  originLabel: {
    native: "Native",
    declared: "You",
    auto: "Smart detection",
    ai: "Local AI",
  },
  originTitle: "The detection route this value came from",

  // Intersections: two routes claim the same text. The precedence rule always
  // decides, so these are WARNINGS that explain the decision, never refusals.
  // The route names come from originLabel above, so a message names a route in
  // the same words the chip on the card uses.
  intersectionTitle: "Overlaps another detection",
  /** intersectionAll(value, winner, route) is the case worth shouting about:
   *  the value is never replaced under its own type. */
  intersectionAll(value, winner, route) {
    return `Every occurrence of "${value}" is also matched by ${route} as "${winner}", which takes priority. This value is not replaced under its own type.`;
  },
  /** intersectionSome(covered, total, value, winner, route) is the milder
   *  case: the value still applies where nothing covers it. */
  intersectionSome(covered, total, value, winner, route) {
    return `${covered} of ${total} occurrences of "${value}" are also matched by ${route} as "${winner}", which takes priority there.`;
  },
  intersectionOrder: "Priority order: native detection, then your own values and patterns, then Smart detection, then Local AI.",
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
  /**
   * variantDeleted(v) confirms a spelling was dropped from one value, and
   * points at the tab that IS for negative rules. Deleting a spelling here
   * stops it belonging to THIS value; it does not stop it being replaced by
   * something else, and the difference is not guessable from the button.
   */
  variantDeleted(v) {
    return `Removed the spelling "${v}" from this value. To stop it being replaced by anything at all, add it to Never anonymise.`;
  },
  /**
   * alsoSpelled(variants) names the longer forms folded into one suggestion.
   * Accepting the row accepts them too, so the row has to say which.
   */
  alsoSpelled(variants) {
    return `also spelled ${variants.join(", ")}`;
  },
  /**
   * foldedIntoValue(added, main) explains that a new value joined an existing
   * one as a spelling rather than becoming a value of its own. Said out loud
   * because a silent fold is indistinguishable from the button not working.
   */
  foldedIntoValue(added, main) {
    return `Added as a spelling of "${main}", which is the shorter form.`;
  },
  /** variantAlreadyThere(v) explains an add that changed nothing. */
  variantAlreadyThere(v) {
    return `${v} is already one of the spellings.`;
  },

  // My values: filters, editing, grouping, conflicts and clearing.
  valuesSearchPlaceholder: "search values and spellings",
  valuesSearchLabel: "Filter values by name or spelling",
  valuesAllTypes: "All types",
  valuesFilterTypeTitle: "Show only one type",
  showVariants: "Show spellings",
  hideVariants: "Hide spellings",
  showVariantsTitle: "Show the spellings under each value",
  hideVariantsTitle: "Hide the spellings to see more values at once",
  noValuesMatch: "No value matches the current search and type filter.",
  clearAll: "Clear all",
  clearAllTitle: "Remove every value from this list",
  /** clearAllConfirm(n) is the in-app confirm before emptying the list. */
  clearAllConfirm(n) {
    return `Remove all ${n} value${n === 1 ? "" : "s"} from the list? Their spellings go with them. This does not touch the never-anonymise list or the suggestions.`;
  },
  /** clearedN(n) reports the result of Clear all. */
  clearedN(n) {
    return n === 0 ? "The list was already empty." : `${n} value${n === 1 ? "" : "s"} removed.`;
  },
  editValueTitle: "Rename this value",
  editValuePlaceholder: "the value, then Enter",
  editVariantTitle: "Edit this spelling (double-click)",
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
  blockedIntro: "Nothing was replaced. Two values would fight over the same text, which would make the re-identification key ambiguous. Fix each conflict below on the Identify step, then run again.",
  blockedFixLabel: "How to fix it",

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

  // Something missed?
  missedTitle: "Something missed?",
  /** missedSummary(n) is the folded card's read-out: how many values the next
   *  run will look for. */
  missedSummary(n) {
    return n === 0 ? "add a value" : `${n} value${n === 1 ? "" : "s"} to replace`;
  },
  missedHint: "Add the value, then re-run the fast passes. Existing placeholders keep their numbers.",
  missedCategoryLabel: "The type of value",
  missedLabel: "A value the run missed",
  missedPlaceholder: "missed value, e.g. P. Stone",
  addValue: "Add value",
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
  continueBlocked: "Resolve the conflict before you continue.",
  hintRunning: "Running...",
  hintNotRun: "Run the anonymisation first",
  /** hintReady(n) says what is ready to export. */
  hintReady(n) {
    return `${n} replacement${n === 1 ? "" : "s"} ready to export`;
  },
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

  // Profile (Save only; Load lives on the Identify rail).
  sessionTitle: "Profile",
  sessionSummary: "reuse placeholders",
  sessionHint: "Saves values, allowlist, patterns, rules and the placeholder registry, so a follow-up batch reuses the same placeholders. Contains the key.",
  save: "Save",
  sessionSaveTitle: "Save the profile file",
  sessionSaveConfirm: "Save profile",
  sessionSaveDone: "Profile saved. A follow-up batch will reuse these placeholders.",
  sessionLoadDone: "Profile loaded: values, allowlist, patterns and rules restored.",

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
// variants keeps this table one row per category and keeps the guard working.
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
};
