import { readdir } from "node:fs/promises";
import { resolve } from "node:path";
import { scanClientBundle } from "../../../scripts/check-client-bundle-security.mjs";

const bundleDirectory = resolve(import.meta.dirname, "../dist-collab");
const result = await scanClientBundle(bundleDirectory);
const assetNames = await readdir(resolve(bundleDirectory, "assets"));
const forbiddenLaneAssets = assetNames.filter((name) =>
  /(?:excalidraw|hocuspocus|yjs)/i.test(name),
);
const issues = [
  ...result.issues,
  ...forbiddenLaneAssets.map(
    (name) => `${name}: dependency ngoài tldraw candidate lane`,
  ),
];

if (issues.length > 0) {
  console.error(issues.join("\n"));
  process.exitCode = 1;
} else {
  console.log(
    `P5-COLLAB-01 bundle guard passed (${result.filesChecked} files, tldraw-only candidate).`,
  );
}
