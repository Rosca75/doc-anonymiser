// ui.js, the shared UI toolkit.
//
// Pure STRING-RENDERING helpers, same pattern as html.js: no DOM access,
// fully testable under `node --test` (ui.test.js). Views compose these
// instead of writing ad-hoc markup, so the brand styling lives in one
// place (brand.css + style.css classes referenced here).
//
// Every user-supplied value is escaped before it reaches markup; the icon
// SVGs are trusted vendored assets (icons.js), never user input.

import { escapeHTML } from "./html.js";
import { ICONS } from "./icons.js";

/**
 * icon(name) returns the vendored Material Symbols SVG markup for the
 * given icon name, wrapped in a span so CSS can size and colour it.
 * Unknown names return an empty string (a missing icon must never break a
 * button render).
 * @param {string} name icon key in icons.js, e.g. "home"
 * @returns {string} safe HTML ("" for unknown names)
 */
export function icon(name) {
  const svg = ICONS[name];
  return svg ? `<span class="icon" aria-hidden="true">${svg}</span>` : "";
}

/**
 * button(label, opts) renders a styled <button>.
 * @param {string} label visible text (escaped)
 * @param {object} [opts]
 * @param {"primary"|"secondary"|"ghost"} [opts.kind] visual kind:
 *   primary = white text on accent orange (ONE per view, the hero element),
 *   secondary = black on quiet grey, ghost = borderless.
 * @param {string} [opts.id] element id
 * @param {string} [opts.icon] icon name rendered before the label
 * @param {boolean} [opts.disabled] disabled attribute
 * @param {string} [opts.title] tooltip (escaped)
 * @param {string} [opts.cls] extra CSS classes appended verbatim
 * @param {object} [opts.data] data-* attributes ({key: value}, escaped)
 * @param {boolean} [opts.current] marks the button as the current location
 *   (aria-current="page"): used by the permanent top menu to announce the
 *   active screen to assistive technology, since the highlight is a quiet
 *   visual one.
 * @param {string} [opts.ariaLabel] accessible name for an icon-only button
 *   (no visible label): the header's Help/Settings icon buttons need one
 *   since they render no text.
 * @returns {string} safe HTML
 */
export function button(label, opts = {}) {
  const kind = opts.kind ?? "secondary";
  const classes = `btn btn-${kind}${kind === "primary" ? " primary" : ""}${opts.cls ? " " + opts.cls : ""}`;
  const attrs = [
    `class="${classes}"`,
    opts.id ? `id="${escapeHTML(opts.id)}"` : "",
    opts.disabled ? "disabled" : "",
    opts.title ? `title="${escapeHTML(opts.title)}"` : "",
    opts.current ? `aria-current="page"` : "",
    opts.ariaLabel ? `aria-label="${escapeHTML(opts.ariaLabel)}"` : "",
  ];
  for (const [k, v] of Object.entries(opts.data ?? {})) {
    attrs.push(`data-${escapeHTML(k)}="${escapeHTML(v)}"`);
  }
  const iconHTML = opts.icon ? icon(opts.icon) : "";
  return `<button ${attrs.filter(Boolean).join(" ")}>${iconHTML}${iconHTML && label ? " " : ""}${escapeHTML(label)}</button>`;
}

// ===========================================================================
// The card kit.
//
// Everything below emits the markup the style.css "design system" block
// styles. The rule is the same as above: these are PURE string builders with
// no DOM and no state, every caller-supplied string is escaped, and the only
// trusted markup is what a caller passes as a `...HTML` argument (which its
// own builder has already escaped).
//
// Views compose these instead of writing card markup by hand, which is what
// stops each screen inventing its own spacing and tint.
// ===========================================================================

/**
 * card(opts) renders a fixed-height card: a head that stays put, a body that
 * is the only thing that scrolls, and an optional foot.
 *
 * The head/body/foot split is a LAYOUT CONTRACT, not decoration
 * (frontend/CLAUDE.md, the fixed-height layout contract): the body is the
 * scroll owner, so a card whose content grows scrolls internally instead of
 * making the page taller than the window.
 *
 * @param {object} opts
 * @param {string} [opts.title] the card heading (escaped)
 * @param {string} [opts.titleTooltip] a sentence shown only on hover over the
 *   heading (escaped, set as the h2's title attribute). Use it instead of a
 *   visible subtitle when the explanation is secondary to a compact card.
 * @param {string} [opts.subtitle] the sentence beside it (escaped). This is
 *   where the copy from the deleted per-step explainer banner now lives.
 * @param {string} [opts.caption] a right-aligned uppercase fact about the
 *   card, e.g. "WORKING FORM" (escaped)
 * @param {string} [opts.headRightHTML] trusted markup for the head's right
 *   side (controls); replaces `caption` when both are given
 * @param {string} [opts.bodyHTML] trusted markup for the scrolling body
 * @param {string} [opts.footHTML] trusted markup for the fixed foot
 * @param {string} [opts.beforeBodyHTML] trusted markup between head and body
 *   that must NOT scroll, e.g. a tab bar
 * @param {string} [opts.afterBodyHTML] trusted markup between body and foot
 *   that must NOT scroll, e.g. a notice strip
 * @param {string} [opts.id] element id
 * @param {string} [opts.cls] extra classes on the card
 * @param {string} [opts.bodyCls] extra classes on the body ("stack", "pane")
 * @param {boolean} [opts.keyBearing] marks the card as holding data that can
 *   re-identify people; tints the head with the key surface
 * @returns {string} safe HTML
 */
export function card(opts = {}) {
  const classes = `card${opts.keyBearing ? " key-bearing" : ""}${opts.cls ? " " + opts.cls : ""}`;
  const hasHead = !!(opts.title || opts.subtitle || opts.caption || opts.headRightHTML);
  const right = opts.headRightHTML
    ? `<div class="card-head-right">${opts.headRightHTML}</div>`
    : (opts.caption ? `<span class="card-caption">${escapeHTML(opts.caption)}</span>` : "");
  const head = hasHead
    ? `<div class="card-head${opts.headRightHTML ? " with-controls" : ""}">` +
      `<div class="card-head-left">` +
      (opts.title
        ? `<h2${opts.titleTooltip ? ` title="${escapeHTML(opts.titleTooltip)}"` : ""}>${escapeHTML(opts.title)}</h2>`
        : "") +
      (opts.subtitle ? `<span class="card-sub">${escapeHTML(opts.subtitle)}</span>` : "") +
      `</div>${right}</div>`
    : "";
  const body = opts.bodyHTML !== undefined
    ? `<div class="card-body${opts.bodyCls ? " " + opts.bodyCls : ""}">${opts.bodyHTML}</div>`
    : "";
  return `<section class="${classes}"${opts.id ? ` id="${escapeHTML(opts.id)}"` : ""}>` +
    head + (opts.beforeBodyHTML ?? "") + body + (opts.afterBodyHTML ?? "") +
    (opts.footHTML ? `<div class="card-foot">${opts.footHTML}</div>` : "") +
    `</section>`;
}

/**
 * countBadge(count, opts) renders the small pill that carries a tab's item
 * count.
 *
 * A null or undefined count renders NOTHING rather than "0": a tab with no
 * count to show (one that is not a list) must not grow an empty badge. Zero
 * itself does render, because "0 suggestions" is information.
 *
 * @param {number|null} count
 * @param {object} [opts]
 * @param {boolean} [opts.active] the owning tab is the active one
 * @returns {string} safe HTML ("" when there is no count)
 */
export function countBadge(count, opts = {}) {
  if (count === null || count === undefined) return "";
  return `<span class="count-badge${opts.active ? " active" : ""}">${escapeHTML(String(count))}</span>`;
}

/**
 * tabbar(tabs, opts) renders the underlined tab row that switches a card's
 * main content.
 *
 * Each button carries data-tab so ONE delegated listener on the container can
 * wire the whole row; a view never attaches a handler per tab.
 *
 * @param {Array<{id: string, label: string, count?: number, active?: boolean,
 *   disabled?: boolean, title?: string}>} tabs
 * @param {object} [opts]
 * @param {string} [opts.ariaLabel] accessible name for the row
 * @param {string} [opts.attr] the data attribute name (default "tab"), so a
 *   screen with two tab rows can wire them separately
 * @returns {string} safe HTML
 */
export function tabbar(tabs, opts = {}) {
  const attr = opts.attr ?? "tab";
  const buttons = (tabs ?? []).map((t) => {
    const active = !!t.active;
    return `<button class="tab${active ? " active" : ""}"` +
      ` data-${escapeHTML(attr)}="${escapeHTML(t.id)}"` +
      (t.disabled ? " disabled" : "") +
      (t.title ? ` title="${escapeHTML(t.title)}"` : "") +
      (active ? ` aria-current="true"` : "") +
      `>${escapeHTML(t.label)}${countBadge(t.count ?? null, { active })}</button>`;
  }).join("");
  return `<nav class="tabbar"${opts.ariaLabel ? ` aria-label="${escapeHTML(opts.ariaLabel)}"` : ""}>${buttons}</nav>`;
}

/**
 * chipRow(chips, opts) renders a row of pill buttons for picking one option
 * out of a small set (the presets, the rail's own tabs).
 *
 * @param {Array<{id: string, label: string, active?: boolean,
 *   disabled?: boolean, title?: string}>} chips
 * @param {object} [opts]
 * @param {string} [opts.attr] data attribute name (default "chip")
 * @param {boolean} [opts.square] the squarer, smaller spelling used by the rail
 * @param {string} [opts.ariaLabel] accessible name for the row
 * @returns {string} safe HTML
 */
export function chipRow(chips, opts = {}) {
  const attr = opts.attr ?? "chip";
  const buttons = (chips ?? []).map((c) =>
    `<button class="tint-chip${opts.square ? " square" : ""}${c.active ? " active" : ""}"` +
    ` data-${escapeHTML(attr)}="${escapeHTML(c.id)}"` +
    (c.disabled ? " disabled" : "") +
    (c.title ? ` title="${escapeHTML(c.title)}"` : "") +
    (c.active ? ` aria-pressed="true"` : "") +
    `>${escapeHTML(c.label)}</button>`).join("");
  return `<div class="chip-row"${opts.ariaLabel ? ` role="group" aria-label="${escapeHTML(opts.ariaLabel)}"` : ""}>${buttons}</div>`;
}

/**
 * sectionLabel(text, opts) renders the uppercase tracked label that opens a
 * block of the rail ("DOCUMENT COUNTRY", "PRESET").
 *
 * The text is upper-cased by CSS, not here, so the accessible name stays the
 * sentence-case string a screen reader should say.
 *
 * @param {string} text
 * @param {object} [opts]
 * @param {boolean} [opts.mini] the smaller spelling used for column captions
 * @returns {string} safe HTML
 */
export function sectionLabel(text, opts = {}) {
  return `<div class="section-label${opts.mini ? " mini" : ""}">${escapeHTML(text)}</div>`;
}

/**
 * statTile(value, label) renders one figure of the run summary
 * (7 REPLACEMENTS, 3 DOCUMENTS). Value first, then its caption, so the row
 * scans as numbers.
 * @param {string|number} value
 * @param {string} label
 * @returns {string} safe HTML
 */
export function statTile(value, label) {
  return `<span class="stat-tile">` +
    `<span class="stat-value">${escapeHTML(String(value))}</span>` +
    `<span class="stat-label">${escapeHTML(label)}</span>` +
    `</span>`;
}

/**
 * collapsibleGroup(id, title, bodyHTML, opts) renders one folded block: a
 * header that toggles, a right-hand count, optional header actions, and a
 * body that hides when closed.
 *
 * Unlike panel() above, the open/closed state is passed IN rather than read
 * from a Set: the screens keep it in view-local state they already
 * own, and a builder that reaches for a shared mutable Set cannot be tested
 * as a pure function.
 *
 * The header carries data-group-toggle; wireGroups() below attaches the
 * single delegated listener.
 *
 * @param {string} id stable group id (the toggle key)
 * @param {string} title header text (escaped)
 * @param {string} bodyHTML trusted body markup
 * @param {object} [opts]
 * @param {boolean} [opts.open] whether the body shows (default true)
 * @param {string} [opts.countLabel] the "3/6" style count (escaped)
 * @param {string} [opts.headRightHTML] trusted markup for header actions,
 *   rendered after the count
 * @param {string} [opts.cls] extra classes on the group
 * @returns {string} safe HTML
 */
export function collapsibleGroup(id, title, bodyHTML, opts = {}) {
  const open = opts.open ?? true;
  return `<section class="cgroup${opts.cls ? " " + opts.cls : ""}" data-open="${open}">` +
    `<div class="cgroup-head">` +
    `<span class="cgroup-title" data-group-toggle="${escapeHTML(id)}" role="button"` +
    ` tabindex="0" aria-expanded="${open}">` +
    `<span class="icon chevron${open ? "" : " closed"}" aria-hidden="true">${ICONS.expand_more}</span>` +
    `${escapeHTML(title)}</span>` +
    `<span class="cgroup-head-right">` +
    (opts.countLabel ? `<span class="cgroup-count">${escapeHTML(opts.countLabel)}</span>` : "") +
    (opts.headRightHTML ?? "") +
    `</span></div>` +
    `<div class="cgroup-body">${bodyHTML}</div>` +
    `</section>`;
}

/**
 * helpTooltip(text, opts) is the ONE way an explanation reaches the interface
 * without taking permanent vertical space.
 *
 * The Configure panel used to carry a paragraph under every control. Read once,
 * they are useful; read on every visit, they are what pushes the panel taller
 * than the window and buries the controls that ARE in use. So the explanation
 * moves here: a small information icon beside the label, and the text on demand.
 *
 * The behaviour is fixed and not negotiable per call site, because an
 * explanation the keyboard cannot reach is an explanation half the users do not
 * have:
 *
 *   - it opens on POINTER HOVER and on KEYBOARD FOCUS, so it is reachable by
 *     Tab as well as by mouse;
 *   - it stays readable while either the icon or the bubble itself has focus or
 *     hover, so a pointer can travel into it to select the text;
 *   - it closes on pointer leave, on blur and on Escape;
 *   - it is wired with aria-describedby, so a screen reader announces it as the
 *     description of the control rather than as a stray paragraph;
 *   - it is NOT a native dialog (frontend/CLAUDE.md: no confirm/alert/prompt),
 *     and it is not a `title` attribute either, because a title is invisible to
 *     touch, slow to appear, and unstyleable.
 *
 * POSITIONING: the bubble is rendered inside the trigger but positioned
 * `fixed` by CSS, so it escapes the rail's clipping scroll container. A bubble
 * positioned `absolute` inside an `overflow: auto` ancestor is clipped at the
 * container's edge, which is exactly the failure the rendering harness checks
 * for.
 *
 * @param {string} text the explanation, from copy.js
 * @param {object} [opts]
 * @param {string} [opts.id] the bubble's id; derived from the text when omitted
 * @param {string} [opts.label] the icon's accessible name; defaults to a generic
 *   "More about this"
 * @returns {string} safe HTML
 */
export function helpTooltip(text, opts = {}) {
  if (!text) return "";
  const id = opts.id ?? `help-${hashText(text)}`;
  const label = opts.label ?? "More about this";
  return `<span class="help" data-help>` +
    `<button type="button" class="help-icon" aria-label="${escapeHTML(label)}"` +
    ` aria-describedby="${escapeHTML(id)}">${icon("info")}</button>` +
    `<span class="help-bubble" id="${escapeHTML(id)}" role="tooltip">${escapeHTML(text)}</span>` +
    `</span>`;
}

/**
 * hashText(text) is a short stable id from a string, so a tooltip's bubble can be
 * referenced by aria-describedby without every call site inventing an id (and
 * without two of them colliding on the same one).
 */
function hashText(text) {
  let h = 0;
  for (let i = 0; i < text.length; i++) {
    h = (h * 31 + text.charCodeAt(i)) | 0;
  }
  return Math.abs(h).toString(36);
}

/**
 * wireHelpTooltips(container) attaches the open/close behaviour to every
 * helpTooltip in the container.
 *
 * The open state is a data attribute on the wrapper rather than a class, so CSS
 * owns the appearance and this owns only WHEN. Hover and focus are tracked
 * separately and the bubble stays open while EITHER holds: with one flag, moving
 * the pointer from the icon into the bubble closes the bubble the pointer is
 * moving towards.
 *
 * @param {HTMLElement} container the view container after innerHTML
 */
export function wireHelpTooltips(container) {
  for (const help of container.querySelectorAll("[data-help]")) {
    let hovered = false;
    let focused = false;
    const sync = () => {
      if (hovered || focused) help.setAttribute("data-open", "true");
      else help.removeAttribute("data-open");
    };
    help.addEventListener("pointerenter", () => { hovered = true; sync(); });
    help.addEventListener("pointerleave", () => { hovered = false; sync(); });
    help.addEventListener("focusin", () => { focused = true; sync(); });
    help.addEventListener("focusout", () => { focused = false; sync(); });
    // Escape closes it and gives up the hover too, so the bubble does not
    // reappear the instant the pointer twitches without leaving.
    help.addEventListener("keydown", (ev) => {
      if (ev.key !== "Escape") return;
      ev.stopPropagation();
      hovered = false;
      focused = false;
      sync();
      help.querySelector(".help-icon")?.blur();
    });
  }
}

/**
 * dropdownChecklist(opts) is a compact summary control that opens a small list of
 * checkboxes.
 *
 * It exists so a set of switches can live in the rail without spending one row
 * per switch forever. The CLOSED state is a summary the user can read at a
 * glance; the open state is the list. That trade is what keeps the panel short as
 * sources are added.
 *
 * It is DATA-DRIVEN: the rows come from the caller's list, so a new entry needs
 * no new markup and no new persisted flag.
 *
 * @param {object} opts
 * @param {string} opts.id the control's element id
 * @param {string} opts.label the label beside the summary
 * @param {string} opts.summary the closed read-out ("Off", a name, "N sources")
 * @param {string} opts.listLabel the heading over the open list
 * @param {boolean} opts.open whether the list is showing
 * @param {Array<{id: string, label: string, detail?: string, checked: boolean}>} opts.rows
 * @param {string} [opts.helpHTML] a helpTooltip to sit beside the label
 * @returns {string} safe HTML
 */
export function dropdownChecklist(opts) {
  const rows = (opts.rows ?? []).map((row) =>
    `<label class="checklist-row">` +
    `<input type="checkbox" class="checklist-box" data-checklist="${escapeHTML(row.id)}"` +
    `${row.checked ? " checked" : ""}/>` +
    `<span class="checklist-label">${escapeHTML(row.label)}</span>` +
    (row.detail ? `<span class="checklist-detail">${escapeHTML(row.detail)}</span>` : "") +
    `</label>`).join("");

  return `<div class="checklist" id="${escapeHTML(opts.id)}"${opts.open ? ` data-open="true"` : ""}>` +
    `<div class="checklist-head">` +
    `<span class="checklist-title">${escapeHTML(opts.label)}${opts.helpHTML ?? ""}</span>` +
    `<button type="button" class="checklist-toggle" aria-expanded="${opts.open ? "true" : "false"}"` +
    ` aria-controls="${escapeHTML(opts.id)}-list">` +
    `<span class="checklist-summary">${escapeHTML(opts.summary)}</span>${icon("expand_more")}` +
    `</button>` +
    `</div>` +
    `<div class="checklist-list" id="${escapeHTML(opts.id)}-list"${opts.open ? "" : " hidden"}>` +
    sectionLabel(opts.listLabel, { mini: true }) +
    rows +
    `</div></div>`;
}

/**
 * wireGroups(container, onToggle) attaches ONE delegated listener that fires
 * onToggle(id) for every collapsibleGroup header in the container, for both a
 * click and a keyboard activation (the header is a role="button" span, so it
 * needs Enter and Space wired by hand).
 * @param {HTMLElement} container the view container after innerHTML
 * @param {(id: string) => void} onToggle
 */
export function wireGroups(container, onToggle) {
  for (const head of container.querySelectorAll("[data-group-toggle]")) {
    const id = head.dataset.groupToggle;
    head.addEventListener("click", () => onToggle(id));
    head.addEventListener("keydown", (ev) => {
      if (ev.key !== "Enter" && ev.key !== " ") return;
      ev.preventDefault();
      onToggle(id);
    });
  }
}

/**
 * stepFooter(opts) renders a screen's own footer bar: a quiet "Back to X"
 * link on the left, then a readiness hint and the primary "CONTINUE TO Y" on
 * the right.
 *
 * Every wizard screen has one: the global Back/Next
 * footer, because a footer that belongs to the shell cannot say what
 * the CURRENT screen still needs before the user can move on. That sentence
 * is `hint`, and it is the whole reason this replaced the global bar.
 *
 * The primary button here is the ONE loud element on the screen, which is why
 * no other builder in this kit emits an accent fill.
 *
 * @param {object} opts
 * @param {string} [opts.backLabel] e.g. "Back to Import"; omitted on step 1
 * @param {string} [opts.backId] element id for the back link
 * @param {string} [opts.hint] what is ready, or what is missing (escaped)
 * @param {string} opts.nextLabel e.g. "CONTINUE TO ANONYMISE"
 * @param {string} opts.nextId element id for the primary button
 * @param {boolean} [opts.nextDisabled] gate the move
 * @param {string} [opts.nextTitle] tooltip, normally why it is disabled
 * @param {boolean} [opts.standalone] render as a card of its own rather than
 *   as the foot of the workspace card
 * @param {string} [opts.rightHTML] trusted markup REPLACING the primary button.
 *   The last step of the wizard uses it: there is nothing to continue to, and
 *   its right-hand action must not look like the other screens' CONTINUE.
 * @returns {string} safe HTML
 */
export function stepFooter(opts = {}) {
  const back = opts.backLabel
    ? button(opts.backLabel, {
      kind: "ghost", id: opts.backId, icon: "arrow_back", cls: "step-back",
    })
    : `<span></span>`; // keeps the primary button justified right on step 1
  const next = opts.rightHTML ?? button(opts.nextLabel ?? "", {
    kind: "primary", id: opts.nextId, icon: "arrow_forward",
    cls: "step-next", disabled: !!opts.nextDisabled, title: opts.nextTitle,
  });
  return `<div class="step-footer${opts.standalone ? " standalone" : ""}">` +
    back +
    `<div class="step-footer-right">` +
    (opts.hint ? `<span class="step-hint">${escapeHTML(opts.hint)}</span>` : "") +
    next +
    `</div></div>`;
}

// NOTICE_TONES maps a notice tone onto its CSS class and leading icon. The
// three tones are the only ones there are, and each means something
// different (brand.css, the notice tint pairs):
//
//   ok something was written or saved
//   info a statement of fact with nothing to fix
//   warn it worked AND the result needs care (a key left the application)
//
// An unknown tone degrades to info rather than throwing: a notice is how the
// application tells the user something, and it must still appear even if its
// caller passed a typo.
const NOTICE_TONES = {
  ok: { cls: "notice-ok", icon: "check_circle" },
  info: { cls: "notice-info", icon: "description" },
  warn: { cls: "notice-warn", icon: "warning" },
};

/**
 * toastHTML(notice, opts) renders the notice strip.
 * @param {{text: string, tone?: string}|null} notice state.notice
 * @param {object} [opts]
 * @param {string} [opts.dismissId] id for the dismiss button (default
 *   "notice-dismiss", which toast.js wireNotice looks for)
 * @returns {string} safe HTML ("" when there is no notice)
 */
export function toastHTML(notice, opts = {}) {
  if (!notice?.text) return "";
  const tone = NOTICE_TONES[notice.tone] ?? NOTICE_TONES.info;
  return `<div class="notice ${tone.cls}" role="status">` +
    icon(tone.icon) +
    `<span class="notice-text">${escapeHTML(notice.text)}</span>` +
    button("", {
      kind: "ghost", id: opts.dismissId ?? "notice-dismiss", icon: "close",
      cls: "notice-dismiss", ariaLabel: "Dismiss", title: "Dismiss",
    }) +
    `</div>`;
}

/**
 * modalHTML(question) renders the in-app confirm that replaces every native
 * confirm.
 *
 * The two buttons carry fixed ids (modal-confirm / modal-cancel) because
 * modal.js wires them once at shell level; a caller never wires its own. When
 * the question carries `choices`, it is a pick-one instead of a yes/no: the
 * affirmative button is replaced by one button per choice (each tagged with
 * `data-choice="<id>"`), and only the Cancel affordance remains fixed.
 *
 * @param {{title: string, body: string, confirmLabel?: string,
 *   cancelLabel?: string, keyBearing?: boolean,
 *   choices?: Array<{id: string, label: string}>}|null} question state.confirm
 * @returns {string} safe HTML ("" when nothing is being asked)
 */
export function modalHTML(question) {
  if (!question) return "";
  const keyBearing = !!question.keyBearing;
  const choices = Array.isArray(question.choices) ? question.choices : null;
  const cancel = button(question.cancelLabel ?? "Cancel", { kind: "secondary", id: "modal-cancel" });
  const actions = choices && choices.length
    ? cancel + choices.map((c) => button(c.label, { kind: "primary", data: { choice: c.id } })).join("")
    : cancel + button(question.confirmLabel ?? "Continue", { kind: "primary", id: "modal-confirm" });
  return `<div class="modal-layer" role="presentation">` +
    `<div class="modal${keyBearing ? " key-bearing" : ""}" role="dialog" aria-modal="true"` +
    ` aria-label="${escapeHTML(question.title ?? "Confirm")}">` +
    `<div class="modal-head">${icon(keyBearing ? "warning" : "help")}` +
    `<span>${escapeHTML(question.title ?? "")}</span></div>` +
    `<div class="modal-body"><p>${escapeHTML(question.body ?? "")}</p>` +
    `<div class="modal-actions">` +
    actions +
    `</div></div></div></div>`;
}
