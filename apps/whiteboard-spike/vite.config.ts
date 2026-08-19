import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  build: {
    reportCompressedSize: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("@excalidraw")) {
            return "engine-excalidraw";
          }
          if (id.includes("tldraw") || id.includes("@tldraw")) {
            return "engine-tldraw";
          }
        },
      },
    },
  },
  test: {
    environment: "jsdom",
    include: [
      "src/**/*.test.{ts,tsx}",
      "server/**/*.test.ts",
      "scripts/**/*.test.ts",
    ],
    setupFiles: ["./src/test/setup.ts"],
  },
});
