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
    { label: "Identify", body: "Pick a preset and fine-tune the 23 detection categories, then review every suggested value. Nothing is replaced until you accept it." },
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
  tabWhat: "What to anonymise",
  tabAI: "AI and advanced settings",
  presetHint: "Start from a preset, then adjust the checkboxes if you need to. Changing any checkbox switches the preset to Custom.",
  groupContact: "Contact and account details",
  groupNames: "Names",
  groupThorough: "Only for thorough anonymisation",
  useAILabel: "Use local AI (Ollama)",
  useAIHint: "When enabled, a language model running on this machine can suggest names to replace and double-check the result. Nothing leaves your computer.",
  contextSizeHint: "Higher values let the AI read longer documents at once but use more memory.",
  aiOffTooltip: "Local AI is turned off. Enable it under Configure, AI and advanced settings.",
  allowTitle: "Never anonymise these terms",
  allowHint: "Terms in this list survive every pass, even when they also appear as names to replace.",
  // BUILD-04 CR9: the group that surfaces the BUILD-03 recognizers.
  groupTechnical: "Payment, tax and technical identifiers",
  // BUILD-04 CR10: the per-group bulk buttons.
  selectAll: "Select all",
  deselectAll: "Deselect all",
  // BUILD-04 CR11: the allowlist bulk button.
  clearAll: "Clear all",
  clearAllConfirm: "Remove every term from the never anonymise list? The terms the application seeds at startup can be added back by restarting it, or from a CSV.",
  // BUILD-04 CR9: the detection-confidence control. Plain language, with
  // the two thresholds that actually change something spelled out.
  confidenceTitle: "Detection confidence",
  confidenceLabel: "Minimum confidence",
  confidenceHint: "Every detection carries a score for how certain it is. Anything below the minimum you set here is left alone. Keep it at 0 to replace everything that is found, which is how the application behaves by default.",
  confidenceScale: "Above 80, the values that only the local AI suggested are left alone. Above 95, the values you listed yourself are left alone too, and only pattern matches remain.",
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
  tabScope: "Scope",
  tabSmart: "Smart detection",
  tabLocalAI: "Local AI",
  tabCloudAI: "Cloud AI",

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
  // Smart detection tuning.
  smartSettingsTitle: "Smart detection settings",
  smartSettingsHint: "Smart detection guesses which words are names from how they are written, so it always proposes some things that are not names. These settings decide how strict it is. Set them all to zero, and untick the box, to see everything it can find.",
  smartMinLength: "Shortest value",
  smartMinLengthHint: "Suggestions shorter than this many letters are skipped.",
  smartMinOccurrences: "Fewest occurrences",
  smartMinOccurrencesHint: "How often a value must appear before it is suggested. 1 means once is enough.",
  smartCommonWords: "Skip ordinary words",
  smartCommonWordsHint: "Ignores month names, weekdays and common sentence openers, which are capitalised without being names.",
  smartMinConfidence: "Minimum certainty",
  smartMinConfidenceHint: "Higher values keep only the strongest suggestions, such as a name followed by a company form or introduced by a title.",

  // Suggestions table.
  colValue: "Value",
  colType: "Type",
  colOccurrences: "Occurrences",
  colFoundBy: "Found by",
  colActions: "Actions",
  searchPlaceholder: "search values",
  filterAllTypes: "All types",
  sortValueHint: "Sort by value, A to Z or Z to A.",
  sortCountHint: "Sort by how often the value occurs.",
  noMatchingSuggestions: "No suggestion matches the current search and type filter.",
  bulkScopeHint: "Applies to the rows shown below, so a search or a type filter limits it too.",
  denyAllConfirm: (n) => `Reject ${n} suggestion${n === 1 ? "" : "s"}? They are removed from the review list and nothing is replaced.`,
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
  client_names: ["Client names", "The client names you list in the Values step"],
  project_names: ["Project names", "The project names you list in the Values step"],
  internal_names: ["Internal names", "Internal staff, teams and systems"],
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
