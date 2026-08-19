import { resolve } from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  build: {
    emptyOutDir: true,
    outDir: "dist-collab",
    reportCompressedSize: true,
    rollupOptions: {
      input: resolve(import.meta.dirname, "collab.html"),
      output: {
        manualChunks(id) {
          if (id.includes("tldraw") || id.includes("@tldraw")) {
            return "engine-tldraw";
          }
        },
      },
    },
  },
});
