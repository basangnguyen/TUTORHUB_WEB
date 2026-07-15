import { readdir, readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const ACTION_SHA =
  /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+(?:\/[A-Za-z0-9_./-]+)?@[0-9a-f]{40}$/;

function rootBlockContains(lines, blockName, expectedLine) {
  const start = lines.findIndex((line) => line === `${blockName}:`);
  if (start < 0) {
    return false;
  }

  for (let index = start + 1; index < lines.length; index += 1) {
    const line = lines[index];
    if (line.trim() === "") {
      continue;
    }
    if (!line.startsWith("  ")) {
      break;
    }
    if (line.trim() === expectedLine) {
      return true;
    }
  }

  return false;
}

function extractActionReference(line) {
  const match = line.match(/^\s*-?\s*uses:\s*(.+?)\s*$/);
  if (!match) {
    return null;
  }

  return match[1]
    .replace(/\s+#.*$/, "")
    .replace(/^['"]|['"]$/g, "")
    .trim();
}

function jobBlocks(lines) {
  const jobsStart = lines.findIndex((line) => line === "jobs:");
  if (jobsStart < 0) {
    return [];
  }

  const starts = [];
  for (let index = jobsStart + 1; index < lines.length; index += 1) {
    const match = lines[index].match(/^ {2}([A-Za-z0-9_-]+):\s*$/);
    if (match) {
      starts.push({ name: match[1], start: index });
    }
  }

  return starts.map((entry, index) => ({
    name: entry.name,
    lines: lines.slice(entry.start, starts[index + 1]?.start ?? lines.length),
  }));
}

export function auditWorkflowSource(source, fileName = "workflow.yml") {
  const lines = source.split(/\r?\n/);
  const issues = [];

  if (!rootBlockContains(lines, "permissions", "contents: read")) {
    issues.push(
      `${fileName}: top-level permissions must include contents: read`,
    );
  }
  if (!lines.some((line) => line === "concurrency:")) {
    issues.push(`${fileName}: top-level concurrency is required`);
  }
  if (/^\s*pull_request_target\s*:/m.test(source)) {
    issues.push(`${fileName}: pull_request_target is forbidden`);
  }
  if (/\bwrite-all\b/.test(source)) {
    issues.push(`${fileName}: write-all is forbidden`);
  }

  for (const line of lines) {
    const permission = line.match(/^\s+([a-z-]+):\s*write\s*$/);
    if (permission && permission[1] !== "security-events") {
      issues.push(
        `${fileName}: write permission is not allowed for ${permission[1]}`,
      );
    }
  }

  let checkoutCount = 0;
  for (const line of lines) {
    const actionReference = extractActionReference(line);
    if (!actionReference || actionReference.startsWith("./")) {
      continue;
    }
    if (actionReference.startsWith("docker://")) {
      if (!/@sha256:[0-9a-f]{64}$/.test(actionReference)) {
        issues.push(
          `${fileName}: Docker action must use an immutable digest: ${actionReference}`,
        );
      }
      continue;
    }
    if (!ACTION_SHA.test(actionReference)) {
      issues.push(
        `${fileName}: action must use a full commit SHA: ${actionReference}`,
      );
    }
    if (actionReference.startsWith("actions/checkout@")) {
      checkoutCount += 1;
    }
  }

  const protectedCheckoutCount = lines.filter((line) =>
    /^\s+persist-credentials:\s*false\s*$/.test(line),
  ).length;
  if (protectedCheckoutCount < checkoutCount) {
    issues.push(
      `${fileName}: every checkout step must set persist-credentials: false`,
    );
  }

  for (const job of jobBlocks(lines)) {
    if (
      !job.lines.some((line) => /^\s{4}timeout-minutes:\s*\d+\s*$/.test(line))
    ) {
      issues.push(`${fileName}: job ${job.name} must define timeout-minutes`);
    }
  }

  return issues;
}

export async function auditWorkflowDirectory(directory) {
  const names = (await readdir(directory))
    .filter((name) => name.endsWith(".yml") || name.endsWith(".yaml"))
    .sort();
  const results = [];

  for (const name of names) {
    const source = await readFile(resolve(directory, name), "utf8");
    results.push(...auditWorkflowSource(source, name));
  }

  return { filesChecked: names.length, issues: results };
}

const isMain =
  process.argv[1] &&
  pathToFileURL(resolve(process.argv[1])).href === import.meta.url;
if (isMain) {
  const workflowDirectory = resolve(process.cwd(), ".github", "workflows");
  const result = await auditWorkflowDirectory(workflowDirectory);
  if (result.issues.length > 0) {
    console.error(result.issues.join("\n"));
    process.exitCode = 1;
  } else {
    console.log(
      `GitHub Actions security check passed (${result.filesChecked} workflow files).`,
    );
  }
}
