// testhtml.js, a tiny HTML query helper for the view tests. DEV-TIME ONLY.
//
// Why it exists: the view modules build HTML strings, so a whole screen can be
// asserted without a browser. Until now those assertions were substring
// matches, which is why four bugs about what a pane SHOWS lived happily beside
// green tests. This helper lets a test say "the text inside #original-pane"
// instead of "the output contains this substring somewhere".
//
// It is deliberately small and dependency-free (no npm, CLAUDE.md frontend
// charter). It understands the markup this application generates and nothing
// more: well-formed tags, quoted attributes, no exotic parsing. It is never
// embedded in the binary, exactly like the *.test.js files beside it.
//
// Selector grammar (one simple selector, no combinators):
//   tag            "pre"
//   #id            "#original-pane"
//   .class         ".pane-body"
//   [attr]         "[data-original]"
//   any combination of those, e.g. "mark.pii[data-ph]"

/** VOID_TAGS never have a closing tag, so they never have inner HTML. */
const VOID_TAGS = new Set(["br", "hr", "img", "input", "meta", "link", "source"]);

/** parseSelector(sel) → {tag, id, cls, attrs[]} */
function parseSelector(sel) {
  const out = { tag: "", id: "", cls: "", attrs: [] };
  const rest = String(sel).replace(/\[([^\]=]+)\]/g, (_, name) => {
    out.attrs.push(name.trim());
    return "";
  });
  const m = /^([a-zA-Z0-9-]*)(?:#([^.\s]+))?(?:\.([^.\s#]+))?$/.exec(rest.trim());
  if (!m) throw new Error(`testhtml: selector ${sel} is not supported`);
  out.tag = m[1] ?? "";
  out.id = m[2] ?? "";
  out.cls = m[3] ?? "";
  return out;
}

/** attrsOf(openTag) parses `name="value"` pairs out of one opening tag. */
function attrsOf(openTag) {
  const attrs = {};
  for (const m of openTag.matchAll(/([a-zA-Z_:][-a-zA-Z0-9_:.]*)(?:="([^"]*)")?/g)) {
    // The first match is the tag name itself (no value, position 1).
    if (m.index === 1) continue;
    attrs[m[1]] = m[2] ?? "";
  }
  return attrs;
}

function matches(tagName, attrs, want) {
  if (want.tag && want.tag !== tagName) return false;
  if (want.id && attrs.id !== want.id) return false;
  if (want.cls && !String(attrs.class ?? "").split(/\s+/).includes(want.cls)) return false;
  for (const a of want.attrs) {
    if (!(a in attrs)) return false;
  }
  return true;
}

/**
 * all(html, selector) → array of {tag, attrs, outer, inner} for every element
 * matching the selector, in document order. Nested matches are returned too.
 */
export function all(html, selector) {
  const want = parseSelector(selector);
  const source = String(html);
  const found = [];
  const tagRe = /<([a-zA-Z0-9-]+)((?:"[^"]*"|[^>"])*)>/g;
  for (const open of source.matchAll(tagRe)) {
    const tagName = open[1].toLowerCase();
    const attrs = attrsOf(open[0]);
    if (!matches(tagName, attrs, want)) continue;
    const start = open.index + open[0].length;
    let inner = "";
    let end = start;
    if (!VOID_TAGS.has(tagName) && !open[0].endsWith("/>")) {
      // Walk forward counting same-name tags so nesting resolves correctly.
      const walker = new RegExp(`<${tagName}\\b|</${tagName}\\s*>`, "gi");
      walker.lastIndex = start;
      let depth = 1;
      let m;
      while ((m = walker.exec(source)) !== null) {
        depth += m[0][1] === "/" ? -1 : 1;
        if (depth === 0) break;
      }
      if (m) {
        inner = source.slice(start, m.index);
        end = m.index + m[0].length;
      } else {
        // Unbalanced markup is a bug in the view, not in the test: say so.
        throw new Error(`testhtml: <${tagName}> matching ${selector} is never closed`);
      }
    }
    found.push({
      tag: tagName, attrs, inner,
      outer: source.slice(open.index, Math.max(end, start)),
    });
  }
  return found;
}

/** one(html, selector) → the single match, or throws with a readable reason. */
export function one(html, selector) {
  const found = all(html, selector);
  if (found.length === 0) throw new Error(`testhtml: nothing matches ${selector}`);
  if (found.length > 1) throw new Error(`testhtml: ${found.length} elements match ${selector}, expected one`);
  return found[0];
}

/** exists(html, selector) → boolean, for "the disabled section is not rendered". */
export function exists(html, selector) {
  return all(html, selector).length > 0;
}

/** attr(html, selector, name) → the attribute value of the single match. */
export function attr(html, selector, name) {
  return one(html, selector).attrs[name];
}

/** unescape(text) reverses html.js escapeHTML, so a test compares real text. */
export function unescape(text) {
  return String(text)
    .replaceAll("&lt;", "<").replaceAll("&gt;", ">")
    .replaceAll("&quot;", '"').replaceAll("&#39;", "'")
    .replaceAll("&amp;", "&"); // last: an escaped & must not re-expand
}

/**
 * textOf(html, selector) → the visible text of the single match: tags removed,
 * entities decoded. This is what the user reads, which is what the preview
 * tests need to assert.
 */
export function textOf(html, selector) {
  // The `>?` makes the closing bracket optional so a dangling, unterminated
  // `<script` at the end of a string is stripped too. A plain /<[^>]*>/g would
  // leave it behind, which CodeQL flags as incomplete tag sanitisation.
  // Apply repeatedly until stable so multi-character/overlapping patterns
  // cannot re-form dangerous fragments such as "<script" after one pass.
  let stripped = one(html, selector).inner;
  let prev;
  do {
    prev = stripped;
    stripped = stripped.replace(/<[^>]*>?/g, "");
  } while (stripped !== prev);
  return unescape(stripped);
}
