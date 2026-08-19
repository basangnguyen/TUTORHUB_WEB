import react from "@vitejs/plugin-react";
import { cp } from "node:fs/promises";
import { resolve } from "node:path";
import { defineConfig, type Plugin } from "vite";

import { excalidrawCandidateSanitizer } from "./scripts/excalidrawCandidateSanitizer";

export default defineConfig({
  plugins: [excalidrawCandidateSanitizer(), copyExcalidrawFonts(), react()],
  resolve: {
    alias: {
      "@radix-ui/react-tabs": resolve(
        __dirname,
        "node_modules/@radix-ui/react-tabs/dist/index.mjs",
      ),
    },
  },
  build: {
    emptyOutDir: true,
    manifest: true,
    outDir: "dist-excalidraw",
    reportCompressedSize: true,
    rollupOptions: {
      input: resolve(__dirname, "excalidraw.html"),
    },
  },
});

function copyExcalidrawFonts(): Plugin {
  return {
    name: "tutorhub-copy-excalidraw-fonts",
    apply: "build",
    async closeBundle() {
      await cp(
        resolve(
          __dirname,
          "node_modules/@excalidraw/excalidraw/dist/prod/fonts",
        ),
        resolve(__dirname, "dist-excalidraw/excalidraw-assets/fonts"),
        { recursive: true },
      );
    },
  };
}
