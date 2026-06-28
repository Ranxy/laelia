import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config";

// Test config layered on top of the shared vite config so the `@` / `@/react`
// aliases and the React plugin carry over. Vitest 4 defaults to the node
// environment, which makes every DOM-based UI test fail with
// `ReferenceError: document is not defined`; jsdom is the project default and
// per-file `// @vitest-environment` pragmas can still override it.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: "jsdom",
      setupFiles: ["./src/test/setup.ts"],
      globals: true,
    },
  })
);