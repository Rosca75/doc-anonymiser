// anonymiseimages.test.js, render tests for the IMAGE half of step 3.
//
// These assert what the surface SHOWS, through testhtml.js, rather than that the
// output contains a substring somewhere: four reported bugs about what a pane
// displayed lived happily beside green substring tests, which is why
// docs/UITESTING.md layer 2 is written this way.
//
// What the picture review has to get right, and what each case here holds it to:
//
//   the seven columns    a header and its rows built from one shared template,
//                        so they cannot drift apart.
//   one decision per     a picture used in three places is ONE row that says so,
//   picture file         because answering per place would need the exporter to
//                        clone picture parts.
//   an unknown fact is   a picture whose header could not be read has no
//   blank, never zero    dimensions, and "0 x 0" would state something untrue.
//   not applicable is    a .txt or a .pdf gets the sentence that answers the
//   an ANSWER            question, not an empty tab.
//   a control that       a treatment imaging.Decision.Validate would refuse is
//   cannot anonymise     disabled AND says why, because the alternative is a
//   is never offered     refusal that arrives at export time.

import { test } from "node:test";
import assert from "node:assert/strict";

import { resetState, setState, getState, openImageEditor } from "./state.js";
import { imageTabHTML, imageCount } from "./views/anonymiseimages.js";
import { all, one, textOf, attr, exists } from "./testhtml.js";
import { IMAGES } from "./copy.js";

const DOC = "deck.pptx";

/** occurrence(location) is one place a picture is used, as the scan reports it. */
function occurrence(location, kind = "picture") {
  return { part: "ppt/slides/slide1.xml", ordinal: 0, kind, location, displayCX: 0, displayCY: 0 };
}

/** asset(patch) is one inventory asset with the fields every row reads. */
function asset(patch) {
  return {
    id: "ppt/media/image1.png",
    name: "Alpine Trust logo",
    format: "png",
    bytes: 26144,
    width: 120,
    height: 80,
    companion: "",
    linked: false,
    occurrences: [occurrence("Slide 1")],
    decision: { treatment: "keep" },
    ...patch,
  };
}

// The eight pictures every case below draws from. They cover each status, each
// format the disable rules turn on, and one picture whose header could not be
// read.
const SHARED = asset({
  id: "ppt/media/image1.png", name: "Alpine Trust logo",
  width: 1200, height: 800, bytes: 348 * 1024,
  occurrences: [occurrence("Slide 1"), occurrence("Slide 4"), occurrence("Slide master")],
});
const BOXED = asset({
  id: "ppt/media/image2.jpeg", name: "Team photo", format: "jpeg",
  decision: { treatment: "box", boxText: "Client logo" },
});
const BLURRED = asset({
  id: "ppt/media/image3.png", name: "System screenshot",
  decision: { treatment: "blur", blurStrength: 7 },
});
const REMOVED = asset({
  id: "ppt/media/image4.png", name: "Org chart",
  decision: { treatment: "remove" },
});
const VECTOR = asset({
  id: "ppt/media/image5.png", name: "Vector mark", format: "svg",
  companion: "ppt/media/image5.svg",
});
const EMF = asset({
  id: "ppt/media/image6.emf", name: "Legacy drawing", format: "other",
  width: 0, height: 0,
});
const LINKED = asset({
  id: "ppt/media/image7.png", name: "Linked photo", format: "other", linked: true,
  width: 0, height: 0,
});
const UNREADABLE = asset({
  id: "ppt/media/image8.png", name: "Damaged picture",
  width: 0, height: 0,
});

const ALL_ASSETS = [SHARED, BOXED, BLURRED, REMOVED, VECTOR, EMF, LINKED, UNREADABLE];

/**
 * seed(assets, patch) puts one imported document with a scanned inventory in the
 * store and returns the state the builders read.
 */
function seed(assets = ALL_ASSETS, patch = {}) {
  resetState();
  setState({
    documents: [{ name: DOC, format: "pptx", markdown: "", previewTruncated: false, isGrid: false }],
    resultDoc: DOC,
    anonymiseTab: "images",
    images: {
      [DOC]: {
        loading: false, error: null,
        inventory: { applicable: true, assets, warnings: [] },
      },
    },
    ...patch,
  });
  return getState();
}

/** seedNotApplicable(reason) is the answer for a format with no image review. */
function seedNotApplicable(reason) {
  resetState();
  setState({
    documents: [{ name: "notes.txt", format: "txt", markdown: "", previewTruncated: false, isGrid: false }],
    resultDoc: "notes.txt",
    anonymiseTab: "images",
    images: {
      "notes.txt": {
        loading: false, error: null,
        inventory: { applicable: false, reason, assets: [], warnings: [] },
      },
    },
  });
  return getState();
}

// --- The details view ------------------------------------------------------

test("the details view shows the seven column headings in order", () => {
  const html = imageTabHTML(seed());
  const heads = all(html, ".col-head").map((h) => h.inner);
  assert.deepEqual(heads, [
    IMAGES.colPreview, IMAGES.colName, IMAGES.colFormat, IMAGES.colDimensions,
    IMAGES.colSize, IMAGES.colLocation, IMAGES.colStatus,
  ], "the header is built from the shared column template, in the documented order");
});

test("a picture used in three places is ONE row that says so", () => {
  const html = imageTabHTML(seed([SHARED]));
  const cell = one(html, ".cell-location");
  assert.equal(textOf(html, ".cell-location"), `Slide 1 ${IMAGES.moreLocations(2)}`,
    "the cell names the first place and how many more there are");
  const title = cell.attrs.title;
  for (const place of ["Slide 1", "Slide 4", "Slide master"]) {
    assert.ok(title.includes(place),
      `the title lists every place, so the reader can see what one decision covers; ` +
      `it is ${JSON.stringify(title)} and does not mention ${place}`);
  }
  assert.equal(all(html, ".grid-row").length, 1,
    "one picture file is one row and one question, whatever number of places it appears in");
});

test("the Status column reads the treatment, not merely kept or anonymised", () => {
  const html = imageTabHTML(seed([SHARED, BOXED, BLURRED, REMOVED]));
  const statuses = all(html, ".image-status").map((s) => s.inner);
  assert.deepEqual(statuses, [
    IMAGES.statusLabel.keep, IMAGES.statusLabel.box,
    IMAGES.statusLabel.blur, IMAGES.statusLabel.remove,
  ], "Kept, Boxed, Blurred and Removed each read as themselves");
});

test("an unreadable dimension renders blank, never 0 x 0", () => {
  const html = imageTabHTML(seed([UNREADABLE]));
  assert.equal(textOf(html, ".cell-dimensions"), "",
    "a picture whose header could not be read has no size, and 0 x 0 would state one");
  assert.equal(textOf(html, ".cell-dimensions").includes("0"), false,
    "nothing about a zero reaches the cell");
});

test("a readable dimension renders as pixels, with the drawn size in the title", () => {
  const drawn = asset({
    occurrences: [{ ...occurrence("Slide 2"), displayCX: 1828800, displayCY: 1219200 }],
    width: 1200, height: 800,
  });
  const html = imageTabHTML(seed([drawn]));
  assert.equal(textOf(html, ".cell-dimensions"), "1200 x 800");
  assert.equal(attr(html, ".cell-dimensions", "title"), IMAGES.displaySize(1828800, 1219200),
    "the pixels are what the file holds; the centimetres are what the reader sees on the slide");
});

test("the Format column names what the bytes turned out to be", () => {
  const html = imageTabHTML(seed([SHARED, BOXED, VECTOR, EMF]));
  const formats = all(html, ".cell-format").map((c) => c.inner);
  assert.deepEqual(formats, ["PNG", "JPG", "SVG", "EMF"],
    "a format the application cannot redraw shows its own extension, which is more use " +
    "to the reader than the word Other");
});

test("the file size is one decimal above a megabyte and none below", () => {
  const html = imageTabHTML(seed([
    asset({ id: "a/1.png", bytes: 348 * 1024 }),
    asset({ id: "a/2.png", bytes: Math.round(1.2 * 1024 * 1024) }),
  ]));
  assert.deepEqual(all(html, ".cell-size").map((c) => c.inner), ["348 KB", "1.2 MB"]);
});

// --- The banner ------------------------------------------------------------

test("the filter chips carry the live counts", () => {
  const html = imageTabHTML(seed());
  const chips = all(html, "[data-imgfilter]").map((c) => c.inner);
  // Three of the eight carry a treatment: boxed, blurred, removed.
  assert.deepEqual(chips, [
    IMAGES.filterChip("all", 8),
    IMAGES.filterChip("kept", 5),
    IMAGES.filterChip("anonymised", 3),
  ], "the banner says how many rows each choice would show before it is pressed");
});

test("Anonymised selects exactly the pictures that are not kept", () => {
  const html = imageTabHTML(seed(ALL_ASSETS, { imageFilter: "anonymised" }));
  const names = all(html, ".cell-name").map((c) => c.inner);
  assert.deepEqual(names, ["Team photo", "System screenshot", "Org chart"],
    "the filter and the Status column read the same mapping, so they cannot disagree");
});

test("Kept selects exactly the pictures no decision has changed", () => {
  const html = imageTabHTML(seed(ALL_ASSETS, { imageFilter: "kept" }));
  assert.equal(all(html, ".grid-row").length, 5);
  for (const status of all(html, ".image-status")) {
    assert.equal(status.inner, IMAGES.statusLabel.keep);
  }
});

test("a filter that matches nothing says so instead of showing an empty grid", () => {
  const html = imageTabHTML(seed([SHARED], { imageFilter: "anonymised" }));
  assert.equal(textOf(html, ".image-message"), IMAGES.noneMatchFilter);
  assert.equal(exists(html, ".grid-row"), false);
});

test("the view toggle offers Details and Tiles, with the active one pressed", () => {
  const html = imageTabHTML(seed(ALL_ASSETS, { imageLayout: "tiles" }));
  const chips = all(html, "[data-imglayout]");
  assert.deepEqual(chips.map((c) => c.inner), [IMAGES.layoutDetails, IMAGES.layoutTiles]);
  assert.equal(chips[1].attrs["aria-pressed"], "true");
  assert.equal(exists(html, ".image-tile"), true, "the tiles view renders tiles");
  assert.equal(exists(html, ".grid-row"), false, "and not the details grid as well");
});

// --- The tiles view --------------------------------------------------------

test("a tile shows the same facts as a row and the same two answers", () => {
  const html = imageTabHTML(seed([SHARED], { imageLayout: "tiles" }));
  assert.equal(textOf(html, ".image-tile-name"), "Alpine Trust logo");
  const meta = all(html, ".image-tile-value").map((d) => d.inner);
  assert.deepEqual(meta, ["PNG 1200 x 800", "348 KB", `Slide 1 ${IMAGES.moreLocations(2)}`]);
  assert.equal(all(html, "[data-imgkeep]").length, 1, "a tile carries the Keep button");
  assert.equal(all(html, "[data-imgedit]").length, 1, "and the Anonymise button beside it");
});

test("a tile's location cell keeps its overflow in the title, not on a second line", () => {
  const html = imageTabHTML(seed([SHARED], { imageLayout: "tiles" }));
  const dd = all(html, ".image-tile-value")[2];
  assert.ok(dd.attrs.title.includes("Slide master"),
    "the full list is in the title: a card that grew a line for the third place would " +
    "move every card below it and cost the reader their scroll position");
});

// --- Not applicable is an answer -------------------------------------------

test("a PDF says its pictures are already gone, and lists nothing", () => {
  const html = imageTabHTML(seedNotApplicable("pdf_images_removed"));
  assert.equal(textOf(html, ".image-message"), IMAGES.reason.pdf_images_removed);
  assert.equal(exists(html, ".grid-row"), false);
  assert.equal(exists(html, "[data-imgfilter]"), false,
    "a filter over an answer with no list is a control that cannot do anything");
});

test("the document selector survives a document with no image review", () => {
  const html = imageTabHTML(seedNotApplicable("pdf_images_removed"));
  assert.ok(exists(html, "#image-doc"),
    "a selector that vanished on a .txt file would trap the user on the one document " +
    "that has nothing to review");
});

test("an unsupported format says where image review is available", () => {
  const html = imageTabHTML(seedNotApplicable("format_not_supported"));
  assert.equal(textOf(html, ".image-message"), IMAGES.reason.format_not_supported);
  assert.equal(exists(html, ".grid-row"), false);
});

test("the IMAGE tab carries no count for a document with no image review", () => {
  assert.equal(imageCount(seedNotApplicable("pdf_images_removed")), null,
    "a badge reading 0 would claim the document was reviewed and had no pictures");
  assert.equal(imageCount(seed()), 8);
});

test("a document with no pictures at all says so", () => {
  const html = imageTabHTML(seed([]));
  assert.equal(textOf(html, ".image-message"), IMAGES.empty);
});

test("a refused scan shows Go's own reason, which names the fix", () => {
  const s = seed([], {
    images: {
      [DOC]: { loading: false, error: "the document is not imported; import it again", inventory: null },
    },
  });
  const html = imageTabHTML(s);
  assert.ok(textOf(html, ".banner").includes("import it again"));
});

test("the per-document warnings print their sentence, and an unknown code prints nothing", () => {
  const s = seed(ALL_ASSETS, {
    images: {
      [DOC]: {
        loading: false, error: null,
        inventory: {
          applicable: true, assets: ALL_ASSETS,
          warnings: ["unreadable_part", "invented_code"],
        },
      },
    },
  });
  const html = imageTabHTML(s);
  const warnings = all(html, ".image-warning").map((w) => w.inner);
  assert.deepEqual(warnings, [IMAGES.warning.unreadable_part],
    "a reader has no use for a JSON key, so a code with no sentence prints nothing");
});

// --- The preview cell ------------------------------------------------------

test("a row with no cached preview renders a placeholder that asks for one", () => {
  const html = imageTabHTML(seed([SHARED]));
  assert.equal(attr(html, "[data-thumbfor]", "data-thumbfor"), SHARED.id,
    "the placeholder names the asset the wiring pass should fetch");
});

test("a cached preview renders as an img, and a failed one says so", () => {
  const s = seed([SHARED, BOXED], {
    imageThumbs: {
      [`${DOC} ${SHARED.id}`]: "data:image/png;base64,AAAA",
      [`${DOC} ${BOXED.id}`]: "",
    },
  });
  const html = imageTabHTML(s);
  assert.equal(attr(html, "img.image-thumb", "src"), "data:image/png;base64,AAAA");
  assert.equal(textOf(html, ".image-thumb-empty"), IMAGES.previewUnavailable);
});

test("only a data:image URL reaches an img src", () => {
  const s = seed([SHARED], {
    imageThumbs: { [`${DOC} ${SHARED.id}`]: "data:text/html;base64,PHNjcmlwdD4=" },
  });
  const html = imageTabHTML(s);
  assert.equal(exists(html, "img.image-thumb"), false,
    "the bytes came out of a client document, so anything that is not an image is not " +
    "given an src at all");
  assert.equal(textOf(html, ".image-thumb-empty"), IMAGES.previewUnavailable);
});

// --- The treatment panel ---------------------------------------------------

/**
 * panel(target, draft) opens the treatment panel on one asset and returns the
 * screen's markup with it in.
 *
 * It renders the whole IMAGE half rather than the panel alone, because that is
 * how the panel reaches the page: it is part of imageTabHTML's output, and a test
 * that called a builder the screen does not use would be testing a second path.
 */
function panel(target, draft = { treatment: "box", boxText: "", blurStrength: 5 }) {
  seed(ALL_ASSETS);
  openImageEditor(DOC, target.id, draft);
  return imageTabHTML(getState());
}

test("the treatment panel offers the three anonymising treatments and no keep", () => {
  const html = panel(SHARED);
  const chips = all(html, "[data-imgtreatment]");
  assert.deepEqual(chips.map((c) => c.attrs["data-imgtreatment"]), ["box", "blur", "remove"],
    "Keep is not a treatment in the panel: it is the row's other button");
  for (const chip of chips) {
    assert.equal("disabled" in chip.attrs, false,
      "a PNG can carry all three, so nothing is disabled for it");
  }
});

test("an SVG cannot be blurred, and the disabled chip says why", () => {
  const html = panel(VECTOR);

  const chips = Object.fromEntries(
    all(html, "[data-imgtreatment]").map((c) => [c.attrs["data-imgtreatment"], c]));
  assert.equal("disabled" in chips.blur.attrs, true);
  assert.equal(chips.blur.attrs.title, IMAGES.blocked.svg_blur,
    "a control that says no has to say why: a blur filter leaves the original shapes " +
    "and text inside the file");
  assert.equal("disabled" in chips.box.attrs, false, "a box is a redraw an SVG can carry");
  assert.equal("disabled" in chips.remove.attrs, false);
});

test("a format the application cannot redraw offers removal only", () => {
  const html = panel(EMF);
  const chips = Object.fromEntries(
    all(html, "[data-imgtreatment]").map((c) => [c.attrs["data-imgtreatment"], c]));
  assert.equal("disabled" in chips.box.attrs, true);
  assert.equal("disabled" in chips.blur.attrs, true);
  assert.equal(chips.box.attrs.title, IMAGES.blocked.format);
  assert.equal("disabled" in chips.remove.attrs, false);
});

test("a linked picture offers removal only, and says the bytes are elsewhere", () => {
  const html = panel(LINKED);
  const chips = Object.fromEntries(
    all(html, "[data-imgtreatment]").map((c) => [c.attrs["data-imgtreatment"], c]));
  assert.equal("disabled" in chips.box.attrs, true);
  assert.equal("disabled" in chips.blur.attrs, true);
  assert.equal(chips.box.attrs.title, IMAGES.blocked.linked,
    "the linked reason comes before the format reason, because it is the true one: " +
    "a converter cannot help with a picture that is not in the file");
  assert.equal("disabled" in chips.remove.attrs, false);
});

test("the box treatment shows a text field with a live counter and the font warning", () => {
  const html = panel(SHARED, { treatment: "box", boxText: "Client logo", blurStrength: 5 });
  assert.equal(attr(html, "#image-box-text", "value"), "Client logo");
  assert.equal(attr(html, "#image-box-text", "maxlength"), "120");
  assert.equal(textOf(html, "#image-box-count"), IMAGES.boxTextCount(11, 120));
  assert.ok(html.includes(IMAGES.boxTextHint.slice(0, 20)),
    "the field says the text is drawn with a built-in font and accents are simplified");
});

test("the blur treatment shows the dial and the honest caption", () => {
  const html = panel(SHARED, { treatment: "blur", boxText: "", blurStrength: 7 });
  assert.equal(attr(html, "#image-blur", "value"), "7");
  assert.equal(attr(html, "#image-blur", "min"), "1");
  assert.equal(attr(html, "#image-blur", "max"), "10");
  assert.equal(textOf(html, "#image-blur-value"), "7");
  assert.ok(html.includes("It is not a guarantee"),
    "the caption says what a blur does not promise, because a control that does not " +
    "anonymise must never be labelled as though it does");
});

test("a removal offers no parameter at all", () => {
  const html = panel(SHARED, { treatment: "remove", boxText: "", blurStrength: 5 });
  assert.equal(exists(html, "#image-box-text"), false);
  assert.equal(exists(html, "#image-blur"), false);
});

test("the panel says how far one decision reaches", () => {
  const html = panel(SHARED);
  assert.ok(textOf(html, ".image-panel-sub").includes(IMAGES.appearsIn(3)),
    "a decision is attached to the picture file and applies to every place it appears");
});

test("the panel's preview waits for Go rather than filtering in CSS", () => {
  const html = panel(SHARED);
  assert.equal(textOf(html, ".image-preview-box"), IMAGES.panelPreviewLoading,
    "the preview is what the export will write, rendered by the real treatment: a CSS " +
    "blur is not the blur the engine applies");
});

test("a rendered preview is an img with the data URL Go answered", () => {
  seed(ALL_ASSETS);
  openImageEditor(DOC, SHARED.id, { treatment: "box", boxText: "", blurStrength: 5 });
  setState({
    imageEditor: {
      ...getState().imageEditor,
      preview: "data:image/png;base64,BBBB", previewLoading: false,
    },
  });
  const html = imageTabHTML(getState());
  assert.equal(attr(html, "#image-preview", "src"), "data:image/png;base64,BBBB");
});

test("a refusal from Go lands on the panel, beside the field that caused it", () => {
  seed(ALL_ASSETS);
  openImageEditor(DOC, SHARED.id, { treatment: "box", boxText: "", blurStrength: 5 });
  setState({
    imageEditor: { ...getState().imageEditor, error: "the box text is 130 characters, the maximum is 120" },
  });
  const html = imageTabHTML(getState());
  assert.ok(textOf(html, "#image-panel-error").includes("the maximum is 120"));
});

test("a closed panel renders nothing at all", () => {
  const html = imageTabHTML(seed());
  assert.equal(exists(html, ".image-panel-layer"), false,
    "the overlay exists only while a decision is being taken");
});
