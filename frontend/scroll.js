// scroll.js, scroll-position preservation across re-renders
//
// The problem it solves: every reducer call runs setState, every setState
// repaints the whole shell, and the repaint rewrites innerHTML. A rewritten
// element starts at scrollTop 0, so ticking a checkbox two thirds of the way
// down the Configure screen, or adding a term to the allowlist, threw the
// user back to the top of the page. With four category groups and a long
// allowlist that made the screen genuinely hard to use.
//
// The fix is deliberately the SMALL one. Re-rendering only the changed
// control would avoid the problem entirely, but it means every view giving
// up its "render the whole thing from state" simplicity, which is the
// discipline the whole frontend is built on (CLAUDE.md section 4). So the
// render stays whole and the scroll offset is carried across it.
//
// The offset is restored SYNCHRONOUSLY, in the same task as the re-render,
// so the browser never paints an intermediate frame at the top: there is no
// visible jump, not even a flicker.
//
// This module is DOM-only and has no state of its own, which is why it is
// not part of state.js.

/**
 * scrollOwner() returns the element that actually scrolls the application
 * body: main#view, the shell's single scrolling region. Falls back to null
 * when it is absent (the shell has not painted yet, or a unit test).
 * @returns {HTMLElement|null}
 */
export function scrollOwner() {
  return typeof document === "undefined" ? null : document.querySelector("main#view");
}

/**
 * keepScrollPosition(mutate) runs mutate() and puts the scroll position
 * back where it was.
 *
 * mutate is expected to trigger a re-render (directly or through a
 * reducer). Anything it returns is passed through, including a promise:
 * an async mutate has its scroll restored twice, once after the
 * synchronous part (which is where the re-render happens) and once when
 * the promise settles, so a slow bridge round-trip that repaints again
 * cannot undo the restore either.
 *
 * @param {Function} mutate the state change to perform
 * @returns {*} whatever mutate returned
 */
export function keepScrollPosition(mutate) {
  return keepScrollPositionOf(scrollOwner, mutate);
}

/**
 * keepScrollPositionIn(selector, mutate) is keepScrollPosition for a scroller
 * that is NOT the page body.
 *
 * The fixed-height layout (CLAUDE.md) scrolls inside a card body, not inside
 * main#view, so restoring main#view's scroll leaves a card scrolled to the top
 * anyway. That is the bug where removing a variant two thirds down the value
 * list threw the list back to the first card. The selector names the actual
 * scroller (e.g. "#identify-workspace .card-body"), re-queried after the
 * re-render because the repaint replaces the element.
 *
 * @param {string} selector a CSS selector for the scrolling element
 * @param {Function} mutate the state change to perform
 * @returns {*} whatever mutate returned
 */
export function keepScrollPositionIn(selector, mutate) {
  const find = () => (typeof document === "undefined" ? null : document.querySelector(selector));
  return keepScrollPositionOf(find, mutate);
}

/**
 * keepScrollPositionOf(find, mutate) is the shared machinery: `find` returns
 * the element that scrolls, re-queried on every call so a repaint that replaces
 * it is handled. It is not exported; the two named entry points above are.
 */
function keepScrollPositionOf(find, mutate) {
  const owner = find();
  if (!owner) return mutate();

  const top = owner.scrollTop;
  const left = owner.scrollLeft;
  const restore = () => {
    const current = find();
    if (!current) return;
    current.scrollTop = top;
    current.scrollLeft = left;
  };

  let result;
  try {
    result = mutate();
  } finally {
    restore();
  }

  if (result && typeof result.then === "function") {
    return result.then(
      (value) => { restore(); return value; },
      (err) => { restore(); throw err; });
  }
  return result;
}
