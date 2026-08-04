import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const blockingSeverities = new Set(["CRITICAL", "HIGH"]);

function safeToken(value, fallback) {
  const token = String(value ?? "")
    .replace(/[^A-Za-z0-9._:/@+-]+/g, "_")
    .slice(0, 160);
  return token || fallback;
}

function severityFromScore(value) {
  const score = Number(value);
  if (!Number.isFinite(score)) {
    return null;
  }
  if (score >= 9) {
    return "CRITICAL";
  }
  if (score >= 7) {
    return "HIGH";
  }
  return null;
}

function normalizedBlockingSeverity(value) {
  const severity = String(value ?? "")
    .trim()
    .toUpperCase();
  return blockingSeverities.has(severity) ? severity : null;
}

function resolveRule(run, result) {
  const rules = run?.tool?.driver?.rules ?? [];
  if (Number.isInteger(result?.ruleIndex) && rules[result.ruleIndex]) {
    return rules[result.ruleIndex];
  }
  return rules.find((rule) => rule?.id === result?.ruleId) ?? null;
}

function blockingSeverity(run, result) {
  const rule = resolveRule(run, result);
  const properties = [result?.properties, rule?.properties].filter(Boolean);

  for (const property of properties) {
    for (const candidate of [property.severity, property.Severity]) {
      const severity = normalizedBlockingSeverity(candidate);
      if (severity) {
        return severity;
      }
    }
    for (const candidate of [
      property["security-severity"],
      property.securitySeverity,
    ]) {
      const severity = severityFromScore(candidate);
      if (severity) {
        return severity;
      }
    }
    for (const tag of Array.isArray(property.tags) ? property.tags : []) {
      const severity = normalizedBlockingSeverity(tag);
      if (severity) {
        return severity;
      }
    }
  }

  // Trivy maps HIGH/CRITICAL findings to SARIF error level. This fallback
  // covers scanners that omit the numeric score and severity tag.
  return result?.level === "error" ? "HIGH" : null;
}

export function summarizeBlockingFindings(sarif) {
  const findings = [];

  for (const run of Array.isArray(sarif?.runs) ? sarif.runs : []) {
    for (const result of Array.isArray(run?.results) ? run.results : []) {
      const severity = blockingSeverity(run, result);
      if (!severity) {
        continue;
      }
      const rule = resolveRule(run, result);
      findings.push({
        id: safeToken(result?.ruleId ?? rule?.id, "TRIVY-FINDING"),
        severity,
      });
    }
  }

  const unique = new Map();
  for (const finding of findings) {
    unique.set(`${finding.severity}:${finding.id}`, finding);
  }

  return {
    count: findings.length,
    findings: [...unique.values()].sort((left, right) =>
      `${left.severity}:${left.id}`.localeCompare(
        `${right.severity}:${right.id}`,
      ),
    ),
  };
}

export function formatBlockingReport(summary, label = "scan") {
  const safeLabel = safeToken(label, "scan");
  if (summary.count === 0) {
    return `Trivy ${safeLabel} gate passed: no HIGH/CRITICAL findings.`;
  }

  const shown = summary.findings
    .slice(0, 20)
    .map((finding) => `${finding.id}[${finding.severity}]`)
    .join(", ");
  const remainder = Math.max(summary.findings.length - 20, 0);
  const suffix = remainder > 0 ? `, +${remainder}_more_rule_ids` : "";
  return `Trivy ${safeLabel} gate rejected ${summary.count} HIGH/CRITICAL result(s): ${shown}${suffix}`;
}

export function formatGitHubAnnotations(summary, label = "scan") {
  const safeLabel = safeToken(label, "scan");
  return summary.findings
    .slice(0, 20)
    .map(
      (finding) =>
        `::error title=Trivy_${safeLabel}_${finding.severity}::${finding.id}`,
    );
}

export async function auditTrivySarif(filePath) {
  const source = await readFile(filePath, "utf8");
  return summarizeBlockingFindings(JSON.parse(source));
}

const isMain =
  process.argv[1] &&
  pathToFileURL(resolve(process.argv[1])).href === import.meta.url;
if (isMain) {
  const filePath = process.argv[2];
  const label = process.argv[3] ?? "scan";
  if (!filePath) {
    console.error("Trivy SARIF gate requires a report path.");
    process.exitCode = 2;
  } else {
    try {
      const summary = await auditTrivySarif(resolve(process.cwd(), filePath));
      const report = formatBlockingReport(summary, label);
      if (summary.count > 0) {
        if (process.env.GITHUB_ACTIONS === "true") {
          for (const annotation of formatGitHubAnnotations(summary, label)) {
            console.error(annotation);
          }
        }
        console.error(report);
        process.exitCode = 1;
      } else {
        console.log(report);
      }
    } catch {
      console.error("Trivy SARIF gate could not read a valid report.");
      process.exitCode = 2;
    }
  }
}
