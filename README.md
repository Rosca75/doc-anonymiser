# doc-anonymiser

**Anonymise client documents entirely on your own machine. Nothing leaves your computer.**

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
the original bytes held in memory, so the layout and styles are preserved.
Pictures are preserved too unless you decide otherwise: see **Images**
below, because that decision is yours and the app makes it visible before
you save. One limitation: a replacement that spans differently formatted
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
`[PERSON_1]` or `[ENTITY_2]`, consistently across every document you load.
A re-identification key (original → placeholder) can be exported so the
process is reversible by you — and only by you.

The app opens on a Home page; "Anonymise documents" starts the wizard,
and your work survives navigating between Home and the wizard.

## Values and Suggestions

Two words carry the whole model, and the difference between them is the point.

A **Value** is one thing to be replaced: a person, a company, a project, a
reference code. It has a main text, any number of alternative **spellings** of
the same thing ("Marie Duval", "M. Duval", "Duval"), and ONE placeholder for the
whole family, so an informal mention and a formal one never become two different
numbers.

A **Suggestion** is something the app THINKS might be a Value. Suggestions are
never applied. You accept or reject each one, and the wizard will not let you
move on to anonymising while any is still waiting, because walking past a
Suggestion silently answers "reject" on your behalf.

## The wizard flow

1. **Import**: open your documents through a native file dialog (or drag
   and drop). CSV files and flat Excel sheets are shown as a table for easy
   review; each Excel sheet becomes its own document. The list and preview
   panes are resizable.
2. **Identify**: one screen with two halves. On the left you choose how the app
   looks; on the right you review what it found.

   The left rail holds two switchable **detection routes**:

   - **Smart detection** (on, and needs nothing installed) has three parts.
     *Built-in patterns* find structured signals: emails, phone numbers, VAT
     numbers, IBANs. Those are matched and replaced directly, because a pattern
     is not a guess. *Signal-based suggestions* use one of those matches as
     EVIDENCE: an address like `pierre.dupont@tpps.com` says a person called
     Pierre Dupont and a company whose name starts "Tpps" are involved, so if
     either is written elsewhere in your files it is suggested for review.
     *Heuristic discovery* finds recurring names by how they are written, with a
     Luxembourg-aware legal-form gazetteer.
   - **Local AI** (off by default) hands the documents to a model running on your
     own machine and suggests what it finds.

     Smart detection also owns the scope both routes read: the document country,
     the preset (Soft, Standard the default, or Thorough), the per-category
     checkboxes, and the match-confidence floor. Every control explains itself
     through a small information icon, on hover or by keyboard, so the panel stays
     a list of controls rather than a wall of text.

   The right half is the review workspace: **Suggestions** waiting for a decision
   and **My Values** already accepted, plus the never-anonymise list, the
   read-only **Built-in patterns** list and your own **Custom patterns**. Each
   Suggestion says which methods found it and why, so you can judge
   it rather than take it on trust. The Built-in patterns tab shows what the
   signal categories you switched on matched the last time you ran detection,
   grouped by category and keeping the heading of a category that matched
   nothing, so ticking a category is something you can check before anything is
   anonymised. It offers no accept or reject, because a built-in pattern is a rule
   you chose and its matches are applied without review. Manual entries show a live "Found N times in M
   documents" preview, and spellings can be dragged between Values to regroup
   them.
3. **Anonymise**: two tabs over the same document. **TEXT** runs the pipeline
   with live progress, then shows the side-by-side before and after with
   highlighted replacements. Hover a placeholder to see the original; select text
   in either pane to make it a spelling of an existing Value or a Value of its
   own. **Add missed Value** declares anything the run did not catch, and a fast
   re-run applies it while every existing placeholder keeps its number. **IMAGE**
   lists every picture in the document and what happens to it on export, one
   decision per picture (see **Images** below).

   This step is fully deterministic. It runs no discovery of any kind and never
   contacts the model, so nothing can be replaced that you did not accept.
4. **Export**: save the anonymised documents through a save dialog
   (single files, a zip of everything, or the clipboard). CSV files come
   back out as CSV; Word, PowerPoint and Excel files can export a
   same-format copy with layout preserved (PDF experimentally, as a
   simplified regenerated layout), each behind a document-properties
   review. Your original files are never modified.

## Images

Anonymising the words is only half of a Word document or a PowerPoint deck. The
client logo, the screenshot of the client's own system, the photo of the team:
those are personal and engagement-specific data too, and a same-format export
copies them out of the original file untouched unless you say otherwise.

The **IMAGE** tab on step 3 lists every picture in the selected document with
where it appears, and gives each one a decision:

- **Keep it.** The default for every picture, and what happens if you never open
  the tab.
- **Replace with a box.** The picture becomes a plain rectangle the same size,
  carrying your own short caption ("Client logo removed"). The original pixels are
  gone from the exported file.
- **Blur.** The picture is broken into blocks, each replaced by its own average,
  and then smoothed. This throws the detail away rather than smearing it, which is
  why it is not a light blur you can read through. It is still not a guarantee:
  the shape, the colours and the layout of what was there remain visible, so for
  anything that must not be recognisable at all, use a box or remove it.
- **Remove.** The picture is deleted from the page AND its bytes are overwritten
  inside the file, because a picture left in the file that the page no longer
  draws is still a picture anyone can extract.

One decision covers every place a picture is used: a logo on five slides is one
question, and the row tells you it appears in five places. An SVG picture cannot
be blurred, and the control says why: a blur filter over a drawing leaves every
original shape and every original text string inside the file.

Where the review is offered:

- **Word and PowerPoint**: fully reviewed, and your decisions reach the
  same-format copy.
- **PDF**: not offered, and nothing is lost by it. A PDF export rebuilds the
  document from the anonymised text, so every picture in a source PDF is already
  absent from the file the app writes.
- **Excel**: not offered. A spreadsheet's pictures are left as they are.
- **Text, CSV and Markdown**: there are no pictures in them.

The properties review that opens before a same-format save states what will
happen, including when nothing will: "This copy keeps all 7 of the document's
images, exactly as they are." The exported report says the same in full, naming
each changed picture, where it appeared and what was done to it.

## Optional: local AI assistance with Ollama

The app works out of the box with **no AI and no internet**: built-in patterns,
signal-based suggestions and heuristic discovery all run offline. If you want a
model to look as well, you can optionally install
[Ollama](https://ollama.com/download) and pull a small local model:

```
ollama pull qwen3.5:0.8b
```

- **With Ollama running:** the Local AI route becomes switchable on Identify. It
  suggests Values it found, and it can also re-file what Smart detection found
  into better categories. Everything it proposes is checked against the source
  text first, so a name the model invented is dropped rather than offered.
- **Without Ollama:** that route is greyed out with a tooltip saying so.
  Everything else works exactly the same.

Detecting Ollama ENABLES the switch; it never flips it. Handing your documents to
a model, however local, stays your decision.

### Make it a little faster: let Ollama use your GPU

Ollama ships with Vulkan support enabled but **ignores integrated GPUs unless you
tell it not to**, and most business laptops have exactly that. It writes
`dropping integrated GPU` in its log and runs the model on the CPU instead.

On Windows the variable has to be set for the **Ollama service**, not in a
terminal window. A variable exported in a shell reaches nothing, and that is the
mistake that makes people conclude the setting does not work:

```
setx OLLAMA_IGPU_ENABLE 1
```

Then quit Ollama from the system tray and start it again.

To confirm it took, open `%LOCALAPPDATA%\Ollama\server.log` and look for the
GPU listed as inference compute and a line reading `offloaded N/N layers to GPU`,
instead of `dropping integrated GPU`.

What to expect: on the reference laptop (an Intel Arc 140V integrated GPU) this
measured **about 1.2x faster** on a 15-slide deck, and the model found somewhat
more than it did on the CPU. It is free and it does no harm, but it is a modest
improvement rather than a transformation, and it does not turn a long scan into a
short one. Scanning fewer pages at a time, or choosing a different model, moves
the clock more than this does.

Discrete NVIDIA and AMD GPUs are used automatically and need none of this.

The app only ever talks to Ollama on `http://127.0.0.1:11434` — your own
machine, never a remote server. There is no cloud option, and nothing in the
application can be pointed at a remote host.

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
  saved file contains the re-identification key. A session file is read only by
  the version that wrote it: one from another version is refused with a clear
  message rather than half-read, because a half-read key silently renumbers
  placeholders and two exports of one engagement stop agreeing.
- The app never modifies your original files. When you choose a
  same-format export, it writes a new anonymised copy; your source file
  is left exactly as it was.
- Same-format exports also anonymise the document properties stored
  inside Office files (title, author, company, custom fields) and propose
  an anonymised export filename, each behind an explicit review panel so
  you decide field by field.
