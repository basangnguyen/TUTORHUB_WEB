import { readdir, readFile, stat } from "node:fs/promises";
import { basename, extname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const TEXT_EXTENSIONS = new Set([
  ".css",
  ".html",
  ".js",
  ".json",
  ".map",
  ".mjs",
]);
const FORBIDDEN_PATTERNS = [
  {
    label: "server-only environment variable",
    expression:
      /\b(?:B2_APPLICATION_KEY|DATABASE_MIGRATION_URL|DATABASE_POOL_URL|HF_TOKEN|HUGGINGFACE_TOKEN|LIVEKIT_API_SECRET|OIDC_CLIENT_SECRET|ZITADEL_CLIENT_SECRET)\b/g,
  },
  {
    label: "private key material",
    expression: /-----BEGIN (?:EC |OPENSSH |RSA )?PRIVATE KEY-----/g,
  },
  { label: "AWS access key", expression: /\bAKIA[0-9A-Z]{16}\b/g },
  { label: "Google API key", expression: /\bAIza[0-9A-Za-z_-]{30,}\b/g },
  { label: "GitHub token", expression: /\bgh[pousr]_[A-Za-z0-9_]{30,}\b/g },
  { label: "Hugging Face token", expression: /\bhf_[A-Za-z0-9]{24,}\b/g },
];

async function listTextFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listTextFiles(path)));
    } else if (TEXT_EXTENSIONS.has(extname(entry.name).toLowerCase())) {
      files.push(path);
    }
  }

  return files;
}

export async function scanClientBundle(directory) {
  const issues = [];
  const files = await listTextFiles(directory);

  for (const file of files) {
    const source = await readFile(file, "utf8");
    for (const pattern of FORBIDDEN_PATTERNS) {
      pattern.expression.lastIndex = 0;
      if (pattern.expression.test(source)) {
        issues.push(`${file}: contains ${pattern.label}`);
      }
    }
  }

  return { filesChecked: files.length, issues };
}

export async function scanMediaBundleIsolation(directory) {
  const issues = [];
  const assetsDirectory = resolve(directory, "assets");
  const assetNames = await readdir(assetsDirectory);
  const indexHtml = await readFile(resolve(directory, "index.html"), "utf8");
  const entryMatch = indexHtml.match(
    /<script[^>]+src=["']([^"']*\/assets\/index-[^"']+\.js)["']/i,
  );
  const entryName = entryMatch ? basename(entryMatch[1]) : null;
  const prejoinName = singleAsset(assetNames, "MediaSpacePages-", ".js");
  const roomName = singleAsset(assetNames, "MediaSpaceRoomPage-", ".js");
  const liveKitName = singleAsset(assetNames, "livekit-", ".js");

  if (!entryName || !assetNames.includes(entryName)) {
    issues.push("client entry chunk is missing");
  }
  if (!prejoinName) issues.push("canonical media prejoin chunk is missing");
  if (!roomName) issues.push("canonical media room chunk is missing");
  if (!liveKitName) issues.push("isolated LiveKit vendor chunk is missing");

  for (const name of [entryName, prejoinName]) {
    if (!name) continue;
    const source = await readFile(resolve(assetsDirectory, name), "utf8");
    if (
      staticImportSpecifiers(source).some(
        (specifier) =>
          specifier.includes("/livekit-") ||
          specifier.includes("/LiveKitPages-"),
      )
    ) {
      issues.push(`${name}: statically imports the LiveKit media bundle`);
    }
  }

  if (roomName && liveKitName) {
    const roomSource = await readFile(
      resolve(assetsDirectory, roomName),
      "utf8",
    );
    if (
      !staticImportSpecifiers(roomSource).some((specifier) =>
        specifier.endsWith(`/${liveKitName}`),
      )
    ) {
      issues.push(`${roomName}: does not import the isolated LiveKit chunk`);
    }
  }

  return issues;
}

function staticImportSpecifiers(source) {
  return [
    ...[...source.matchAll(/\bimport\s*["']([^"']+)["']/g)].map(
      (match) => match[1],
    ),
    ...[
      ...source.matchAll(
        /\b(?:import|export)\s*[^"'()]*?\bfrom\s*["']([^"']+)["']/g,
      ),
    ].map((match) => match[1]),
  ];
}

function singleAsset(names, prefix, suffix) {
  const matches = names.filter(
    (name) => name.startsWith(prefix) && name.endsWith(suffix),
  );
  return matches.length === 1 ? matches[0] : null;
}

const isMain =
  process.argv[1] &&
  pathToFileURL(resolve(process.argv[1])).href === import.meta.url;
if (isMain) {
  const bundleDirectory = resolve(process.cwd(), "apps", "web", "dist");
  try {
    if (!(await stat(bundleDirectory)).isDirectory()) {
      throw new Error("not a directory");
    }
  } catch {
    console.error(
      `Client bundle not found: ${bundleDirectory}. Run the web build first.`,
    );
    process.exit(1);
  }

  const result = await scanClientBundle(bundleDirectory);
  const issues = [
    ...result.issues,
    ...(await scanMediaBundleIsolation(bundleDirectory)),
  ];
  if (issues.length > 0) {
    console.error(issues.join("\n"));
    process.exitCode = 1;
  } else {
    console.log(
      `Client bundle security check passed (${result.filesChecked} files).`,
    );
  }
}
