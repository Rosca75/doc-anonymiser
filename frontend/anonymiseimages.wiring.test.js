// anonymiseimages.wiring.test.js, wiring tests for the IMAGE half of step 3.
//
// A render test reads the HTML string a view wrote. A browser RE-READS it, and
// its parser lower-cases attribute names, so a camel-case data attribute renders,
// matches every string assertion, and is unreachable as dataset.x in every
// handler: seven controls on one card were reported dead while the suite stayed
// green. testdom.js is the minimal DOM whose parser behaves the same way, so a
// handler wired against it fails for the reason it fails in the application.
//
// Everything here drives the REAL render path (renderAnonymise, which owns the
// tab bar and dispatches to the IMAGE half), against a stubbed Wails bridge, so
// what is under test is the wiring rather than a builder called by hand.
//
// The four properties these cases exist for:
//
//   the right asset      Keep and Anonymise, on a row and on a tile, act on the
//                        picture they sit beside and no other.
//   the tab bar works    switching to TEXT renders the screen step 3 has always
//                        rendered, so the wrapper cannot silently break it.
//   the panel debounces  a keystroke or a slider step is not a render of the
//                        real treatment; holding a slider must not queue ten.
//   Cancel records       the panel keeps a DRAFT, so dismissing it must leave
//   nothing              the stored decision exactly as it was.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  resetState, setState, getState, subscribe, imagesFor,
} from "./state.js";
import { renderAnonymise } from "./views/anonymise.js";
import { container, fire } from "./testdom.js";
import { IMAGES } from "./copy.js";

const DOC = "deck.pptx";

/** asset(patch) is one inventory asset with the fields the rows read. */
function asset(patch) {
  return {
    id: "ppt/media/image1.png", name: "Alpine Trust logo", format: "png",
    bytes: 26144, width: 120, height: 80, companion: "", linked: false,
    occurrences: [{ part: "ppt/slides/slide1.xml", ordinal: 0, kind: "picture", location: "Slide 1", displayCX: 0, displayCY: 0 }],
    decision: { treatment: "keep" },
    ...patch,
  };
}

const LOGO = asset({ id: "ppt/media/image1.png", name: "Alpine Trust logo" });
const PHOTO = asset({ id: "ppt/media/image2.png", name: "Team photo" });

/**
 * bridge(app) installs a stubbed Wails bridge, the same namespace api.js reads,
 * and returns a restore function. The panel and the previews are the only things
 * on this surface that talk to Go, so a test names the methods it cares about
 * and everything else still rejects, which every caller here already handles.
 */
function bridge(app) {
  const previousWindow = globalThis.window;
  const previousDocument = globalThis.document;
  globalThis.window = { go: { backend: { App: app } } };
  // The treatment panel is dismissible with Escape, so it attaches a
  // document-level handler exactly as modal.js does. testdom models an element
  // tree and not a document, so the two calls the panel makes are stubbed.
  globalThis.document = {
    addEventListener() {},
    removeEventListener() {},
  };
  return () => {
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
    if (previousDocument === undefined) delete globalThis.document;
    else globalThis.document = previousDocument;
  };
}

/** calls() is a recording stub: the function plus the arguments it was given. */
function calls(answer) {
  const seen = [];
  const fn = async (...args) => {
    seen.push(args);
    if (typeof answer === "function") return answer(...args);
    return answer;
  };
  fn.seen = seen;
  return fn;
}

/**
 * screen(assets, patch) seeds one imported document with a scanned inventory,
 * renders step 3's IMAGE half and keeps it repainting on every state change,
 * which is what main.js does.
 *
 * The caller must call stop(): the subscription outlives the test otherwise and
 * the next one repaints through a stale root.
 */
function screen(assets = [LOGO, PHOTO], patch = {}, docName = DOC) {
  resetState();
  setState({
    documents: [{ name: docName, format: "pptx", markdown: "", previewTruncated: false, isGrid: false }],
    resultDoc: docName,
    anonymiseTab: "images",
    images: {
      [docName]: {
        loading: false, error: null,
        inventory: { applicable: true, assets, warnings: [] },
      },
    },
    ...patch,
  });
  const root = container();
  const paint = () => renderAnonymise(root);
  const stop = subscribe(paint);
  paint();
  return { root, stop };
}

/** settle(ms) waits out the panel's real debounce. */
function settle(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// --- The two direct answers ------------------------------------------------

test("Keep on a row records a keep for THAT picture", async () => {
  const SetImageDecision = calls(undefined);
  const restore = bridge({ SetImageDecision, ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen();
  try {
    const buttons = root.querySelectorAll("[data-imgkeep]");
    assert.equal(buttons.length, 2, "one Keep per row");
    // The SECOND row, because a handler that resolves against the wrong asset
    // usually resolves against the first one and looks right.
    await fire(buttons[1], "click");
    assert.deepEqual(SetImageDecision.seen, [[DOC, PHOTO.id, { treatment: "keep" }]],
      "a keep is stored as the ABSENCE of a decision, so it is sent as a keep and clears " +
      "whatever was recorded");
  } finally {
    stop(); restore();
  }
});

test("Keep on a row that is already kept is disabled rather than a second call", async () => {
  const SetImageDecision = calls(undefined);
  const restore = bridge({ SetImageDecision, ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen([LOGO]);
  try {
    const keep = root.querySelector("[data-imgkeep]");
    assert.equal(keep.disabled, true, "there is nothing for Keep to do on a kept picture");
  } finally {
    stop(); restore();
  }
});

test("Anonymise on a row opens the treatment panel on THAT picture", async () => {
  const restore = bridge({
    ImageThumbnail: async () => ({ dataUrl: "" }),
    PreviewImageTreatment: async () => ({ dataUrl: "data:image/png;base64,AA" }),
  });
  const { root, stop } = screen();
  try {
    const buttons = root.querySelectorAll("[data-imgedit]");
    await fire(buttons[1], "click");
    assert.equal(getState().imageEditor?.assetId, PHOTO.id);
    assert.equal(getState().imageEditor?.docName, DOC);
    assert.ok(root.querySelector(".image-panel-layer"), "the panel is on screen");
  } finally {
    stop(); restore();
  }
});

test("Keep and Anonymise work the same way from a tile", async () => {
  const SetImageDecision = calls(undefined);
  const restore = bridge({ SetImageDecision, ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen([LOGO, PHOTO], { imageLayout: "tiles" });
  try {
    assert.equal(root.querySelectorAll(".image-tile").length, 2, "the tiles view is on screen");
    await fire(root.querySelectorAll("[data-imgkeep]")[1], "click");
    assert.deepEqual(SetImageDecision.seen, [[DOC, PHOTO.id, { treatment: "keep" }]]);
    await fire(root.querySelectorAll("[data-imgedit]")[0], "click");
    assert.equal(getState().imageEditor?.assetId, LOGO.id,
      "a tile's buttons carry the same asset id its metadata describes");
  } finally {
    stop(); restore();
  }
});

test("a refused decision reaches the notice strip rather than being swallowed", async () => {
  const restore = bridge({
    SetImageDecision: async () => { throw new Error("the document is not imported"); },
    ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const { root, stop } = screen([LOGO, PHOTO], {
    images: {
      [DOC]: {
        loading: false, error: null,
        inventory: { applicable: true, warnings: [], assets: [asset({ decision: { treatment: "remove" } })] },
      },
    },
  });
  try {
    await fire(root.querySelector("[data-imgkeep]"), "click");
    assert.equal(getState().notice?.text, "the document is not imported");
    assert.equal(getState().notice?.tone, "warn");
  } finally {
    stop(); restore();
  }
});

// --- The tab bar ----------------------------------------------------------

test("the tab bar switches halves, and the TEXT half still renders", async () => {
  const restore = bridge({ ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen();
  try {
    assert.ok(root.querySelector("#image-card"), "the IMAGE half is on screen");
    await fire(root.querySelector('[data-anontab="text"]'), "click");
    assert.equal(getState().anonymiseTab, "text");
    assert.ok(root.querySelector("#compare-card"),
      "the TEXT half is the whole screen step 3 has always rendered, unchanged");
    assert.equal(root.querySelector("#image-card"), null);

    await fire(root.querySelector('[data-anontab="images"]'), "click");
    assert.equal(getState().anonymiseTab, "images");
    assert.ok(root.querySelector("#image-card"));
  } finally {
    stop(); restore();
  }
});

test("switching halves keeps the same document selected", async () => {
  const restore = bridge({ ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen();
  try {
    assert.equal(getState().resultDoc, DOC);
    await fire(root.querySelector('[data-anontab="text"]'), "click");
    assert.equal(getState().resultDoc, DOC,
      "the document selector is shared, so a tab switch is not a file switch");
  } finally {
    stop(); restore();
  }
});

test("the document selector writes the SHARED selection", async () => {
  const restore = bridge({ ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen();
  try {
    setState({
      documents: [
        { name: DOC, format: "pptx" },
        { name: "brief.docx", format: "docx" },
      ],
    });
    const select = root.querySelector("#image-doc");
    select.value = "brief.docx";
    await fire(select, "change");
    assert.equal(getState().resultDoc, "brief.docx",
      "it writes state.resultDoc, the field the Compare card reads");
  } finally {
    stop(); restore();
  }
});

// --- The banner chips -----------------------------------------------------

test("the filter chips change the filter and nothing else", async () => {
  const restore = bridge({ ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen();
  try {
    const before = getState().imageLayout;
    await fire(root.querySelector('[data-imgfilter="anonymised"]'), "click");
    assert.equal(getState().imageFilter, "anonymised");
    assert.equal(getState().imageLayout, before, "the view toggle is a separate question");
    assert.equal(getState().anonymiseTab, "images");
  } finally {
    stop(); restore();
  }
});

test("the view chips change the layout and nothing else", async () => {
  const restore = bridge({ ImageThumbnail: async () => ({ dataUrl: "" }) });
  const { root, stop } = screen();
  try {
    await fire(root.querySelector('[data-imglayout="tiles"]'), "click");
    assert.equal(getState().imageLayout, "tiles");
    assert.equal(getState().imageFilter, "all", "the filter is a separate question");
  } finally {
    stop(); restore();
  }
});

// --- The treatment panel --------------------------------------------------

test("the box text field debounces into ONE preview call", async () => {
  const PreviewImageTreatment = calls({ dataUrl: "data:image/png;base64,AA" });
  const restore = bridge({
    PreviewImageTreatment, ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const { root, stop } = screen([LOGO]);
  try {
    await fire(root.querySelector("[data-imgedit]"), "click");
    // Opening asks once: a chip press or an open is one deliberate act.
    await settle(300);
    PreviewImageTreatment.seen.length = 0;

    for (const text of ["C", "Cl", "Cli"]) {
      const field = root.querySelector("#image-box-text");
      field.value = text;
      await fire(field, "input");
    }
    await settle(300);
    assert.equal(PreviewImageTreatment.seen.length, 1,
      "three keystrokes are one render of the real treatment, not three");
    assert.deepEqual(PreviewImageTreatment.seen[0],
      [DOC, LOGO.id, { treatment: "box", boxText: "Cli" }, 480],
      "the call carries the LAST draft, and only the parameter the treatment uses");
  } finally {
    stop(); restore();
  }
});

test("the blur slider debounces into ONE preview call", async () => {
  const PreviewImageTreatment = calls({ dataUrl: "data:image/png;base64,AA" });
  const restore = bridge({
    PreviewImageTreatment, ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const { root, stop } = screen([LOGO]);
  try {
    await fire(root.querySelector("[data-imgedit]"), "click");
    await fire(root.querySelector('[data-imgtreatment="blur"]'), "click");
    await settle(300);
    PreviewImageTreatment.seen.length = 0;

    for (const value of ["4", "6", "8"]) {
      const slider = root.querySelector("#image-blur");
      slider.value = value;
      await fire(slider, "input");
    }
    await settle(300);
    assert.equal(PreviewImageTreatment.seen.length, 1,
      "holding a slider must not queue one real treatment render per step");
    assert.deepEqual(PreviewImageTreatment.seen[0],
      [DOC, LOGO.id, { treatment: "blur", blurStrength: 8 }, 480]);
  } finally {
    stop(); restore();
  }
});

test("Cancel records nothing and leaves the stored decision alone", async () => {
  const SetImageDecision = calls(undefined);
  const restore = bridge({
    SetImageDecision,
    PreviewImageTreatment: async () => ({ dataUrl: "data:image/png;base64,AA" }),
    ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const { root, stop } = screen([LOGO]);
  try {
    await fire(root.querySelector("[data-imgedit]"), "click");
    const field = root.querySelector("#image-box-text");
    field.value = "Client logo";
    await fire(field, "input");

    await fire(root.querySelector("#image-cancel"), "click");
    assert.equal(getState().imageEditor, null, "the panel is closed");
    assert.deepEqual(SetImageDecision.seen, [],
      "the panel keeps a DRAFT until Apply, so cancelling must record nothing");
    const stored = imagesFor(getState(), DOC).inventory.assets[0].decision;
    assert.deepEqual(stored, { treatment: "keep" }, "the stored decision is untouched");
  } finally {
    stop(); restore();
  }
});

test("Apply records the decision and the row says so", async () => {
  const SetImageDecision = calls(undefined);
  const restore = bridge({
    SetImageDecision,
    PreviewImageTreatment: async () => ({ dataUrl: "data:image/png;base64,AA" }),
    ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const { root, stop } = screen([LOGO]);
  try {
    await fire(root.querySelector("[data-imgedit]"), "click");
    const field = root.querySelector("#image-box-text");
    field.value = "Client logo";
    await fire(field, "input");
    await fire(root.querySelector("#image-apply"), "click");

    assert.deepEqual(SetImageDecision.seen,
      [[DOC, LOGO.id, { treatment: "box", boxText: "Client logo" }]]);
    assert.equal(getState().imageEditor, null, "Apply closes the panel");
    assert.equal(root.querySelector(".image-status").textContent, IMAGES.statusLabel.box,
      "the row reports what Go accepted without a second round trip to learn it");
    assert.equal(getState().notice?.text, IMAGES.decisionApplied("box"));
  } finally {
    stop(); restore();
  }
});

test("a refused Apply keeps the panel open with the reason on it", async () => {
  const restore = bridge({
    SetImageDecision: async () => { throw new Error("the box text is 130 characters, the maximum is 120") },
    PreviewImageTreatment: async () => ({ dataUrl: "data:image/png;base64,AA" }),
    ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const { root, stop } = screen([LOGO]);
  try {
    await fire(root.querySelector("[data-imgedit]"), "click");
    await fire(root.querySelector("#image-apply"), "click");
    assert.ok(getState().imageEditor, "the panel stays open, because the fix is on it");
    assert.match(root.querySelector("#image-panel-error").textContent, /maximum is 120/);
  } finally {
    stop(); restore();
  }
});

test("a disabled treatment chip is not a route into a refused decision", async () => {
  const SetImageDecision = calls(undefined);
  const restore = bridge({
    SetImageDecision,
    PreviewImageTreatment: async () => ({ dataUrl: "data:image/png;base64,AA" }),
    ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const vector = asset({ id: "ppt/media/image9.png", name: "Vector mark", format: "svg" });
  const { root, stop } = screen([vector]);
  try {
    await fire(root.querySelector("[data-imgedit]"), "click");
    const blur = root.querySelector('[data-imgtreatment="blur"]');
    assert.equal(blur.disabled, true, "an SVG cannot be blurred");
    assert.equal(getState().imageEditor.draft.treatment, "box",
      "the panel opens on a treatment the picture CAN carry, so Apply is never dead " +
      "on arrival");
  } finally {
    stop(); restore();
  }
});

test("a picture that can only be removed opens on remove", async () => {
  const restore = bridge({
    PreviewImageTreatment: async () => ({ dataUrl: "" }),
    ImageThumbnail: async () => ({ dataUrl: "" }),
  });
  const linked = asset({ id: "x/1.png", name: "Linked photo", format: "other", linked: true });
  const { root, stop } = screen([linked]);
  try {
    await fire(root.querySelector("[data-imgedit]"), "click");
    assert.equal(getState().imageEditor.draft.treatment, "remove");
  } finally {
    stop(); restore();
  }
});

// --- Lazy previews --------------------------------------------------------

test("a forty-picture deck does not fire forty preview calls on its first paint", async () => {
  const ImageThumbnail = calls({ dataUrl: "data:image/png;base64,AA" });
  const restore = bridge({ ImageThumbnail });
  // A document name of its own: the in-flight guard is keyed by document and
  // asset, and reusing a name would let one test's requests silence another's.
  const many = Array.from({ length: 40 }, (_, i) =>
    asset({ id: `ppt/media/many${i}.png`, name: `Picture ${i}` }));
  const { stop } = screen(many, {}, "many.pptx");
  try {
    assert.ok(ImageThumbnail.seen.length > 0, "the visible rows do ask for their previews");
    assert.ok(ImageThumbnail.seen.length <= 12,
      `one paint asks for one batch, not the whole deck; it asked for ` +
      `${ImageThumbnail.seen.length}`);
    // Every request is for a picture in this document, at the row size.
    for (const [docName, , maxPx] of ImageThumbnail.seen) {
      assert.equal(docName, "many.pptx");
      assert.equal(maxPx, 96);
    }
  } finally {
    stop(); restore();
  }
});

test("a preview that cannot be rendered says so once and is not asked for again", async () => {
  let asked = 0;
  const restore = bridge({
    ImageThumbnail: async () => { asked += 1; throw new Error("no bridge") },
  });
  const { root, stop } = screen([LOGO], {}, "failing.pptx");
  try {
    await settle(20);
    assert.equal(asked, 1, "the failure is cached, so a repaint does not retry forever");
    assert.equal(root.querySelector(".image-thumb-empty").textContent,
      IMAGES.previewUnavailable);
  } finally {
    stop(); restore();
  }
});
