// state.js — the single source of truth for frontend state (CLAUDE.md §4).
//
// Minimal store: a plain object plus a subscribe/notify mechanism. Views
// never keep their own copies of shared data — they read from getState()
// and re-render when notified. Mutations go through setState() only, so
// every state change is observable and debuggable in one place.

// The initial application state. Grows as views are added; keep every field
// documented.
const state = {
  // Result of the bridge self-test: null = not yet run, "pong" = OK,
  // any other string = the error message to display.
  bridge: null,

  // Result of the last Ollama probe: null = not yet probed, otherwise the
  // OllamaStatus object from Go ({available, models, detail}).
  ollama: null,
};

// Subscribers are plain callbacks invoked after every state change.
const listeners = new Set();

/**
 * getState() returns the live state object. Treat it as read-only outside
 * this module — always mutate via setState().
 */
export function getState() {
  return state;
}

/**
 * setState(patch) shallow-merges the patch into the state and notifies all
 * subscribers. Example: setState({ bridge: "pong" }).
 * @param {object} patch fields to update.
 */
export function setState(patch) {
  Object.assign(state, patch);
  for (const fn of listeners) fn(state);
}

/**
 * subscribe(fn) registers a callback run after every state change.
 * @param {(state: object) => void} fn
 * @returns {() => void} an unsubscribe function.
 */
export function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
