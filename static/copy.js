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

// Home page copy (BUILD-02 Phase 2b, rewritten for BUILD-04 CR1).
//
// The body is an ARRAY of three paragraphs, rendered one <p> each by
// views/home.js. The three cover, in order: who stays in control, the two
// ways sensitive information is found, and where the processing happens.
// They replace the former single lede plus three feature panels, which said
// the same three things twice.
export const HOME = {
  headline: "Anonymise your documents safely",
  body: [
    "You stay in control from the first step to the last. The app shows you every value it proposes to replace, and nothing is replaced until you accept it. Your original files are only ever read, never changed, and the anonymised copies are written where you choose to save them.",
    "Two ways of finding sensitive information work side by side. Predefined patterns catch everything that always looks the same, such as email addresses, phone numbers, bank account numbers and national identification numbers. AI powered discovery looks for the rest, the names of people, clients and projects that no pattern can predict, and puts each one on a review list for you to accept or reject.",
    "Everything runs on this machine by default. The predefined patterns need no network connection at all. The optional AI features talk to a language model running on 127.0.0.1, the address of your own computer, so your documents never leave it. The app makes no other network connection: no cloud service, no telemetry, no update check.",
  ],
};

// Documentation placeholder page (BUILD-02 Phase 2c).
export const DOCS_PLACEHOLDER = "Documentation is coming soon.";

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
};
