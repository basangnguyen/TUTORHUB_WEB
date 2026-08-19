import { readdir, readFile, stat } from "node:fs/promises";
import { basename, extname, resolve } from "node:path";
import { scanClientBundle } from "../../../scripts/check-client-bundle-security.mjs";

const structureOnly = process.argv.includes("--structure-only");
const bundleDirectory = resolve(import.meta.dirname, "..", "dist-excalidraw");
const assetsDirectory = resolve(bundleDirectory, "assets");
const manifestPath = resolve(bundleDirectory, ".vite", "manifest.json");
const noticePath = resolve(bundleDirectory, "THIRD_PARTY_NOTICES.txt");

try {
  if (!(await stat(bundleDirectory)).isDirectory()) {
    throw new Error("not a directory");
  }
} catch {
  console.error("Excalidraw bundle not found. Run build:excalidraw first.");
  process.exit(1);
}

const structureIssues = [];
const assetNames = await readdir(assetsDirectory);
const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
const html = await readFile(
  resolve(bundleDirectory, "excalidraw.html"),
  "utf8",
);
const entryMatch = html.match(
  /<script[^>]+src=["']([^"']*\/assets\/[^"']+\.js)["']/i,
);
const entryName = entryMatch ? basename(entryMatch[1]) : null;
const engineName = basename(
  manifest["src/adapters/ExcalidrawBoard.tsx"]?.file ?? "",
);
const forbiddenAssetNames = assetNames.filter((name) =>
  /(?:^|[-_.])(?:tldraw|@tldraw)(?:[-_.]|$)/i.test(name),
);

try {
  const notice = await readFile(noticePath, "utf8");
  for (const marker of [
    "@excalidraw/excalidraw@0.18.1",
    "@radix-ui/react-tabs@1.1.21",
    "SIL Open Font License 1.1",
  ]) {
    if (!notice.includes(marker)) {
      structureIssues.push(
        `shipped third-party notice is missing ${marker}`,
      );
    }
  }
} catch {
  structureIssues.push("shipped THIRD_PARTY_NOTICES.txt is missing");
}

if (!entryName || !assetNames.includes(entryName)) {
  structureIssues.push("candidate entry chunk is missing");
}
if (!engineName || !assetNames.includes(engineName)) {
  structureIssues.push("isolated Excalidraw adapter/engine chunk is missing");
}
if (forbiddenAssetNames.length > 0) {
  structureIssues.push(
    `forbidden tldraw asset chunks found: ${forbiddenAssetNames.join(", ")}`,
  );
}

if (entryName) {
  const entrySource = await readFile(
    resolve(assetsDirectory, entryName),
    "utf8",
  );
  if (
    staticImportSpecifiers(entrySource).some((specifier) =>
      specifier.endsWith(`/${engineName}`),
    )
  ) {
    structureIssues.push(
      `${entryName}: statically imports the Excalidraw engine chunk`,
    );
  }
}

for (const name of assetNames.filter((asset) => extname(asset) === ".js")) {
  const source = await readFile(resolve(assetsDirectory, name), "utf8");
  if (/[@/]tldraw|\btldraw\b/i.test(source)) {
    structureIssues.push(`${name}: contains tldraw code or identifiers`);
  }
}

if (structureIssues.length > 0) {
  for (const issue of structureIssues) console.error(`BLOCK ${issue}`);
  console.error(
    `Excalidraw candidate structure gate blocked (${structureIssues.length} findings).`,
  );
  process.exitCode = 1;
} else {
  console.log(
    `PASS Excalidraw-only structure: ${assetNames.length} assets, isolated engine, lazy initial entry, no tldraw code.`,
  );
}

if (!structureOnly) {
  const security = await scanClientBundle(bundleDirectory);
  const upstreamPatterns = [
    {
      label: "demo Firebase host/config",
      expression: /firebaseio\.com|firebaseapp\.com/i,
    },
    {
      label: "Excalidraw demo collaboration config",
      expression: /excalidraw-room/i,
    },
  ];
  const upstreamIssues = [];
  for (const file of await listTextFiles(bundleDirectory)) {
    const source = await readFile(file, "utf8");
    for (const pattern of upstreamPatterns) {
      if (pattern.expression.test(source)) {
        upstreamIssues.push(`${basename(file)}: contains ${pattern.label}`);
      }
    }
  }

  const securityIssues = [...new Set([...security.issues, ...upstreamIssues])];
  if (securityIssues.length > 0) {
    for (const issue of securityIssues) console.error(`BLOCK ${issue}`);
    console.error(
      `Excalidraw candidate security gate blocked (${securityIssues.length} findings; values suppressed).`,
    );
    process.exitCode = 1;
  } else {
    console.log(
      `PASS Excalidraw candidate security: ${security.filesChecked} text assets checked.`,
    );
  }
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

async function listTextFiles(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) files.push(...(await listTextFiles(path)));
    else if (
      [".css", ".html", ".js", ".json", ".mjs"].includes(extname(entry.name))
    ) {
      files.push(path);
    }
  }
  return files;
}
