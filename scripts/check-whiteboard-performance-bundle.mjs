import { gzipSync } from "node:zlib";
import { readdir, readFile, stat } from "node:fs/promises";
import { basename, resolve } from "node:path";
import { pathToFileURL } from "node:url";

export const P514_BUNDLE_BUDGETS = Object.freeze({
  entryRawBytes: 768 * 1024,
  whiteboardClosureGzipBytes: 2 * 1024 * 1024,
  whiteboardClosureRawBytes: 6 * 1024 * 1024,
  whiteboardRootRawBytes: 800 * 1024,
});

export async function scanWhiteboardPerformanceBundle(directory) {
  const assetsDirectory = resolve(directory, "assets");
  const assetNames = await readdir(assetsDirectory);
  const indexHtml = await readFile(resolve(directory, "index.html"), "utf8");
  const entryMatch = indexHtml.match(
    /<script[^>]+src=["']([^"']*\/assets\/index-[^"']+\.js)["']/i,
  );
  const entryName = entryMatch ? basename(entryMatch[1]) : null;
  const whiteboardNames = assetNames.filter(
    (name) => name.startsWith("LazyWhiteboardEngine-") && name.endsWith(".js"),
  );
  const issues = [];

  if (!entryName || !assetNames.includes(entryName)) {
    issues.push("client entry chunk is missing");
  }
  if (whiteboardNames.length !== 1) {
    issues.push("exactly one lazy whiteboard engine chunk is required");
  }
  if (issues.length > 0) return { issues };

  const whiteboardName = whiteboardNames[0];
  const entryClosure = await staticClosure(assetsDirectory, entryName);
  if (entryClosure.has(whiteboardName)) {
    issues.push("initial entry statically imports the whiteboard engine");
  }

  const whiteboardClosure = await staticClosure(
    assetsDirectory,
    whiteboardName,
  );
  const entryRawBytes = await fileBytes(assetsDirectory, entryName);
  const whiteboardRootRawBytes = await fileBytes(
    assetsDirectory,
    whiteboardName,
  );
  let whiteboardClosureRawBytes = 0;
  let whiteboardClosureGzipBytes = 0;
  for (const name of whiteboardClosure) {
    const bytes = await readFile(resolve(assetsDirectory, name));
    whiteboardClosureRawBytes += bytes.byteLength;
    whiteboardClosureGzipBytes += gzipSync(bytes).byteLength;
  }

  enforceBudget(
    issues,
    "entry raw",
    entryRawBytes,
    P514_BUNDLE_BUDGETS.entryRawBytes,
  );
  enforceBudget(
    issues,
    "whiteboard root raw",
    whiteboardRootRawBytes,
    P514_BUNDLE_BUDGETS.whiteboardRootRawBytes,
  );
  enforceBudget(
    issues,
    "whiteboard closure raw",
    whiteboardClosureRawBytes,
    P514_BUNDLE_BUDGETS.whiteboardClosureRawBytes,
  );
  enforceBudget(
    issues,
    "whiteboard closure gzip",
    whiteboardClosureGzipBytes,
    P514_BUNDLE_BUDGETS.whiteboardClosureGzipBytes,
  );

  return {
    entryChunk: entryName,
    entryRawBytes,
    issues,
    whiteboardChunk: whiteboardName,
    whiteboardClosureChunks: whiteboardClosure.size,
    whiteboardClosureGzipBytes,
    whiteboardClosureRawBytes,
    whiteboardRootRawBytes,
  };
}

async function staticClosure(assetsDirectory, rootName) {
  const visited = new Set();
  const pending = [rootName];
  while (pending.length > 0) {
    const name = pending.pop();
    if (!name || visited.has(name)) continue;
    visited.add(name);
    const source = await readFile(resolve(assetsDirectory, name), "utf8");
    for (const specifier of staticImportSpecifiers(source)) {
      const dependency = basename(specifier.split("?")[0]);
      if (dependency.endsWith(".js") && !visited.has(dependency)) {
        pending.push(dependency);
      }
    }
  }
  return visited;
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

async function fileBytes(directory, name) {
  return (await stat(resolve(directory, name))).size;
}

function enforceBudget(issues, label, actual, budget) {
  if (actual > budget)
    issues.push(`${label} exceeds ${budget} bytes (${actual})`);
}

const isMain =
  process.argv[1] &&
  pathToFileURL(resolve(process.argv[1])).href === import.meta.url;
if (isMain) {
  const directory = resolve(process.cwd(), "apps", "web", "dist");
  const result = await scanWhiteboardPerformanceBundle(directory);
  if (result.issues.length > 0) {
    console.error(result.issues.join("\n"));
    process.exitCode = 1;
  } else {
    console.log("P5_COLLAB_14_BUNDLE", JSON.stringify(result));
  }
}
