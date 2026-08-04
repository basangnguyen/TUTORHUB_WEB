import assert from "node:assert/strict";
import test from "node:test";

import {
  formatBlockingReport,
  formatGitHubAnnotations,
  summarizeBlockingFindings,
} from "./check-trivy-sarif.mjs";

test("rejects HIGH and CRITICAL results without exposing messages or locations", () => {
  const secret = "DO_NOT_EXPOSE_MATCHED_SECRET";
  const summary = summarizeBlockingFindings({
    runs: [
      {
        tool: {
          driver: {
            rules: [
              { id: "CVE-2026-0001", properties: { tags: ["HIGH"] } },
              {
                id: "AVD-DS-0001",
                properties: { "security-severity": "9.1" },
              },
            ],
          },
        },
        results: [
          {
            ruleId: "CVE-2026-0001",
            ruleIndex: 0,
            level: "error",
            message: { text: secret },
            locations: [
              { physicalLocation: { artifactLocation: { uri: secret } } },
            ],
          },
          {
            ruleId: "AVD-DS-0001",
            ruleIndex: 1,
            level: "error",
            message: { text: secret },
          },
        ],
      },
    ],
  });

  assert.deepEqual(summary, {
    count: 2,
    findings: [
      { id: "AVD-DS-0001", severity: "CRITICAL" },
      { id: "CVE-2026-0001", severity: "HIGH" },
    ],
  });
  assert.doesNotMatch(
    formatBlockingReport(summary, "repository"),
    new RegExp(secret),
  );
  assert.deepEqual(formatGitHubAnnotations(summary, "repository"), [
    "::error title=Trivy_repository_CRITICAL::AVD-DS-0001",
    "::error title=Trivy_repository_HIGH::CVE-2026-0001",
  ]);
});

test("ignores non-blocking SARIF results", () => {
  const summary = summarizeBlockingFindings({
    runs: [
      {
        tool: {
          driver: {
            rules: [
              { id: "LOW-1", properties: { tags: ["LOW"] } },
              {
                id: "MEDIUM-1",
                properties: { "security-severity": "6.9" },
              },
            ],
          },
        },
        results: [
          { ruleIndex: 0, level: "note" },
          { ruleIndex: 1, level: "warning" },
        ],
      },
    ],
  });

  assert.deepEqual(summary, { count: 0, findings: [] });
  assert.match(formatBlockingReport(summary, "container"), /gate passed/);
});

test("uses SARIF error level when a scanner omits severity metadata", () => {
  assert.deepEqual(
    summarizeBlockingFindings({
      runs: [
        {
          tool: { driver: { rules: [{ id: "SECRET-1" }] } },
          results: [{ ruleIndex: 0, level: "error" }],
        },
      ],
    }),
    {
      count: 1,
      findings: [{ id: "SECRET-1", severity: "HIGH" }],
    },
  );
});
