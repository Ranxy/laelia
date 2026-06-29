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
