# doc-anonymiser

**Anonymise client documents entirely on your own machine, nothing ever
leaves it.**

doc-anonymiser is a small desktop application (Windows first, Linux
secondary, macOS best-effort) that removes personally identifiable
information and engagement-specific names from text-based documents:

- `.txt` — plain text
- `.csv` — spreadsheets exported as CSV (round-trips back to CSV on export)
- `.md` — Markdown
- `.docx` — Word documents
- `.pptx` — PowerPoint presentations
- `.xlsx` — Excel workbooks
- `.pdf` — PDF documents (experimental)

Office and PDF files are converted to Markdown on import for preview and
processing, and export as text formats (`.md`, `.txt`, `.csv`, or `.json`).
Word, PowerPoint and Excel files can ALSO export as a **same-format
anonymised copy** (`.docx`, `.pptx`, `.xlsx`): the app rewrites a copy of
the original bytes held in memory, so the layout, styles and images are
preserved. One limitation: a replacement that spans differently formatted
text runs (for example a name whose first half is bold) adopts the
formatting of its first run. Document properties (title, author, company,
custom fields) and the export filename go through the same anonymisation
with an explicit review step; nothing is rewritten silently. The app never
modifies your original files: when you choose a same-format export, it
writes a new anonymised copy and your source file is left exactly as it
was. PDF support is experimental: it reads the text layer only and does
not perform OCR, so scanned PDFs cannot be processed.

It replaces emails, phone numbers, IBANs, national IDs, VAT numbers, person
names, client names, project names and more with stable placeholders such as
`[PERSON_1]` or `[CLIENT_2]`, consistently across every document you load.
A re-identification key (original → placeholder) can be exported so the
process is reversible by you — and only by you.

The app opens on a Home page; "Anonymise documents" starts the wizard,
and your work survives navigating between Home and the wizard.

## The wizard flow

1. **Import**: open your documents through a native file dialog (or drag
   and drop). CSV files and flat Excel sheets are shown as a table for easy
   review; each Excel sheet becomes its own document. The list and preview
   panes are resizable.
2. **Configure**: two focused screens. "What to anonymise" starts from a
   preset (Soft, Standard — the default, or Thorough) over granular
   per-category checkboxes (emails, phone numbers, bank accounts, names,
   dates, amounts, ...), plus the allowlist with CSV import and a
   downloadable template. "AI and advanced settings" holds the master
   "Use local AI (Ollama)" toggle, port, model and context size.
3. **Entities**: three discovery methods: auto-discovery with the local
   AI (when enabled), always-available **Smart detection** (finds likely
   names offline by how they are written, with a Luxembourg-aware
   legal-form gazetteer), and a cloud placeholder for later. EVERY
   suggestion lands in one review list; nothing is replaced until you
   accept it. Manual entries show a live "Found N times in M documents"
   preview, and variants can be regrouped between entities by
   drag-and-drop.
4. **Run**: execute the pipeline with live progress, review the
   side-by-side before/after with highlighted replacements, hover a
   placeholder to see the original, click it to reassign the value as a
   variant of another entity, and fix anything missed with a fast re-run.
5. **Export**: save the anonymised documents through a save dialog
   (single files, a zip of everything, or the clipboard). CSV files come
   back out as CSV; Word, PowerPoint and Excel files can export a
   same-format copy with layout preserved (PDF experimentally, as a
   simplified regenerated layout), each behind a document-properties
   review. Your original files are never modified.

## Optional: local AI assistance with Ollama

The core anonymisation engine is fully deterministic and works out of the
box with **no AI and no internet**. If you want smarter entity discovery
(finding names the regex pass cannot know about), you can optionally install
[Ollama](https://ollama.com/download) and pull a small local model:

```
ollama pull qwen2.5:3b-instruct
```

- **With Ollama running:** the app gains an "entity discovery" step and a
  "deep scan" pass that suggest additional entities found in your text.
  Every suggestion is verified against the source text before being used.
- **Without Ollama:** those two features are simply greyed out. Everything
  else works exactly the same.

The app only ever talks to Ollama on `http://127.0.0.1:11434` — your own
machine, never a remote server.

## Download and run

1. Go to the [Releases page](https://github.com/Rosca75/doc-anonymiser/releases)
   and download the latest `doc-anonymiser-vX.Y.Z-windows.zip`
   (or the Linux zip on Linux).
2. Unzip it anywhere (e.g. your Desktop).
3. Read `README-FIRST.txt` inside the zip, then double-click
   `doc-anonymiser.exe`.
4. On Windows, SmartScreen may warn about an unknown publisher: click
   **More info → Run anyway**. The binary is built publicly on GitHub
   Actions from this repository's source.

## Screenshots

_Screenshots will be added here as the UI takes shape._

## Privacy statement

- No data ever leaves your machine. The application performs **no network
  I/O at all**, with one exception: HTTP requests to `127.0.0.1:11434`
  (the loopback address) when you have optionally installed Ollama.
- No telemetry, no crash reporting, no update checks, no remote fonts or
  CDNs. All assets are embedded in the binary.
- Sensitive state (the re-identification registry) stays in memory unless
  you explicitly save a session, in which case the app warns you that the
  saved file contains the re-identification key.
- The app never modifies your original files. When you choose a
  same-format export, it writes a new anonymised copy; your source file
  is left exactly as it was.
- Same-format exports also anonymise the document properties stored
  inside Office files (title, author, company, custom fields) and propose
  an anonymised export filename, each behind an explicit review panel so
  you decide field by field.
