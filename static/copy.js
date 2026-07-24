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
