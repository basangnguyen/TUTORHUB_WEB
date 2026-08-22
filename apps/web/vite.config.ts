import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { configDefaults, defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [stripExcalidrawUpstreamFirebaseKey(), react()],
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: "livekit",
              test: /node_modules[\\/](?:@livekit|livekit-client)[\\/]/,
              priority: 100,
              includeDependenciesRecursively: false,
            },
          ],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
        rewrite: (path) =>
          path.replace(/^\/api\/(health|ready)(?=\/|\?|$)/, "/$1"),
      },
    },
  },
  test: {
    environment: "jsdom",
    exclude: [...configDefaults.exclude, "e2e/**"],
    maxWorkers: 4,
    setupFiles: ["./src/test/setup.ts"],
  },
});

function stripExcalidrawUpstreamFirebaseKey(): Plugin {
  const googleAPIKey = /\bAIza[0-9A-Za-z_-]{30,}\b/g;
  return {
    name: "tutorhub-strip-excalidraw-upstream-firebase-key",
    enforce: "pre",
    transform(source, id) {
      const normalizedID = id.replaceAll("\\", "/");
      if (
        !normalizedID.includes("/@excalidraw/excalidraw/dist/") ||
        !source.includes("VITE_APP_FIREBASE_CONFIG")
      ) {
        return null;
      }
      const redacted = source.replace(
        googleAPIKey,
        "TutorHubDisabledUpstreamFirebaseKey",
      );
      return redacted === source ? null : { code: redacted, map: null };
    },
  };
}
