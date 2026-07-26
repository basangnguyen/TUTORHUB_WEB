import { existsSync } from "node:fs";
import { readFile, readdir } from "node:fs/promises";
import { extname, join, relative, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const EXACT_VERSIONS = new Map([
  ["@fullcalendar/react", "7.0.1"],
  ["temporal-polyfill", "1.0.1"],
]);

const SOURCE_EXTENSIONS = new Set([
  ".css",
  ".html",
  ".js",
  ".jsx",
  ".mjs",
  ".ts",
  ".tsx",
]);
const FORBIDDEN_FULLCALENDAR_FEATURE =
  /(?:^|[/_-])(?:premium|resource|scheduler|timeline)(?:$|[/_.-])/i;
const FORBIDDEN_TELEMETRY =
  /\b(?:segment|amplitude|mixpanel|google-analytics|posthog|sentry)\b/i;
const REMOTE_STYLE_OR_SCRIPT = [
  /@import\s+(?:url\(\s*)?["']?https?:\/\//i,
  /<script\b[^>]*\bsrc\s*=\s*["']https?:\/\//i,
  /<link\b(?=[^>]*\brel\s*=\s*["']stylesheet["'])[^>]*\bhref\s*=\s*["']https?:\/\//i,
];

async function listSourceFiles(directory) {
  if (!existsSync(directory)) {
    return [];
  }
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listSourceFiles(path)));
    } else if (SOURCE_EXTENSIONS.has(extname(entry.name).toLowerCase())) {
      files.push(path);
    }
  }
  return files.sort();
}

function moduleSpecifiers(source) {
  const specifiers = [];
  const patterns = [
    /\b(?:import|export)\s+(?:type\s+)?(?:[^"';()]*?\s+from\s+)?["']([^"']+)["']/g,
    /\bimport\s*\(\s*["']([^"']+)["']/g,
    /\brequire\s*\(\s*["']([^"']+)["']/g,
    /@import\s+(?:url\(\s*)?["']([^"']+)["']/g,
  ];
  for (const pattern of patterns) {
    for (const match of source.matchAll(pattern)) {
      specifiers.push(match[1]);
    }
  }
  return specifiers;
}

export function readNVDAGateStatus(evidence) {
  const marker = /^###\s+`?PENDING_NVDA_REVIEW`?\s*$/m.exec(evidence);
  if (!marker) {
    return "missing";
  }
  const remainder = evidence.slice(marker.index + marker[0].length);
  const nextHeading = /^#{1,3}\s+/m.exec(remainder);
  const section =
    nextHeading === null ? remainder : remainder.slice(0, nextHeading.index);
  const results = [...section.matchAll(/^Result:\s*(.+?)\s*$/gim)].map(
    (match) => match[1].trim().toUpperCase(),
  );
  if (results.includes("FAIL")) {
    return "fail";
  }
  if (results.includes("PASS")) {
    return "pass";
  }
  return "pending";
}

function allDependencies(packageJson) {
  return {
    ...(packageJson.dependencies ?? {}),
    ...(packageJson.devDependencies ?? {}),
    ...(packageJson.optionalDependencies ?? {}),
    ...(packageJson.peerDependencies ?? {}),
  };
}

function isFullCalendarSpecifier(specifier) {
  return (
    specifier === "fullcalendar" ||
    specifier.startsWith("fullcalendar/") ||
    specifier.startsWith("@fullcalendar/")
  );
}

function isAllowedStandardSpecifier(specifier) {
  return (
    specifier === "@fullcalendar/react" ||
    specifier.startsWith("@fullcalendar/react/")
  );
}

export async function checkCalendarProductionGate(repositoryRoot) {
  const root = resolve(repositoryRoot);
  const evidencePath = resolve(
    root,
    "docs",
    "calendar",
    "P3_CAL_01_SPIKE_EVIDENCE.md",
  );
  const webRoot = resolve(root, "apps", "web");
  const packagePath = resolve(webRoot, "package.json");
  const issues = [];

  let evidence;
  try {
    evidence = await readFile(evidencePath, "utf8");
  } catch {
    return {
      filesChecked: 0,
      gateStatus: "missing",
      issues: [
        "NVDA production gate evidence is missing: docs/calendar/P3_CAL_01_SPIKE_EVIDENCE.md",
      ],
    };
  }
  const gateStatus = readNVDAGateStatus(evidence);
  if (gateStatus === "missing") {
    issues.push(
      "NVDA production gate marker PENDING_NVDA_REVIEW is missing; fail closed.",
    );
  }

  let packageJson;
  try {
    packageJson = JSON.parse(await readFile(packagePath, "utf8"));
  } catch {
    return {
      filesChecked: 0,
      gateStatus,
      issues: [
        ...issues,
        "apps/web/package.json is required and must be valid JSON.",
      ],
    };
  }
  const dependencies = allDependencies(packageJson);
  const dependencyNames = Object.keys(dependencies);
  const fullCalendarDependencies = dependencyNames.filter((name) =>
    isFullCalendarSpecifier(name),
  );

  const sourceFiles = await listSourceFiles(resolve(webRoot, "src"));
  const indexPath = resolve(webRoot, "index.html");
  if (existsSync(indexPath)) {
    sourceFiles.push(indexPath);
  }
  sourceFiles.sort();

  const fullCalendarImports = [];
  for (const file of sourceFiles) {
    const source = await readFile(file, "utf8");
    const fileLabel = relative(root, file).replaceAll("\\", "/");
    const specifiers = moduleSpecifiers(source);
    for (const specifier of specifiers) {
      if (isFullCalendarSpecifier(specifier)) {
        fullCalendarImports.push({ file: fileLabel, specifier });
      }
      if (/^https?:\/\//i.test(specifier)) {
        issues.push(
          `${fileLabel}: remote module import is forbidden: ${specifier}`,
        );
      }
    }
    if (FORBIDDEN_TELEMETRY.test(source)) {
      issues.push(`${fileLabel}: unreviewed telemetry reference is forbidden.`);
    }
    if (REMOTE_STYLE_OR_SCRIPT.some((pattern) => pattern.test(source))) {
      issues.push(`${fileLabel}: remote stylesheet or script is forbidden.`);
    }
  }

  const hasFullCalendar =
    fullCalendarDependencies.length > 0 || fullCalendarImports.length > 0;
  if (gateStatus !== "pass" && hasFullCalendar) {
    issues.push(
      `FullCalendar production integration is blocked while NVDA gate is ${gateStatus}.`,
    );
  }

  if (gateStatus === "pass" && hasFullCalendar) {
    for (const [name, version] of EXACT_VERSIONS) {
      if (dependencies[name] !== version) {
        issues.push(`${name} must be pinned exactly to ${version}.`);
      }
    }
  }

  if (
    gateStatus === "pass" &&
    dependencies["temporal-polyfill"] !== undefined &&
    dependencies["temporal-polyfill"] !==
      EXACT_VERSIONS.get("temporal-polyfill")
  ) {
    issues.push("temporal-polyfill must be pinned exactly to 1.0.1.");
  }

  for (const name of dependencyNames) {
    if (FORBIDDEN_TELEMETRY.test(name)) {
      issues.push(`Telemetry dependency is forbidden in apps/web: ${name}`);
    }
    if (!isFullCalendarSpecifier(name)) {
      continue;
    }
    if (
      name !== "@fullcalendar/react" ||
      FORBIDDEN_FULLCALENDAR_FEATURE.test(name)
    ) {
      issues.push(
        `Premium/resource FullCalendar dependency is forbidden: ${name}`,
      );
    }
  }

  for (const entry of fullCalendarImports) {
    if (
      !isAllowedStandardSpecifier(entry.specifier) ||
      FORBIDDEN_FULLCALENDAR_FEATURE.test(entry.specifier)
    ) {
      issues.push(
        `${entry.file}: Premium/resource or direct FullCalendar entrypoint is forbidden: ${entry.specifier}`,
      );
    }
  }

  return {
    filesChecked: sourceFiles.length,
    gateStatus,
    issues: [...new Set(issues)],
  };
}

const isMain =
  process.argv[1] &&
  pathToFileURL(resolve(process.argv[1])).href === import.meta.url;

if (isMain) {
  const result = await checkCalendarProductionGate(process.cwd());
  if (result.issues.length > 0) {
    console.error(result.issues.join("\n"));
    process.exitCode = 1;
  } else {
    console.log(
      `Calendar production gate passed (${result.gateStatus}; ${result.filesChecked} web source files).`,
    );
  }
}
