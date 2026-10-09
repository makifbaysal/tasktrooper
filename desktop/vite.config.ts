import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The renderer is loaded from disk in a packaged app (file://…/dist/renderer/
// index.html), so every asset reference has to be relative. An absolute "/"
// base — which is what web/ uses, because it is served by a web server —
// resolves against the filesystem root under file:// and 404s every chunk.
export default defineConfig({
  root: path.resolve(import.meta.dirname, "src/renderer"),
  base: "./",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src/renderer"),
      "@shared": path.resolve(import.meta.dirname, "./src/shared"),
      "@ipc": path.resolve(import.meta.dirname, "./src/ipc"),
    },
  },
  server: {
    port: 3210,
    strictPort: true,
  },
  build: {
    outDir: path.resolve(import.meta.dirname, "dist/renderer"),
    emptyOutDir: true,
    // No sourcemaps in the shipped bundle: they are the one artefact that
    // could carry a literal from a secret-handling path into a file a user can
    // open, and the renderer never sees a secret to begin with — keeping it
    // that way is cheaper than auditing the map.
    sourcemap: false,
    // Two pages: the chrome, and the API-key window (main/keys/window.ts).
    rolldownOptions: {
      input: {
        index: path.resolve(import.meta.dirname, "src/renderer/index.html"),
        keys: path.resolve(import.meta.dirname, "src/renderer/keys.html"),
      },
    },
  },
});
