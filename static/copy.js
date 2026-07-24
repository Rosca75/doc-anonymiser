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
  entities: {
    title: "Entities",
    body: "Tell the app the names it should replace. You can add them yourself or let the app suggest candidates for your review.",
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

// Home page copy (BUILD-02 Phase 2b).
export const HOME = {
  headline: "Anonymise client documents on this machine",
  lede: "doc-anonymiser replaces names, personal details and other sensitive values in your documents with consistent placeholders, so you can share or process them safely. Everything happens locally.",
  panels: [
    {
      title: "Anonymise documents",
      body: "Import text, Word, PowerPoint, Excel or PDF files, review what will be replaced, and export anonymised copies.",
      icon: "description",
    },
    {
      title: "Everything stays on this machine",
      body: "No cloud services, no telemetry. Your files are read once and never modified.",
      icon: "cloud_off",
    },
    {
      title: "Optional local AI",
      body: "If Ollama is installed, a local model can suggest names to replace. The app works fully without it.",
      icon: "smart_toy",
    },
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
  client_names: ["Client names", "The client names you list in the Entities step"],
  project_names: ["Project names", "The project names you list in the Entities step"],
  internal_names: ["Internal names", "Internal staff, teams and systems"],
  person_names: ["Person names", "For example Marie Duval, M. Duval or just Marie"],
  custom_patterns: ["Custom patterns", "The regular expressions you add in the Entities step"],
  date: ["Dates", "For example 15 January 2026 or 15/01/2026"],
  organisation_names: ["Organisation names", "Organisations suggested by the AI review"],
  location_names: ["Place names", "Cities and places suggested by the AI review"],
  amount: ["Money amounts", "For example EUR 12,500"],
};
