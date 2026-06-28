// Global test setup. Loaded before every test file via vitest.config.ts.
// Registers @testing-library/jest-dom matchers (toBeInTheDocument, etc.) on the
// vitest `expect` so DOM assertions read naturally across UI tests.
import "@testing-library/jest-dom";
