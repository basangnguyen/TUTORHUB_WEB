import { readFile, readdir } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const EXACT_ALLOWED = new Map([
  ["@fullcalendar/react", "7.0.1"],
  ["temporal-polyfill", "1.0.1"],
]);

const FORBIDDEN_PACKAGE = /(?:^|\/)(?:@fullcalendar\/(?:premium|scheduler|resource|timeline)|fullcalendar-scheduler|@fullcalendar-[^/]*(?:premium|scheduler|resource|timeline))/i;
const FORBIDDEN_SOURCE = [
  /\b(?:segment|amplitude|mixpanel|google-analytics|posthog|sentry)\b/i,
  /https?:\/\//i,
  /@import\s+url\(/i,
];

async function listSourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listSourceFiles(path)));
    } else if ([".css", ".html", ".js", ".mjs", ".ts", ".tsx"].includes(extname(path))) {
      files.push(path);
    }
  }
  return files;
}

export async function checkCalendarDependencies(packageRoot) {
  const issues = [];
  const packageJsonPath = resolve(packageRoot, "package.json");
  const packageJson = JSON.parse(await readFile(packageJsonPath, "utf8"));
  const allDependencies = {
    ...(packageJson.dependencies ?? {}),
    ...(packageJson.devDependencies ?? {}),
    ...(packageJson.optionalDependencies ?? {}),
  };

  for (const [name, version] of EXACT_ALLOWED) {
    if (allDependencies[name] !== version) {
      issues.push(`${name} must be pinned to ${version}`);
    }
  }

  for (const name of Object.keys(allDependencies)) {
    if (FORBIDDEN_PACKAGE.test(name)) {
      issues.push(`Premium/resource dependency is forbidden: ${name}`);
    }
  }

  const fullCalendarPackages = Object.keys(allDependencies).filter((name) =>
    name.startsWith("@fullcalendar/"),
  );
  for (const name of fullCalendarPackages) {
    if (name !== "@fullcalendar/react") {
      issues.push(
        `FullCalendar v7 entrypoints must come from @fullcalendar/react; remove direct ${name}`,
      );
    }
  }

  const sourceDirectory = resolve(packageRoot, "src");
  for (const file of await listSourceFiles(sourceDirectory)) {
    const source = await readFile(file, "utf8");
    for (const expression of FORBIDDEN_SOURCE) {
      if (expression.test(source)) {
        issues.push(`${file}: contains an unreviewed analytics/remote asset reference`);
        break;
      }
    }
  }

  const rootLockPath = resolve(packageRoot, "..", "..", "pnpm-lock.yaml");
  try {
    const lock = await readFile(rootLockPath, "utf8");
    for (const line of lock.split(/\r?\n/)) {
      const match = line.match(/^\s{2}(\/?@?[^:]+):/);
      if (match && FORBIDDEN_PACKAGE.test(match[1])) {
        issues.push(`Lockfile contains forbidden FullCalendar package: ${match[1]}`);
      }
    }
  } catch {
    // A package-local spike can be checked before the root lock is updated.
  }

  return { issues };
}

const isMain =
  process.argv[1] &&
  pathToFileURL(resolve(process.argv[1])).href === import.meta.url;

if (isMain) {
  const cwdSource = resolve(process.cwd(), "src");
  const packageRoot = existsSync(cwdSource)
    ? resolve(process.cwd())
    : resolve(dirname(fileURLToPath(import.meta.url)), "..");
  const result = await checkCalendarDependencies(packageRoot);
  if (result.issues.length > 0) {
    console.error(result.issues.join("\n"));
    process.exitCode = 1;
  } else {
    console.log("Calendar dependency guard passed.");
  }
}
