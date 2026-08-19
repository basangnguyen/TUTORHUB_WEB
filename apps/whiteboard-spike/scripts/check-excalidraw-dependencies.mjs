import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { readdir, readFile } from "node:fs/promises";

const spikeRoot = resolve(import.meta.dirname, "..");
const appManifestPath = join(spikeRoot, "package.json");
const appManifest = await readJson(appManifestPath);
const appRequire = createRequire(appManifestPath);
const expected = {
  engine: "0.18.1",
  radixTabs: "1.1.21",
  react: "19.2.7",
  reactDom: "19.2.7",
};
const issues = [];

checkExact("@excalidraw/excalidraw", appManifest.dependencies, expected.engine);
checkExact("@radix-ui/react-tabs", appManifest.dependencies, expected.radixTabs);
checkExact("react", appManifest.dependencies, expected.react);
checkExact("react-dom", appManifest.dependencies, expected.reactDom);

const enginePath = await resolvePackageJson(
  "@excalidraw/excalidraw",
  appRequire,
);
const engineManifest = await readJson(enginePath);
const reactManifest = await readJson(
  await resolvePackageJson("react", appRequire),
);
const reactDomManifest = await readJson(
  await resolvePackageJson("react-dom", appRequire),
);

checkValue("installed Excalidraw", engineManifest.version, expected.engine);
checkValue("installed React", reactManifest.version, expected.react);
checkValue("installed ReactDOM", reactDomManifest.version, expected.reactDom);
checkValue("Excalidraw package license", engineManifest.license, "MIT");

for (const peer of ["react", "react-dom"]) {
  const range = engineManifest.peerDependencies?.[peer];
  if (!supportsReact19(range)) {
    issues.push(
      `@excalidraw/excalidraw ${engineManifest.version} does not declare React 19 support for ${peer}.`,
    );
  }
}

const engineRequire = createRequire(enginePath);
const transitiveLicenses = new Map();
for (const dependency of Object.keys(
  engineManifest.dependencies ?? {},
).sort()) {
  const dependencyPath = await resolvePackageJson(dependency, engineRequire);
  const dependencyManifest = await readJson(dependencyPath);
  const license = dependencyManifest.license;
  const dependencyFiles = await listRelativeFiles(dirname(dependencyPath));
  const packagedLicense = dependencyFiles.find((path) =>
    /(?:^|\/)(?:licen[cs]e|copying)(?:[.-]|$)/i.test(path),
  );
  if (
    (typeof license !== "string" || license.trim() === "") &&
    !packagedLicense
  ) {
    issues.push(
      `${dependency}@${dependencyManifest.version} has neither license metadata nor a packaged license file.`,
    );
  } else if (/\b(?:AGPL|BUSL|GPL|SSPL)\b/i.test(license)) {
    issues.push(
      `${dependency}@${dependencyManifest.version} uses review-required license ${license}.`,
    );
  }
  transitiveLicenses.set(
    `${dependency}@${dependencyManifest.version}`,
    license ?? `packaged:${packagedLicense}`,
  );
}

const tabsManifest = await readJson(
  await resolvePackageJson("@radix-ui/react-tabs", appRequire),
);
checkValue("installed Radix Tabs", tabsManifest.version, expected.radixTabs);
checkValue("Radix Tabs package license", tabsManifest.license, "MIT");
for (const peer of ["react", "react-dom"]) {
  const range = tabsManifest.peerDependencies?.[peer];
  if (!supportsReact19(range)) {
    issues.push(
      `@radix-ui/react-tabs ${tabsManifest.version} peer ${peer}=${String(range)} excludes React 19.2.7.`,
    );
  }
}

const packagedFiles = await listRelativeFiles(dirname(enginePath));

const expectedFontFamilies = [
  "Assistant",
  "Cascadia",
  "ComicShanns",
  "Excalifont",
  "Liberation",
  "Lilita",
  "Nunito",
  "Virgil",
  "Xiaolai",
];
const missingFonts = expectedFontFamilies.filter(
  (family) =>
    !packagedFiles.some((path) =>
      path.replaceAll("\\", "/").includes(`/fonts/${family}/`),
    ),
);
if (missingFonts.length > 0) {
  issues.push(`Expected font assets are missing: ${missingFonts.join(", ")}.`);
}
const noticePath = join(spikeRoot, "public", "THIRD_PARTY_NOTICES.txt");
let notice = "";
try {
  notice = await readFile(noticePath, "utf8");
} catch {
  issues.push("Candidate THIRD_PARTY_NOTICES.txt is missing.");
}
for (const marker of [
  "a2ec2889babf7d2295469c6d90ebe77fae57df84",
  "@radix-ui/react-tabs@1.1.21",
  "fuzzy (LICENSE-MIT)",
  "MIT License text",
  "SIL Open Font License 1.1",
  ...expectedFontFamilies,
]) {
  if (!notice.includes(marker)) {
    issues.push(`Candidate notice is missing required marker: ${marker}.`);
  }
}

console.log(
  `PASS exact pins: Excalidraw ${expected.engine}, React ${expected.react}, ReactDOM ${expected.reactDom}.`,
);
console.log(
  `PASS engine metadata: MIT and React 19 declared by @excalidraw/excalidraw.`,
);
console.log(
  `AUDIT direct Excalidraw dependencies: ${transitiveLicenses.size} package manifests checked.`,
);
console.log(
  `AUDIT packaged assets: ${expectedFontFamilies.length - missingFonts.length} font families found; candidate notice verified.`,
);

if (issues.length > 0) {
  for (const issue of [...new Set(issues)]) {
    console.error(`BLOCK ${issue}`);
  }
  console.error(
    `Excalidraw dependency/license gate blocked (${issues.length} findings).`,
  );
  process.exitCode = 1;
} else {
  console.log("Excalidraw dependency/license gate passed.");
}

function checkExact(name, dependencies, version) {
  const actual = dependencies?.[name];
  if (actual !== version) {
    issues.push(
      `${name} must be pinned exactly to ${version}; found ${String(actual)}.`,
    );
  }
}

function checkValue(label, actual, expectedValue) {
  if (actual !== expectedValue) {
    issues.push(`${label} must be ${expectedValue}; found ${String(actual)}.`);
  }
}

function supportsReact19(range) {
  return typeof range === "string" && /(?:\^|>=)\s*19(?:\.0\.0)?\b/.test(range);
}

async function readJson(path) {
  return JSON.parse(await readFile(path, "utf8"));
}

async function resolvePackageJson(name, scopedRequire) {
  try {
    return scopedRequire.resolve(`${name}/package.json`);
  } catch {
    const entry = scopedRequire.resolve(name);
    let directory = dirname(entry);
    while (directory !== dirname(directory)) {
      const candidate = join(directory, "package.json");
      try {
        const manifest = await readJson(candidate);
        if (manifest.name === name) return candidate;
      } catch {
        // Keep walking toward the package root.
      }
      directory = dirname(directory);
    }
    throw new Error(`Unable to resolve package manifest for ${name}.`);
  }
}

async function listRelativeFiles(directory, prefix = "") {
  const entries = await readdir(join(directory, prefix), {
    withFileTypes: true,
  });
  const files = [];
  for (const entry of entries) {
    const relative = join(prefix, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listRelativeFiles(directory, relative)));
    } else {
      files.push(relative.replaceAll("\\", "/"));
    }
  }
  return files;
}
