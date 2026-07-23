# doc-anonymiser

**Anonymise client documents entirely on your own machine — nothing ever
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

Office and PDF files are converted to Markdown on import and exported as text
formats (`.md`, `.txt`, `.csv`, or `.json`) — the app never writes back a
binary Office or PDF file. PDF support is experimental: it reads the text
layer only and does not perform OCR, so scanned PDFs cannot be processed.

It replaces emails, phone numbers, IBANs, national IDs, VAT numbers, person
names, client names, project names and more with stable placeholders such as
`[PERSON_1]` or `[CLIENT_2]`, consistently across every document you load.
A re-identification key (original → placeholder) can be exported so the
process is reversible by you — and only by you.

## The three-step flow

1. **Import** — open your documents through a native file dialog (or drag
   and drop). CSV files are shown as a Markdown table for easy review.
2. **Anonymise** — pick a level (`soft`, `medium` — the default, or
   `advanced`), review what will be replaced, add or remove entities, and
   maintain an allowlist of terms that must never be touched.
3. **Export** — save the anonymised documents through a save dialog. CSV
   files come back out as CSV. Your original files are never modified.

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
