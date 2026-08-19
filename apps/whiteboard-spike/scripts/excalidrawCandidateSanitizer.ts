import type { Plugin } from "vite";

const expectedMatches = {
  googleApiKey: 1,
  firebaseHost: 2,
  // Vite reaches three of the five raw release-chunk occurrences.
  demoRoomName: 3,
} as const;

const patterns = {
  googleApiKey: /\bAIza[0-9A-Za-z_-]{30,}\b/g,
  firebaseHost: /[A-Za-z0-9.-]+(?:firebaseio|firebaseapp)\.com/g,
  demoRoomName: /excalidraw-room/g,
} as const;

const replacements = {
  googleApiKey: "TUTORHUB_DISABLED_GOOGLE_API_KEY",
  firebaseHost: "disabled.invalid",
  demoRoomName: "tutorhub-collab-disabled",
} as const;

type MatchKey = keyof typeof expectedMatches;

export function sanitizeExcalidrawCandidateSource(source: string) {
  const counts = emptyCounts();
  let code = source;

  for (const key of Object.keys(patterns) as MatchKey[]) {
    code = code.replace(patterns[key], () => {
      counts[key] += 1;
      return replacements[key];
    });
  }

  return { code, counts };
}

export function excalidrawCandidateSanitizer(): Plugin {
  const totals = emptyCounts();

  return {
    name: "tutorhub-excalidraw-candidate-sanitizer",
    enforce: "pre",
    apply: "build",
    transform(source, id) {
      const normalizedId = id.replaceAll("\\", "/");
      if (!normalizedId.includes("/@excalidraw/excalidraw/dist/prod/")) {
        return null;
      }

      const result = sanitizeExcalidrawCandidateSource(source);
      for (const key of Object.keys(totals) as MatchKey[]) {
        totals[key] += result.counts[key];
      }

      return result.code === source ? null : { code: result.code, map: null };
    },
    buildEnd(error) {
      if (error) return;

      const mismatches = (Object.keys(expectedMatches) as MatchKey[])
        .filter((key) => totals[key] !== expectedMatches[key])
        .map(
          (key) =>
            `${key}: expected ${expectedMatches[key]}, found ${totals[key]}`,
        );

      if (mismatches.length > 0) {
        throw new Error(
          `Excalidraw candidate sanitization drifted (${mismatches.join("; ")}).`,
        );
      }
    },
  };
}

function emptyCounts(): Record<MatchKey, number> {
  return {
    googleApiKey: 0,
    firebaseHost: 0,
    demoRoomName: 0,
  };
}
