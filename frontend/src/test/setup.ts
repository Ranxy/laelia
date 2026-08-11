// Global test setup. Loaded before every test file via vitest.config.ts.
// Registers @testing-library/jest-dom matchers (toBeInTheDocument, etc.) on the
// vitest `expect` so DOM assertions read naturally across UI tests.
import "@testing-library/jest-dom";

// jsdom does not implement Element.prototype.scrollIntoView, so components
// that scroll the active option into view (e.g. MentionPopup) throw during
// effects. Polyfill it as a no-op so those tests exercise the render path
// without depending on a real layout engine.
if (
  typeof Element !== "undefined" &&
  typeof Element.prototype.scrollIntoView !== "function"
) {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}

// jsdom does not implement window.matchMedia (used by useIsDesktop). Polyfill
// it as a non-matching query so components using the hook render in tests
// (mobile by default; tests that care mock the hook explicitly).
if (typeof window !== "undefined" && typeof window.matchMedia !== "function") {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}
