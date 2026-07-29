// copy.js, the single home for user-visible strings (BUILD-02 Phase 2e).
//
// Keeping copy in one module gives the style guard (copy.test.js) and a
// future i18n pass exactly one place to look. Style rules (BUILD-02 ground
// rule 4): no em dashes, no "+" as a stand-in for "and", no unexplained
// jargon such as "PII" without an example, full sentences.

// STEP_BANNERS: the per-step explainer strip shown at the top of every
// wizard step (title, body, icon name from icons.js).
export const STEP_BANNERS = {
  import: {
    title: "Import",
    body: "Add the documents you want to anonymise. You can drag files here or browse for them. Your files are only read, never changed.",
    icon: "upload_file",
  },
  configure: {
    title: "Configure",
    body: "Choose what kinds of information to hide, and decide whether to use the optional local AI.",
    icon: "tune",
  },
  // BUILD-04 CR3: the step token and every user-visible word changed from
  // "entities" to "values". Engine category identifiers are untouched.
  values: {
    title: "Values",
    body: "Tell the app the values it should replace. You can add them yourself or let the app suggest candidates for your review.",
    icon: "badge",
  },
  run: {
    title: "Run",
    body: "Run the anonymisation and check the result side by side.",
    icon: "play_arrow",
  },
  export: {
    title: "Export",
    body: "Save the anonymised copies and, if you need it, the re-identification key.",
    icon: "download",
  },
};

// WORKFLOW: the banner that holds the five step chips (BUILD-04 CR7).
// It sits under the permanent top menu, so the header itself no longer
// changes shape when the user enters or leaves the wizard.
export const WORKFLOW = {
  title: "Anonymisation workflow",
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
  // The visible step names, for the confirmation sentence. Kept here
  // rather than reusing STEP_LABELS because those carry the "3 " prefix.
  stepNames: {
    import: "Import",
    configure: "Configure",
    values: "Values",
    run: "Run",
    export: "Export",
  },
  backConfirm(step) {
    const name = NAV.stepNames[step] ?? step;
    return `Going back will reset the ${name} step. Your imported documents are kept. Continue?`;
  },
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
// `steps` feeds the "five steps" sidebar next to the hero: a plain-language
// walk through the wizard so a first-time user knows what they are signing
// up for before they click Anonymise documents. The labels here are
// display-only; they are deliberately not the wizard's own step tokens
// (state.js WIZARD_STEPS) or NAV.stepNames, which keep "Values" and "Run".
export const HOME = {
  headline: "Anonymise your documents safely",
  body: [
    "doc-anonymiser is a simple yet powerful application that helps you anonymise documents directly from your workstation. It replaces names, personal details and other sensitive information with consistent placeholders, making your documents safer to share or process.",
    "You remain in control throughout the process. Choose from a wide range of predefined patterns or use AI-powered discovery to identify information that may need to be anonymised. You can then review what has been detected and decide which data to replace.",
    "Depending on your security and confidentiality requirements, you can run the entire process locally or connect to an AI endpoint.",
  ],
  stepsTitle: "The five steps",
  steps: [
    { label: "Import", body: "Drop in .docx, .pptx, .xlsx, .pdf, .csv, .md or .txt files. Your originals are only ever read, never changed." },
    { label: "Configure", body: "Pick a preset, then fine-tune the 23 detection categories to fit the document." },
    { label: "Identify", body: "Go through every candidate one by one. Nothing is replaced until you accept it." },
    { label: "Anonymise", body: "Check the result side by side, with every replacement mapped back to its original." },
    { label: "Export", body: "Save the anonymised copies, the report and the re-identification key." },
  ],
  docsLink: "Read the documentation",
};

// The documentation placeholder page was retired by BUILD-04 CR6: real
// documentation now lives in frontend/docs/index.html and opens in its own
// window, so there is no in-app docs screen and no placeholder string.

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
