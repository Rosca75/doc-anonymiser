// views/anonymiseimages.js, the IMAGE half of wizard step 3.
//
// Step 3 has two halves and they answer two different questions about the same
// document. The TEXT half (anonymise.js) is about what the pipeline replaced in
// the words; this half is about the PICTURES, which the pipeline never touches:
// a same-format export copies every archive entry it has no rewriter for, so a
// picture leaves the machine untouched unless the user decides otherwise here.
//
// It is a sibling module of anonymise.js for the reason identifyrail.js and
// identifyworkspace.js are siblings of identify.js: one screen, halves big
// enough to own a file each. anonymise.js owns the tab bar, the shared document
// selector and the footer; everything below the tab bar on the IMAGE side is
// here.
//
// The surface, top to bottom:
//
//   the banner        the document selector, the status filter with live counts,
//                     and the Details / Tiles view toggle.
//   the list          the ONLY scrolling element on this screen: either the
//                     seven-column details grid or the tiles.
//   the panel         the treatment panel, an in-app overlay (never a native
//                     dialog) holding the live preview, the three treatments,
//                     their parameters and Apply / Cancel.
//
// A decision is attached to the picture FILE and applies to every place it
// appears, which is why a row says "appears in N places" rather than offering N
// decisions: answering per place would need the exporter to clone picture parts
// and rewrite relationships.

import {
  listDocumentImages, imageThumbnail, previewImageTreatment, setImageDecision,
} from "../api.js";
import {
  getState, setState,
  IMAGE_STATUS_FILTERS, IMAGE_TREATMENTS, IMAGE_FORMATS, IMAGE_KINDS,
  imageDocName, imagesFor, imageAssets, imageStatus, imageStatusCounts,
  filteredImages, imageThumbKey, treatmentAvailable, treatmentBlockedReason,
  startImageScan, setImageInventory, setImageScanError, cacheImageThumb,
  applyImageDecision, openImageEditor, updateImageEditor, closeImageEditor,
  setImageFilter, setImageLayout,
} from "../state.js";
import { escapeHTML } from "../html.js";
import { button, card, chipRow, sectionLabel, toastHTML } from "../ui.js";
import { notify, wireNotice } from "../toast.js";
import { CARDS, IMAGES } from "../copy.js";

// --- Constants ------------------------------------------------------------

// The details grid's column template, declared ONCE and shared by the header
// and every row. Two templates for one grid is how a header and its rows drift
// apart, which is the reason identifyworkspace.js declares SUGGESTION_COLUMNS
// the same way. The last track is the two direct buttons.
const IMAGE_COLUMNS =
  "4rem minmax(0,1fr) 4.5rem 6.5rem 5rem minmax(6rem,10rem) 5.5rem 11rem";

// The longest side of a row or tile preview, in pixels. Asking Go for a
// 48-pixel thumbnail and drawing it at 48 keeps the cache small; asking for the
// full picture and letting CSS shrink it would hold a deck's worth of
// screenshots in memory for a column four characters wide.
const THUMB_MAX_PX = 96;

// The longest side of the treatment panel's preview. It is the same picture the
// export will write, downscaled, so it has to be big enough to judge a blur by.
const PREVIEW_MAX_PX = 480;

// How many previews one paint may ask for. A two-hundred-picture deck must not
// fire two hundred bridge calls on its first paint, so a paint loads the batch
// the reader can actually see and the next scroll asks for the next one.
const THUMB_BATCH = 12;

// How far outside the list's visible box a row still counts as worth loading,
// in pixels, so a slow scroll meets pictures that are already there.
const THUMB_MARGIN = 200;

// How long the panel waits after a keystroke or a slider move before asking Go
// to render the preview again. Every change would otherwise be one render of
// the real treatment, and holding a slider would queue ten of them.
const PREVIEW_DEBOUNCE_MS = 200;


// --- View-local state -----------------------------------------------------

// Which previews are IN FLIGHT, by imageThumbKey, so a paint that lands while a
// request is running does not fire a second one. What stops a THIRD request is
// the cache rather than this set: every outcome writes it, a failure writing the
// empty string (the cell then reads "No preview"), which is what stops a
// bridgeless page retrying the same picture on every repaint forever.
const thumbsInFlight = new Set();

// The pending preview render, so a keystroke replaces the previous one instead
// of queueing beside it.
let previewTimer = null;

// The document-level Escape handler, kept so a repaint can remove the previous
// one. Without this the listeners accumulate one per paint, and after twenty
// repaints a single Escape would close twenty panels that are already gone.
let escapeListener = null;

// --- The tab bar's count --------------------------------------------------

/**
 * imageCount(s) is the number the IMAGE tab's badge carries, or null when the
 * tab must show no badge at all.
 *
 * Null covers both "not asked yet" and "this format has no image review": a
 * badge reading 0 on a .txt file would state that the document was reviewed and
 * had no pictures, which is a different claim from "there is nothing here to
 * review".
 *
 * @param {object} s state
 * @returns {number|null} the count, or null for no badge
 */
export function imageCount(s) {
  const record = imagesFor(s, imageDocName(s));
  if (!record?.inventory?.applicable) return null;
  return (record.inventory.assets ?? []).length;
}

// --- The surface ----------------------------------------------------------

/**
 * imageTabHTML(s) is the whole IMAGE half below the tab bar.
 *
 * Full width rather than a column beside the text cards, because the details
 * view has seven columns and a preview: half a workspace would make it a
 * horizontal scroller, and the fixed-height layout contract says wide content
 * scrolls inside its own container, not that a screen may be built to need it.
 *
 * @param {object} s state
 * @returns {string} safe HTML
 */
export function imageTabHTML(s) {
  const docName = imageDocName(s);
  const record = imagesFor(s, docName);
  return `<div class="workspace workspace-images">` +
    card({
      id: "image-card", cls: "image-card",
      title: CARDS.images.title,
      subtitle: CARDS.images.subtitle,
      beforeBodyHTML: bannerHTML(s, docName, record) + warningsHTML(record),
      bodyHTML: listHTML(s, docName, record),
      bodyCls: "image-list",
      bodyId: "image-list",
      afterBodyHTML: toastHTML(s.notice),
    }) +
    `</div>` +
    treatmentPanelHTML(s);
}

/** documentSelect(s, docName) is the SHARED document selector: it reads and
 *  writes the same state.resultDoc the Compare card uses, so switching tabs
 *  keeps the user on the same file and switching file keeps them on the same
 *  tab. It lists the IMPORTED documents, because the pictures are in the bytes
 *  captured at import and this half needs no run. */
function documentSelect(s, docName) {
  const options = (s.documents ?? []).map((d) =>
    `<option value="${escapeHTML(d.name)}"${d.name === docName ? " selected" : ""}>` +
    `${escapeHTML(d.name)}</option>`).join("");
  if (!options) return "";
  return `<select id="image-doc" class="compare-select"` +
    ` aria-label="${escapeHTML(IMAGES.documentLabel)}">${options}</select>`;
}

/**
 * bannerHTML(s, docName, record) is the strip between the card head and the
 * list: the document selector, the status filter and the view toggle.
 *
 * It sits OUTSIDE the scrolling body, so a filter stays reachable from the bottom
 * of a two-hundred-picture deck.
 *
 * The document selector renders whatever the answer is, and the filter and the
 * toggle only when there is a list to look at. That asymmetry is the point: a
 * filter over an answer with no list is a control that cannot do anything, while
 * a document selector that vanished on a .txt file would trap the user on the one
 * document that has nothing to review.
 */
function bannerHTML(s, docName, record) {
  const applicable = !!record?.inventory?.applicable;
  const counts = imageStatusCounts(s);
  const filters = IMAGE_STATUS_FILTERS.map((id) => ({
    id, label: IMAGES.filterChip(id, counts[id] ?? 0),
    active: (s.imageFilter ?? "all") === id,
  }));
  const layouts = [
    { id: "details", label: IMAGES.layoutDetails, active: (s.imageLayout ?? "details") === "details" },
    { id: "tiles", label: IMAGES.layoutTiles, active: s.imageLayout === "tiles" },
  ];
  const filterBlock = applicable
    ? sectionLabel(IMAGES.filterLabel, { mini: true }) +
      chipRow(filters, { attr: "imgfilter", square: true, ariaLabel: IMAGES.filterLabel })
    : "";
  const layoutBlock = applicable
    ? sectionLabel(IMAGES.layoutLabel, { mini: true }) +
      chipRow(layouts, { attr: "imglayout", square: true, ariaLabel: IMAGES.layoutLabel })
    : "";
  // The label goes with the selector: a caption over a control that is not there
  // (no document is imported at all) names nothing.
  const select = documentSelect(s, docName);
  const documentBlock = select
    ? sectionLabel(IMAGES.documentLabel, { mini: true }) + select
    : "";
  return `<div class="image-banner">` +
    `<div class="image-banner-left">${documentBlock}${filterBlock}</div>` +
    `<div class="image-banner-right">${layoutBlock}</div></div>`;
}

/** warningsHTML(record) prints the per-document notes Go answered with as
 *  CODES. An unknown code prints nothing rather than its own identifier: a
 *  reader has no use for a JSON key. */
function warningsHTML(record) {
  const codes = record?.inventory?.warnings ?? [];
  const lines = codes.map((code) => IMAGES.warning[code]).filter(Boolean);
  if (lines.length === 0) return "";
  return lines.map((line) =>
    `<div class="banner warn image-warning">${escapeHTML(line)}</div>`).join("");
}

/** listHTML(s, docName, record) is the card body: the list, or the one sentence
 *  that stands in for it. */
function listHTML(s, docName, record) {
  if (!docName) return message(IMAGES.empty);
  if (record?.error) return `<div class="banner warn">${escapeHTML(IMAGES.scanFailed(record.error))}</div>`;
  if (!record || (record.loading && !record.inventory)) return message(IMAGES.loading);

  const inventory = record.inventory;
  // applicable: false is an ANSWER, not a failure, and the reason is a CODE the
  // frontend turns into its own sentence. The tab stays where it is: a tab that
  // appears and disappears as the user changes file reads as a bug, and the
  // sentence is the answer to the question a missing tab would raise.
  if (!inventory?.applicable) {
    return message(IMAGES.reason[inventory?.reason] ?? IMAGES.reason.format_not_supported);
  }
  if (imageAssets(s).length === 0) return message(IMAGES.empty);

  const shown = filteredImages(s);
  if (shown.length === 0) return message(IMAGES.noneMatchFilter);
  return s.imageLayout === "tiles"
    ? tilesHTML(s, docName, shown)
    : detailsHTML(s, docName, shown);
}

/** message(text) is the list's stand-in when there is nothing to list. */
function message(text) {
  return `<p class="hint image-message">${escapeHTML(text)}</p>`;
}

// --- The details view -----------------------------------------------------

/** detailsHTML(s, docName, assets) is the header row plus one row per asset. */
function detailsHTML(s, docName, assets) {
  return `<div class="image-grid">` + detailsHead() +
    assets.map((a) => detailsRow(s, docName, a)).join("") +
    `</div>`;
}

/** detailsHead() is the seven column headings, plus the actions track. The
 *  actions cell carries no heading because its two buttons say what they do. */
function detailsHead() {
  const heads = [
    IMAGES.colPreview, IMAGES.colName, IMAGES.colFormat, IMAGES.colDimensions,
    IMAGES.colSize, IMAGES.colLocation, IMAGES.colStatus,
  ].map((h) => `<span class="col-head">${escapeHTML(h)}</span>`).join("");
  return `<div class="grid-head image-row" style="grid-template-columns:${IMAGE_COLUMNS}">` +
    heads + `<span class="col-actions"></span></div>`;
}

/** detailsRow(s, docName, asset) is one picture's row. */
function detailsRow(s, docName, asset) {
  const place = locationCell(asset);
  const dims = IMAGES.dimensions(asset.width, asset.height);
  const displayed = displayTitle(asset);
  return `<div class="grid-row image-row" style="grid-template-columns:${IMAGE_COLUMNS}"` +
    ` data-imgasset="${escapeHTML(asset.id)}">` +
    `<span class="cell-preview">${previewCell(s, docName, asset)}</span>` +
    `<span class="cell-name" title="${escapeHTML(baseName(asset.id))}">` +
    `${escapeHTML(asset.name || baseName(asset.id))}</span>` +
    `<span class="cell-format">${escapeHTML(formatLabel(asset))}</span>` +
    `<span class="cell-dimensions"${displayed ? ` title="${escapeHTML(displayed)}"` : ""}>` +
    `${escapeHTML(dims)}</span>` +
    `<span class="cell-size">${escapeHTML(IMAGES.fileSize(asset.bytes))}</span>` +
    `<span class="cell-location" title="${escapeHTML(place.title)}">${escapeHTML(place.text)}</span>` +
    `<span class="cell-status">${statusChip(asset)}</span>` +
    `<span class="col-actions">${rowActions(asset)}</span>` +
    `</div>`;
}

/** rowActions(asset) is the two direct answers, so the common decisions cost
 *  one click each. */
function rowActions(asset) {
  const kept = imageStatus(asset) === "kept";
  return button(IMAGES.keep, {
    kind: "secondary", cls: "image-action", disabled: kept,
    title: IMAGES.keepTitle, data: { imgkeep: asset.id },
  }) + button(IMAGES.anonymise, {
    kind: "secondary", cls: "image-action",
    title: IMAGES.anonymiseTitle, data: { imgedit: asset.id },
  });
}

// --- The tiles view -------------------------------------------------------

/**
 * tilesHTML(s, docName, assets) is one card per picture.
 *
 * The card is a FIXED-HEIGHT surface, for the reason frontend/CLAUDE.md gives
 * about value cards: when one card grows, every card below it moves, the browser
 * clamps the grid's scroll offset to the shorter content, and the next repaint
 * snapshots the clamped value, so the reader's place is lost for good. So the
 * preview box is fixed, the metadata list is a fixed number of lines with the
 * location's overflow behind a title, and the status is a chip rather than a
 * sentence that can wrap.
 */
function tilesHTML(s, docName, assets) {
  return `<div class="image-tiles">` +
    assets.map((a) => tile(s, docName, a)).join("") + `</div>`;
}

function tile(s, docName, asset) {
  const place = locationCell(asset);
  const dims = IMAGES.dimensions(asset.width, asset.height);
  return `<article class="image-tile" data-imgasset="${escapeHTML(asset.id)}">` +
    `<div class="image-tile-preview">${previewCell(s, docName, asset)}</div>` +
    `<div class="image-tile-name" title="${escapeHTML(baseName(asset.id))}">` +
    `${escapeHTML(asset.name || baseName(asset.id))}</div>` +
    `<dl class="image-tile-meta">` +
    `<div><dt>${escapeHTML(IMAGES.colFormat)}</dt>` +
    `<dd class="image-tile-value">${escapeHTML(formatLabel(asset))}` +
    `${dims ? " " + escapeHTML(dims) : ""}</dd></div>` +
    `<div><dt>${escapeHTML(IMAGES.colSize)}</dt>` +
    `<dd class="image-tile-value">${escapeHTML(IMAGES.fileSize(asset.bytes))}</dd></div>` +
    `<div><dt>${escapeHTML(IMAGES.colLocation)}</dt>` +
    `<dd class="image-tile-value" title="${escapeHTML(place.title)}">` +
    `${escapeHTML(place.text)}</dd></div>` +
    `</dl>` +
    `<div class="image-tile-foot">${statusChip(asset)}` +
    `<span class="image-tile-actions">${rowActions(asset)}</span></div>` +
    `</article>`;
}

// --- Cells ----------------------------------------------------------------

/**
 * previewCell(s, docName, asset) is the thumbnail, or the placeholder that asks
 * for it.
 *
 * The placeholder carries data-thumbfor, which is what the wiring pass reads to
 * decide which previews this paint should fetch: a row the reader cannot see
 * costs nothing until they scroll to it.
 *
 * An SVG preview arrives as an image/svg+xml data URL and is rendered through
 * an <img src> and NEVER inlined into the page as an <svg> element. An <img>
 * context executes no script and an inlined element does, and the SVG in
 * question came out of a client's document.
 */
function previewCell(s, docName, asset) {
  const key = imageThumbKey(docName, asset.id);
  const cached = (s.imageThumbs ?? {})[key];
  if (cached === undefined) {
    return `<span class="image-thumb image-thumb-pending" data-thumbfor="${escapeHTML(asset.id)}"` +
      ` aria-hidden="true"></span>`;
  }
  if (!isImageDataURL(cached)) {
    return `<span class="image-thumb image-thumb-empty">${escapeHTML(IMAGES.previewUnavailable)}</span>`;
  }
  return `<img class="image-thumb" src="${escapeHTML(cached)}" alt="" loading="lazy">`;
}

/**
 * isImageDataURL(value) admits only what Go's thumbnailer produces.
 *
 * The bytes behind the picture came out of a client's document, so the one thing
 * that must not happen is a value from that path reaching an src that is not an
 * image at all. Checking the prefix costs nothing and makes the rule explicit
 * where the tag is built.
 */
function isImageDataURL(value) {
  return typeof value === "string" && value.startsWith("data:image/");
}

/** statusChip(asset) is the ONE way a decision is shown: the banner, the Status
 *  column and the tile all read state.js imageStatus, so they cannot disagree
 *  about whether a picture changed. The chip names the TREATMENT, because
 *  "Blurred" says more than "Anonymised" once the decision is taken. */
function statusChip(asset) {
  const treatment = asset?.decision?.treatment || "keep";
  const label = IMAGES.statusLabel[treatment] ?? IMAGES.statusLabel.keep;
  return `<span class="image-status ${escapeHTML(imageStatus(asset))}">${escapeHTML(label)}</span>`;
}

/**
 * locationCell(asset) is where the picture appears, and where "one decision per
 * picture file" becomes visible.
 *
 * The cell shows the FIRST occurrence and how many more there are; the title
 * lists them all. That is the fixed-height rule again: a cell listing five
 * slides makes its row taller than its neighbours.
 */
function locationCell(asset) {
  const places = (asset.occurrences ?? []).map((o) => o.location).filter(Boolean);
  if (places.length === 0) {
    return { text: IMAGES.unknownLocation, title: IMAGES.unknownLocation };
  }
  // The cell is one line of a fixed-height row, so a location longer than the
  // column is clipped by CSS (text-overflow) rather than shortened here: a
  // hand-rolled ellipsis would be user-visible text written in a view, and the
  // title carries the full list anyway.
  const text = places.length > 1
    ? `${places[0]} ${IMAGES.moreLocations(places.length - 1)}`
    : places[0];
  const kinds = kindNote(asset);
  const title = [places.join(", "), IMAGES.appearsIn(places.length), kinds]
    .filter(Boolean).join(". ");
  return { text, title };
}

/** kindNote(asset) names what ENCLOSES the picture when it is not a plain
 *  picture element, because removing a background or a shape fill has no
 *  element to delete: it means overwriting the bytes. */
function kindNote(asset) {
  // Walked in IMAGE_KINDS order rather than in document order, so the same two
  // kinds always read the same way round: a note whose wording depends on which
  // slide came first is a note the reader cannot compare between rows.
  const present = new Set((asset.occurrences ?? []).map((o) => o.kind));
  return IMAGE_KINDS
    .filter((kind) => kind !== "picture" && present.has(kind))
    .map((kind) => IMAGES.kindLabel[kind])
    .join(", ");
}

/**
 * formatLabel(asset) names what the BYTES turned out to be.
 *
 * A format this application cannot redraw shows its own EXTENSION, which is more
 * use to the reader than the word "other". A format neither side knows is
 * treated as "other" rather than upper-cased on the spot: the vocabulary is
 * IMAGE_FORMATS, held to the engine's list by the parity guard, so an
 * unrecognised value is a bridge that has moved and not a new column heading.
 */
function formatLabel(asset) {
  const format = asset?.format ?? "";
  if (format !== "other" && IMAGE_FORMATS.includes(format)) return IMAGES.formatLabel[format];
  const base = baseName(asset?.id ?? "");
  const ext = base.includes(".") ? base.split(".").pop() : "";
  return ext ? ext.toUpperCase() : IMAGES.formatLabel.other;
}

/** displayTitle(asset) is the DRAWN size, in centimetres, for the Dimensions
 *  cell's tooltip: the pixels are what the file holds and the centimetres are
 *  what the reader sees on the slide. */
function displayTitle(asset) {
  const first = (asset.occurrences ?? [])[0];
  return IMAGES.displaySize(first?.displayCX, first?.displayCY);
}

/** baseName(id) is the archive part path's last segment. The id is the path, so
 *  it is what identifies the asset; the base name is what a reader recognises. */
function baseName(id) {
  const parts = String(id ?? "").split("/");
  return parts[parts.length - 1] ?? "";
}

// --- The treatment panel --------------------------------------------------

/**
 * treatmentPanelHTML(s) is the in-app overlay that takes the decision.
 *
 * It is an overlay through the same mechanism as the confirm (a backdrop, a
 * dialog, Escape and a click outside), never a native dialog: a native dialog in
 * a WebView is unstyled, unbranded, and on Windows it steals focus from the
 * window it belongs to. It is NOT modal.js's confirm, because this is an editing
 * surface rather than a question, and it carries its own draft.
 *
 * @param {object} [s] state
 * @returns {string} safe HTML ("" when the panel is closed)
 */
export function treatmentPanelHTML(s = getState()) {
  const editor = s.imageEditor;
  if (!editor) return "";
  const asset = imageAssets(s).find((a) => a.id === editor.assetId);
  if (!asset) return "";
  const draft = editor.draft ?? { treatment: "box" };

  const chips = IMAGE_TREATMENTS.filter((t) => t !== "keep").map((t) => {
    const reason = treatmentBlockedReason(asset, t);
    return {
      id: t,
      label: IMAGES.treatmentLabel[t],
      active: draft.treatment === t,
      disabled: reason !== "",
      title: reason ? IMAGES.blocked[reason] : "",
    };
  });

  return `<div class="image-panel-layer" role="presentation">` +
    `<div class="image-panel" role="dialog" aria-modal="true"` +
    ` aria-label="${escapeHTML(IMAGES.panelTitle)}">` +
    `<div class="image-panel-head">` +
    `<h2>${escapeHTML(IMAGES.panelTitle)}</h2>` +
    `<span class="image-panel-sub" title="${escapeHTML(baseName(asset.id))}">` +
    `${escapeHTML(asset.name || baseName(asset.id))}, ` +
    `${escapeHTML(IMAGES.appearsIn((asset.occurrences ?? []).length))}</span>` +
    `</div>` +
    `<div class="image-panel-body">` +
    previewBlock(editor) +
    `<div class="image-panel-controls">` +
    sectionLabel(IMAGES.panelTreatmentLabel, { mini: true }) +
    chipRow(chips, { attr: "imgtreatment", square: true, ariaLabel: IMAGES.panelTreatmentLabel }) +
    parameterBlock(draft) +
    (editor.error ? `<p class="hint bad" id="image-panel-error">${escapeHTML(editor.error)}</p>` : "") +
    `</div></div>` +
    `<div class="image-panel-foot">` +
    button(IMAGES.panelClose, { kind: "secondary", id: "image-cancel" }) +
    button(IMAGES.panelApply, { kind: "primary", id: "image-apply" }) +
    `</div></div></div>`;
}

/** previewBlock(editor) is what the export WILL write, rendered by Go with the
 *  real treatment. It is deliberately not a CSS filter: a CSS blur is not the
 *  blur the engine applies, and a preview showing something other than the
 *  output is worse than no preview. */
function previewBlock(editor) {
  const inner = editor.previewLoading || editor.preview === null
    ? `<span class="hint">${escapeHTML(IMAGES.panelPreviewLoading)}</span>`
    : (isImageDataURL(editor.preview)
      // Rendered through an <img src> and never inlined as an <svg> element:
      // an <img> context executes no script and an inlined element does.
      ? `<img class="image-preview" id="image-preview" src="${escapeHTML(editor.preview)}" alt="">`
      : `<span class="hint">${escapeHTML(IMAGES.previewUnavailable)}</span>`);
  return `<div class="image-panel-preview">` +
    sectionLabel(IMAGES.panelPreviewLabel, { mini: true }) +
    `<div class="image-preview-box">${inner}</div>` +
    `<p class="hint">${escapeHTML(IMAGES.panelPreviewHint)}</p>` +
    `</div>`;
}

/** parameterBlock(draft) is the chosen treatment's own control: the box text,
 *  the blur dial, or nothing at all for a removal. */
function parameterBlock(draft) {
  if (draft.treatment === "box") {
    const text = draft.boxText ?? "";
    return `<label class="image-field" for="image-box-text">` +
      `<span>${escapeHTML(IMAGES.boxTextLabel)}</span>` +
      `<input type="text" id="image-box-text" maxlength="${MAX_BOX_TEXT}"` +
      ` placeholder="${escapeHTML(IMAGES.boxTextPlaceholder)}"` +
      ` value="${escapeHTML(text)}"></label>` +
      `<p class="rail-readout" id="image-box-count">` +
      `${escapeHTML(IMAGES.boxTextCount([...text].length, MAX_BOX_TEXT))}</p>` +
      `<p class="hint">${escapeHTML(IMAGES.boxTextHint)}</p>`;
  }
  if (draft.treatment === "blur") {
    const strength = draft.blurStrength || DEFAULT_BLUR_STRENGTH;
    return `<label class="image-field" for="image-blur">` +
      `<span>${escapeHTML(IMAGES.blurLabel)}</span>` +
      `<input type="range" id="image-blur" min="${MIN_BLUR_STRENGTH}" max="${MAX_BLUR_STRENGTH}"` +
      ` step="1" value="${strength}"></label>` +
      `<p class="rail-readout" id="image-blur-value">${escapeHTML(String(strength))}</p>` +
      `<p class="hint">${escapeHTML(IMAGES.blurCaption)}</p>`;
  }
  return "";
}

// The three numbers imaging.Decision states, mirrored here because the panel's
// controls have to state them in markup (a maxlength, a slider's ends) before
// any call is made. Go REFUSES anything outside them, so the controls are the
// polite half of a rule the engine enforces.
const MAX_BOX_TEXT = 120;
const MIN_BLUR_STRENGTH = 1;
const MAX_BLUR_STRENGTH = 10;
const DEFAULT_BLUR_STRENGTH = 5;

// --- Wiring ---------------------------------------------------------------

/**
 * wireImageTab(container, s) attaches everything on the IMAGE half and asks for
 * the data the paint found missing.
 *
 * Safe to call after every paint: it detaches its own stale Escape handler and
 * every request it makes is guarded against being made twice.
 *
 * @param {HTMLElement} container the element the view just rendered into
 * @param {object} s state as of this paint
 */
export function wireImageTab(container, s) {
  const docName = imageDocName(s);
  ensureInventory(s, docName);

  container.querySelector("#image-doc")?.addEventListener("change", (ev) => {
    // The document selector is SHARED with the Compare card, so it writes the
    // same field: switching file here and switching tab there stay in step.
    setState({ resultDoc: ev.target.value });
  });

  for (const chip of container.querySelectorAll("[data-imgfilter]")) {
    chip.addEventListener("click", () => setImageFilter(chip.dataset.imgfilter));
  }
  for (const chip of container.querySelectorAll("[data-imglayout]")) {
    chip.addEventListener("click", () => setImageLayout(chip.dataset.imglayout));
  }

  for (const btn of container.querySelectorAll("[data-imgkeep]")) {
    btn.addEventListener("click", () => keepAsset(docName, btn.dataset.imgkeep));
  }
  for (const btn of container.querySelectorAll("[data-imgedit]")) {
    btn.addEventListener("click", () => openPanel(docName, btn.dataset.imgedit));
  }

  wirePanel(container, docName);
  wireNotice(container);
  loadVisibleThumbs(container, docName);
}

/** ensureInventory(s, docName) asks Go for the picture list once per document.
 *  The screen repaints on every keystroke elsewhere, so the guard is what keeps
 *  one scan from becoming one scan per paint. */
function ensureInventory(s, docName) {
  if (!docName) return;
  const record = imagesFor(s, docName);
  if (record) return;
  startImageScan(docName);
  listDocumentImages(docName)
    .then((inventory) => setImageInventory(docName, inventory))
    // The bridge's own message names what failed and how to fix it, so it is
    // shown as it is rather than replaced by a sentence that knows less.
    .catch((err) => setImageScanError(docName, err?.message ?? String(err)));
}

/**
 * loadVisibleThumbs(container, docName) fetches the previews for the rows the
 * reader can actually see, up to one batch.
 *
 * Visibility is measured when the browser can measure it and ignored when it
 * cannot (the render tests have no layout), where document order and the batch
 * cap give the same guarantee: a paint never fires more than THUMB_BATCH calls.
 */
function loadVisibleThumbs(container, docName) {
  const list = container.querySelector("#image-list");
  if (!list) return;
  // Re-checking on scroll is what makes the loading lazy rather than merely
  // capped: the rows that come into view are the next batch.
  list.addEventListener("scroll", () => loadVisibleThumbs(container, docName));

  const box = rectOf(list);
  const wanted = [];
  for (const node of list.querySelectorAll("[data-thumbfor]")) {
    const r = box ? rectOf(node) : null;
    if (r && (r.bottom < box.top - THUMB_MARGIN || r.top > box.bottom + THUMB_MARGIN)) continue;
    wanted.push(node.dataset.thumbfor);
    if (wanted.length >= THUMB_BATCH) break;
  }

  for (const assetId of wanted) {
    const key = imageThumbKey(docName, assetId);
    if (thumbsInFlight.has(key)) continue;
    thumbsInFlight.add(key);
    imageThumbnail(docName, assetId, THUMB_MAX_PX)
      .then((thumb) => cacheImageThumb(docName, assetId, thumb?.dataUrl ?? ""))
      // A refused preview caches the empty string, so the cell reads "No
      // preview" and the next paint does not ask again. Retrying forever is
      // what a page with no bridge would otherwise do.
      .catch(() => cacheImageThumb(docName, assetId, ""))
      // The in-flight set guards against asking TWICE AT ONCE, and nothing more:
      // what stops a second request is the cache, which every outcome writes.
      // Left in the set for good, a key would outlive its document, and a file
      // re-imported under the same name would never load a preview again.
      .finally(() => thumbsInFlight.delete(key));
  }
}

/** rectOf(node) is getBoundingClientRect where there is a renderer, and null
 *  where there is not, so the caller can fall back instead of throwing. */
function rectOf(node) {
  return typeof node?.getBoundingClientRect === "function" ? node.getBoundingClientRect() : null;
}

/** keepAsset(docName, assetId) is the one-click Keep. A keep is stored as the
 *  ABSENCE of a decision, so this clears whatever was recorded. */
async function keepAsset(docName, assetId) {
  const decision = { treatment: "keep" };
  try {
    await setImageDecision(docName, assetId, decision);
    applyImageDecision(docName, assetId, decision);
    notify(IMAGES.kept, "ok");
  } catch (err) {
    notify(err?.message ?? String(err), "warn");
  }
}

/** openPanel(docName, assetId) opens the treatment panel on a draft that starts
 *  from the stored decision, or from the first treatment the picture can carry
 *  when it is still kept: a panel opening on a choice the asset refuses would
 *  make its Apply button dead on arrival. */
function openPanel(docName, assetId) {
  const asset = imageAssets(getState()).find((a) => a.id === assetId);
  if (!asset) return;
  const stored = asset.decision ?? {};
  const treatment = stored.treatment && stored.treatment !== "keep"
    ? stored.treatment
    : (IMAGE_TREATMENTS.filter((t) => t !== "keep").find((t) => treatmentAvailable(asset, t)) ?? "remove");
  openImageEditor(docName, assetId, {
    treatment,
    boxText: stored.boxText ?? "",
    blurStrength: stored.blurStrength || DEFAULT_BLUR_STRENGTH,
  });
  requestPreview(0);
}

/** wirePanel(container, docName) attaches the treatment panel: the chips, the
 *  parameter control, Apply, Cancel, the backdrop and Escape. */
function wirePanel(container, docName) {
  if (escapeListener) {
    document.removeEventListener("keydown", escapeListener);
    escapeListener = null;
  }
  const layer = container.querySelector(".image-panel-layer");
  if (!layer) return;

  for (const chip of container.querySelectorAll("[data-imgtreatment]")) {
    chip.addEventListener("click", () => {
      updateImageEditor({
        draft: { ...getState().imageEditor?.draft, treatment: chip.dataset.imgtreatment },
        error: "",
      });
      requestPreview(0);
    });
  }

  container.querySelector("#image-box-text")?.addEventListener("input", (ev) => {
    updateImageEditor({
      draft: { ...getState().imageEditor?.draft, boxText: ev.target.value ?? "" },
      error: "",
    });
    requestPreview();
  });

  container.querySelector("#image-blur")?.addEventListener("input", (ev) => {
    updateImageEditor({
      draft: {
        ...getState().imageEditor?.draft,
        blurStrength: Number(ev.target.value) || DEFAULT_BLUR_STRENGTH,
      },
      error: "",
    });
    requestPreview();
  });

  container.querySelector("#image-cancel")?.addEventListener("click", () => cancelPanel());
  container.querySelector("#image-apply")?.addEventListener("click", () => applyPanel(docName));

  // A click on the backdrop dismisses; a click INSIDE the panel must not, so
  // the target has to be the layer itself rather than any descendant.
  layer.addEventListener("click", (ev) => {
    if (ev.target === layer) cancelPanel();
  });

  escapeListener = (ev) => {
    if (ev.key !== "Escape") return;
    ev.preventDefault();
    cancelPanel();
  };
  document.addEventListener("keydown", escapeListener);
}

/** cancelPanel() drops the draft. The stored decision is untouched, which is
 *  the whole reason the draft is separate from it. */
function cancelPanel() {
  if (previewTimer) {
    clearTimeout(previewTimer);
    previewTimer = null;
  }
  closeImageEditor();
}

/** applyPanel(docName) records the draft. A refusal from Go lands ON the panel,
 *  next to the field the fix goes into, the way every other per-surface error on
 *  this screen already does. */
async function applyPanel(docName) {
  const editor = getState().imageEditor;
  if (!editor) return;
  const decision = wireDecision(editor.draft);
  try {
    await setImageDecision(docName, editor.assetId, decision);
  } catch (err) {
    updateImageEditor({ error: err?.message ?? String(err) });
    return;
  }
  applyImageDecision(docName, editor.assetId, decision);
  closeImageEditor();
  notify(IMAGES.decisionApplied(decision.treatment), "ok");
}

/**
 * wireDecision(draft) is the draft as the bridge wants it: only the parameter
 * the chosen treatment uses travels.
 *
 * Sending a box text with a blur, or a strength with a removal, would put a
 * value in the session file that no treatment reads, and a stored field nothing
 * consumes is the next thing somebody trusts.
 */
function wireDecision(draft) {
  const treatment = draft?.treatment ?? "keep";
  if (treatment === "box") {
    return { treatment, boxText: draft.boxText ?? "" };
  }
  if (treatment === "blur") {
    return { treatment, blurStrength: draft.blurStrength || DEFAULT_BLUR_STRENGTH };
  }
  return { treatment };
}

/**
 * requestPreview(delay) renders the draft through Go, debounced.
 *
 * Every keystroke and every slider step would otherwise be one real treatment
 * plus one thumbnail, and holding a slider would queue ten. A treatment CHOICE
 * asks immediately (delay 0), because a chip press is one deliberate act rather
 * than a stream of them.
 *
 * @param {number} [delay] milliseconds to wait, defaulting to the debounce
 */
function requestPreview(delay = PREVIEW_DEBOUNCE_MS) {
  if (previewTimer) clearTimeout(previewTimer);
  previewTimer = setTimeout(() => {
    previewTimer = null;
    const editor = getState().imageEditor;
    if (!editor) return;
    const decision = wireDecision(editor.draft);
    updateImageEditor({ previewLoading: true });
    previewImageTreatment(editor.docName, editor.assetId, decision, PREVIEW_MAX_PX)
      .then((preview) => {
        // The panel may have been cancelled while this was in flight, and
        // updateImageEditor is a no-op then: a preview must not reopen a panel
        // the user has dismissed.
        updateImageEditor({ preview: preview?.dataUrl ?? "", previewLoading: false });
      })
      .catch((err) => updateImageEditor({
        preview: "", previewLoading: false, error: err?.message ?? String(err),
      }));
  }, delay);
}
