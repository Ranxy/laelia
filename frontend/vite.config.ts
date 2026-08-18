import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
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
        // markstream-react's published dist contains misplaced /* @__PURE__ */
        // comments (e.g. after `case "xxx":` labels). Rolldown warns and
        // ignores them; they are harmless. Silence the noise.
        invalidAnnotation: false,
      },
      output: {
        // Rolldown's manualChunks takes a function. Bucket the big, stable
        // vendors into cacheable chunks so the entry chunk stays lean.
        manualChunks(id) {
          // Check i18next before the broad react match (react-i18next would
          // otherwise land in the react vendor).
          if (
            id.includes("react-i18next") ||
            id.includes("/i18next/")
          ) {
            return "i18next";
          }
          if (
            id.includes("react-router") ||
            id.includes("react-dom") ||
            id.includes("/react/")
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
          // Deliberately NOT bucketing markstream/stream-markdown: forcing them
          // into a named chunk made Rolldown treat it as an entry static
          // dependency (fetched at boot), defeating the lazy preview overlays.
          // The default splitter makes it a shared chunk loaded on demand.
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
