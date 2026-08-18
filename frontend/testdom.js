// testdom.js, a minimal DOM for the wiring tests. DEV-TIME ONLY.
//
// Why it exists: `testhtml.js` answers "what does this pane SHOW", by querying
// the HTML STRING a view builds. That is the right tool for a render assertion
// and the wrong one for a wiring assertion, because a string preserves whatever
// the view wrote while a BROWSER re-reads it. The difference is not cosmetic: an
// HTML parser LOWER-CASES attribute names, so `data-mainText` reaches the DOM as
// `data-maintext` and `dataset.mainText` is undefined in every handler that
// reads it. A string test cannot see that; every control it describes renders
// exactly as written and does nothing when clicked.
//
// So this module parses the markup the way a parser does and exposes the result
// through the same surface the view modules use: `querySelector`,
// `querySelectorAll`, `dataset`, `classList`, `addEventListener`,
// `replaceWith`, `ownerDocument.createElement`. A handler wired against these
// nodes fails for the same reason it fails in the application.
//
// It understands the markup this application generates and nothing more:
// well-formed tags, quoted attributes, one compound selector at a time. It is
// dependency-free (frontend charter: no npm) and never embedded in the binary,
// exactly like the *.test.js files beside it.
//
// Selector grammar (one compound selector, no combinators):
//   tag            "button"
//   #id            "#btn-add-value"
//   .class         ".value-card"
//   [attr]         "[data-wstab]"
//   [attr="v"]     '[data-act="merge"]'
//   :checked       ".group-pick:checked"
//   any combination, e.g. '.solve-action[data-act="merge"]'

/** VOID_TAGS never have a closing tag, so they never have children. */
const VOID_TAGS = new Set(["br", "hr", "img", "input", "meta", "link", "source"]);

/**
 * datasetKey(attrName) maps an attribute name to its `dataset` key, the way a
 * browser does: the `data-` prefix is dropped and each `-x` becomes `X`.
 * The attribute name arriving here is ALREADY lower case, because that is what
 * the parser hands the DOM, which is the whole point of this module.
 */
function datasetKey(attrName) {
  return attrName.slice("data-".length).replace(/-([a-z0-9])/g, (_, c) => c.toUpperCase());
}

/**
 * FakeNode is one element. Its `attributes` keys are lower-cased on the way in,
 * so every derived view of them (dataset, id, class) inherits the parser's
 * behaviour rather than the author's spelling.
 */
class FakeNode {
  constructor(tag, attributes, ownerDocument) {
    this.tag = tag;
    this.tagName = tag.toUpperCase();
    this.attributes = attributes;
    this.children = [];
    this.parentNode = null;
    this.ownerDocument = ownerDocument;
    this.style = {};
    this.listeners = new Map();
    this.focused = false;
    this.selected = false;
    this.text = "";

    this.dataset = {};
    for (const [name, value] of Object.entries(attributes)) {
      if (name.startsWith("data-")) this.dataset[datasetKey(name)] = value;
    }

    // The DOM reflects a handful of attributes as live properties. Only the
    // ones this application reads are modelled; anything else stays an
    // attribute, so a test that needs more fails loudly instead of quietly
    // reading undefined.
    this.value = attributes.value ?? "";
    this.checked = "checked" in attributes;
    this.disabled = "disabled" in attributes;
    this.placeholder = attributes.placeholder ?? "";
    this.className = attributes.class ?? "";

    const self = this;
    this.classList = {
      contains(name) { return self.classes().includes(name); },
      add(name) {
        if (!self.classes().includes(name)) self.setClasses([...self.classes(), name]);
      },
      remove(name) { self.setClasses(self.classes().filter((c) => c !== name)); },
      toggle(name, on) {
        const want = on ?? !self.classList.contains(name);
        if (want) self.classList.add(name);
        else self.classList.remove(name);
      },
    };
  }

  classes() {
    return String(this.className ?? "").split(/\s+/).filter(Boolean);
  }

  setClasses(list) {
    this.className = list.join(" ");
    this.attributes.class = this.className;
  }

  get id() { return this.attributes.id ?? ""; }

  /** textContent concatenates this element's own text and its descendants', and
   *  assigning it replaces the subtree with one text run, as the DOM does. */
  get textContent() {
    return this.text + this.children.map((c) => c.textContent).join("");
  }

  set textContent(value) {
    this.children = [];
    this.text = String(value);
  }

  /** innerHTML is write-only here: the tests set it (that is how a view
   *  renders) and never read it back, so parsing is the only direction needed. */
  set innerHTML(html) {
    this.children = parseChildren(String(html), this.ownerDocument);
    this.text = "";
    for (const child of this.children) child.parentNode = this;
  }

  setAttribute(name, value) {
    const key = String(name).toLowerCase();
    this.attributes[key] = String(value);
    if (key.startsWith("data-")) this.dataset[datasetKey(key)] = String(value);
    if (key === "class") this.className = String(value);
  }

  getAttribute(name) {
    return this.attributes[String(name).toLowerCase()] ?? null;
  }

  removeAttribute(name) {
    delete this.attributes[String(name).toLowerCase()];
  }

  addEventListener(type, fn) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(fn);
  }

  focus() { this.focused = true; }

  select() { this.selected = true; }

  /** replaceWith(node) swaps this element for another in its parent's child
   *  list, which is how the inline rename input takes the name button's place. */
  replaceWith(node) {
    const parent = this.parentNode;
    if (!parent) return;
    parent.children = parent.children.map((c) => (c === this ? node : c));
    node.parentNode = parent;
    this.parentNode = null;
  }

  remove() {
    const parent = this.parentNode;
    if (!parent) return;
    parent.children = parent.children.filter((c) => c !== this);
    this.parentNode = null;
  }

  /** closest(selector) walks up from this element, itself included. */
  closest(selector) {
    const want = parseSelector(selector);
    for (let node = this; node; node = node.parentNode) {
      if (node.matches && node.matches(want)) return node;
    }
    return null;
  }

  matches(want) {
    const parsed = typeof want === "string" ? parseSelector(want) : want;
    if (parsed.tag && parsed.tag !== this.tag) return false;
    if (parsed.id && this.id !== parsed.id) return false;
    for (const cls of parsed.classes) {
      if (!this.classes().includes(cls)) return false;
    }
    for (const { name, value } of parsed.attrs) {
      if (!(name in this.attributes)) return false;
      if (value !== null && this.attributes[name] !== value) return false;
    }
    if (parsed.checked && !this.checked) return false;
    return true;
  }

  /** descendants() lists every element below this one, in document order. */
  descendants() {
    const out = [];
    const walk = (node) => {
      for (const child of node.children) {
        out.push(child);
        walk(child);
      }
    };
    walk(this);
    return out;
  }

  querySelectorAll(selector) {
    const want = parseSelector(selector);
    return this.descendants().filter((n) => n.matches(want));
  }

  querySelector(selector) {
    return this.querySelectorAll(selector)[0] ?? null;
  }
}

/** parseSelector(sel) → {tag, id, classes[], attrs[{name,value}], checked} */
function parseSelector(sel) {
  const out = { tag: "", id: "", classes: [], attrs: [], checked: false };
  let rest = String(sel).trim();
  rest = rest.replace(/\[([^\]=]+)(?:="([^"]*)")?\]/g, (_, name, value) => {
    out.attrs.push({ name: name.trim().toLowerCase(), value: value ?? null });
    return "";
  });
  if (rest.includes(":checked")) {
    out.checked = true;
    rest = rest.replaceAll(":checked", "");
  }
  const m = /^([a-zA-Z0-9-]*)(?:#([^.\s]+))?((?:\.[^.\s#]+)*)$/.exec(rest);
  if (!m) throw new Error(`testdom: selector ${sel} is not supported`);
  out.tag = (m[1] ?? "").toLowerCase();
  out.id = m[2] ?? "";
  out.classes = (m[3] ?? "").split(".").filter(Boolean);
  return out;
}

/** attrsOf(openTag) parses `name="value"` pairs, LOWER-CASING every name. That
 *  single call is what makes this module worth having. */
function attrsOf(openTag) {
  const attrs = {};
  const body = openTag.replace(/^<[a-zA-Z0-9-]+/, "").replace(/\/?>$/, "");
  for (const m of body.matchAll(/([a-zA-Z_:][-a-zA-Z0-9_:.]*)(?:="([^"]*)")?/g)) {
    attrs[m[1].toLowerCase()] = unescapeHTML(m[2] ?? "");
  }
  return attrs;
}

/** unescapeHTML reverses html.js escapeHTML, so a dataset value equals the text
 *  the view was given rather than its markup spelling. */
function unescapeHTML(text) {
  return String(text)
    .replaceAll("&lt;", "<").replaceAll("&gt;", ">")
    .replaceAll("&quot;", '"').replaceAll("&#39;", "'")
    .replaceAll("&amp;", "&"); // last: an escaped & must not re-expand
}

/** parseChildren(html, ownerDocument) builds the element tree of one fragment.
 *  Text runs are folded into the nearest element's own text, which is all the
 *  handlers under test read. */
function parseChildren(html, ownerDocument) {
  const source = String(html);
  const roots = [];
  const stack = [];
  const push = (node) => {
    const parent = stack[stack.length - 1];
    if (parent) {
      node.parentNode = parent;
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  };

  const tokenRe = /<\/?([a-zA-Z0-9-]+)((?:"[^"]*"|[^>"])*)>/g;
  let cursor = 0;
  for (const token of source.matchAll(tokenRe)) {
    const between = source.slice(cursor, token.index);
    if (between) {
      const parent = stack[stack.length - 1];
      if (parent) parent.text += unescapeHTML(between);
    }
    cursor = token.index + token[0].length;

    const tag = token[1].toLowerCase();
    if (token[0].startsWith("</")) {
      // Close the innermost matching element. Unbalanced markup is a bug in the
      // view, not in the test, so say which tag went wrong.
      const at = stack.map((n) => n.tag).lastIndexOf(tag);
      if (at === -1) throw new Error(`testdom: </${tag}> closes nothing`);
      stack.length = at;
      continue;
    }
    const node = new FakeNode(tag, attrsOf(token[0]), ownerDocument);
    push(node);
    if (!VOID_TAGS.has(tag) && !token[0].endsWith("/>")) stack.push(node);
  }
  const trailing = source.slice(cursor);
  if (trailing) {
    const parent = stack[stack.length - 1];
    if (parent) parent.text += unescapeHTML(trailing);
  }
  return roots;
}

/**
 * createDocument() returns the document object a node reaches through
 * `ownerDocument`. Only `createElement` is modelled, because that is the only
 * document API the view modules use (the inline rename and spelling inputs).
 */
function createDocument() {
  const doc = {
    createElement(tag) {
      return new FakeNode(String(tag).toLowerCase(), {}, doc);
    },
  };
  return doc;
}

/**
 * container() returns an empty element a view can render into: assigning
 * `innerHTML` parses the markup, and everything below it is queryable.
 * @param {string} [tag] the element name (a div, unless a test needs otherwise)
 */
export function container(tag = "div") {
  const doc = createDocument();
  return new FakeNode(tag, {}, doc);
}

/**
 * fire(node, type, extra) dispatches one event, bubbling up the parent chain
 * until a handler calls stopPropagation.
 *
 * Bubbling is modelled rather than faked because the application depends on it:
 * a spelling chip is a drag handle wrapping a remove button, and the remove
 * handler's `stopPropagation()` is the only thing keeping a click from doing
 * both jobs. A dispatcher that never bubbled would report that guard as working
 * whether it was there or not.
 *
 * @param {object} node the element the event starts on
 * @param {string} type the event name, e.g. "click"
 * @param {object} [extra] extra event fields ({key: "Enter"}, a target value)
 * @returns {Promise<void>} resolves once every handler's promise has settled,
 *   so a test can await an async handler without a timer
 */
export async function fire(node, type, extra = {}) {
  let stopped = false;
  const event = {
    type,
    target: node,
    currentTarget: node,
    stopPropagation() { stopped = true; },
    preventDefault() {},
    ...extra,
  };
  const pending = [];
  for (let at = node; at && !stopped; at = at.parentNode) {
    event.currentTarget = at;
    for (const fn of at.listeners.get(type) ?? []) {
      pending.push(fn(event));
      if (stopped) break;
    }
  }
  await Promise.all(pending);
}
