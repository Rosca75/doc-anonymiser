// ui.js, the shared UI toolkit (BUILD-02 Phase 1c).
//
// Pure STRING-RENDERING helpers, same pattern as html.js: no DOM access,
// fully testable under `node --test` (ui.test.js). Views compose these
// instead of writing ad-hoc markup, so the PwC brand styling lives in one
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
  ];
  for (const [k, v] of Object.entries(opts.data ?? {})) {
    attrs.push(`data-${escapeHTML(k)}="${escapeHTML(v)}"`);
  }
  const iconHTML = opts.icon ? icon(opts.icon) : "";
  return `<button ${attrs.filter(Boolean).join(" ")}>${iconHTML}${iconHTML && label ? " " : ""}${escapeHTML(label)}</button>`;
}

/**
 * banner(title, body, opts) renders the per-step explainer strip: quiet
 * grey background, black text, an icon slot (BUILD-02 Phase 1c).
 * @param {string} title short heading (escaped)
 * @param {string} body one or two explaining sentences (escaped)
 * @param {object} [opts]
 * @param {string} [opts.icon] icon name for the leading slot
 * @returns {string} safe HTML
 */
export function banner(title, body, opts = {}) {
  return `<div class="step-banner">${opts.icon ? icon(opts.icon) : ""}` +
    `<span class="banner-title">${escapeHTML(title)}</span>` +
    `<span class="banner-body">${escapeHTML(body)}</span></div>`;
}

/**
 * panel(id, title, contentHTML, opts) renders a <section class="panel">.
 * When collapsible, the header row carries data-panel-toggle and the
 * section a data-collapsed attribute; wirePanels() (below) attaches the
 * click handler after the view sets innerHTML. Collapsed state is
 * view-local: pass the view's own Set of collapsed panel ids via
 * opts.collapsedSet at render time.
 *
 * contentHTML is trusted view markup (already escaped by its builder);
 * id and title are escaped here.
 *
 * @param {string} id stable panel id (used for state + wiring)
 * @param {string} title panel heading
 * @param {string} contentHTML panel body markup (trusted, pre-escaped)
 * @param {object} [opts]
 * @param {boolean} [opts.collapsible] header toggles the body
 * @param {boolean} [opts.startOpen] initial state when the set has no entry (default true)
 * @param {Set<string>} [opts.collapsedSet] the view's collapsed-panel id set
 * @param {string} [opts.headExtraHTML] trusted markup rendered right of the title (buttons)
 * @returns {string} safe HTML
 */
export function panel(id, title, contentHTML, opts = {}) {
  const collapsible = !!opts.collapsible;
  const startOpen = opts.startOpen ?? true;
  // The Set records panels the user toggled AWAY from their default.
  const toggled = opts.collapsedSet?.has(id) ?? false;
  const collapsed = collapsible && (startOpen ? toggled : !toggled);
  const chevron = collapsed ? icon("expand_more") : icon("expand_less");
  return `<section class="panel" id="${escapeHTML(id)}" ${collapsible ? `data-collapsed="${collapsed}"` : ""}>` +
    `<div class="panel-head${collapsible ? " collapsible" : ""}" ${collapsible ? `data-panel-toggle="${escapeHTML(id)}"` : ""}>` +
    `<h2>${escapeHTML(title)}</h2>` +
    `<div class="panel-head-right">${opts.headExtraHTML ?? ""}${collapsible ? `<span class="panel-toggle">${chevron}</span>` : ""}</div>` +
    `</div>` +
    `<div class="panel-body">${contentHTML}</div>` +
    `</section>`;
}

/**
 * wirePanels(container, collapsedSet, rerender) attaches the collapse
 * toggle to every collapsible panel rendered by panel(). The toggle flips
 * the panel id in the view's Set and calls rerender() so the chevron and
 * body state repaint. Buttons INSIDE the header (headExtraHTML) do not
 * toggle: their clicks are ignored here so actions stay actions.
 * @param {HTMLElement} container the view container after innerHTML
 * @param {Set<string>} collapsedSet the view-local toggled-panel id set
 * @param {Function} rerender re-render callback
 */
export function wirePanels(container, collapsedSet, rerender) {
  for (const head of container.querySelectorAll("[data-panel-toggle]")) {
    head.addEventListener("click", (ev) => {
      if (ev.target.closest("button") && !ev.target.closest(".panel-toggle")) return;
      const id = head.dataset.panelToggle;
      if (collapsedSet.has(id)) collapsedSet.delete(id); else collapsedSet.add(id);
      rerender();
    });
  }
}
