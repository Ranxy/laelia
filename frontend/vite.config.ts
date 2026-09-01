import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      // PWA app-shell precaching + Web Push in one service worker. We ship a
      // hand-written manifest in public/ (manifest:false) and register the SW
      // ourselves at app boot (injectRegister:null), so the existing
      // web-push.ts keeps working unchanged.
      strategies: "injectManifest",
      srcDir: "sw",
      filename: "sw.ts",
      registerType: "autoUpdate",
      injectRegister: null,
      manifest: false,
      injectManifest: {
        globPatterns: ["**/*.{js,css,html,svg,png,webmanifest,woff2}"],
        maximumFileSizeToCacheInBytes: 5 * 1024 * 1024,
      },
      devOptions: { enabled: false },
    }),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    // Warn only on genuinely large chunks. Rolldown already splits shared
    // modules into per-route chunks; manualChunks below only buckets the big,
    // stable vendors so the entry chunk stays lean and the browser can cache
    // them across deploys.
    chunkSizeWarningLimit: 800,
    rollupOptions: {
      checks: {
        // Streamdown and its dependencies may contain pure annotations that
        // Rolldown cannot associate with a statement. They are harmless.
        invalidAnnotation: false,
      },
      output: {
        // Rolldown's manualChunks takes a function. Bucket the big, stable
        // vendors into cacheable chunks so the entry chunk stays lean.
        manualChunks(id) {
          const normalizedId = id.replaceAll("\\\\", "/");
          // Streamdown has a pnpm peer suffix containing `react-dom` and
          // `react`; exclude it before matching React package directories so
          // Markdown stays in the lazy chunks that use it.
          if (
            normalizedId.includes("/node_modules/streamdown/") ||
            normalizedId.includes("/node_modules/@streamdown/code/")
          ) {
            return undefined;
          }
          // Check i18next before the React match (react-i18next would
          // otherwise land in the React vendor).
          if (
            normalizedId.includes("/node_modules/react-i18next/") ||
            normalizedId.includes("/node_modules/i18next/")
          ) {
            return "i18next";
          }
          if (
            normalizedId.includes("/node_modules/react-router/") ||
            normalizedId.includes("/node_modules/react-router-dom/") ||
            normalizedId.includes("/node_modules/react-dom/") ||
            normalizedId.includes("/node_modules/react/")
          ) {
            return "react";
          }
          if (id.includes("@connectrpc") || id.includes("@bufbuild")) {
            return "connect";
          }
          if (id.includes("/zustand/")) {
            return "zustand";
          }
          if (id.includes("@base-ui") || id.includes("floating-ui")) {
            return "base-ui";
          }
          // Keep Streamdown and its code plugin out of manual vendor buckets.
          // The default splitter can leave Markdown dependencies in the lazy
          // route chunks instead of forcing them into the initial entry.
          return undefined;
        },
      },
    },
  },
  server: {
    proxy: {
      "/v1": {
        target: "http://localhost:8181",
        changeOrigin: true,
      },
      "/api/version": {
        target: "http://localhost:8181",
        changeOrigin: true,
      },
    },
    allowedHosts:["localhost","laeliapage.metaxisdata.com"],
  },
});
