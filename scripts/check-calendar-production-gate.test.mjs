import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  checkCalendarProductionGate,
  readNVDAGateStatus,
} from "./check-calendar-production-gate.mjs";

const pendingEvidence = `# Calendar evidence

### \`PENDING_NVDA_REVIEW\`

\`\`\`text
Result: PASS / FAIL
\`\`\`

## Security notes
`;

function passedEvidence() {
  return pendingEvidence.replace("Result: PASS / FAIL", "Result: PASS");
}

async function createRepository({
  evidence = pendingEvidence,
  packageJson = { dependencies: { "temporal-polyfill": "1.0.1" } },
  source = 'export const calendarShell = "agenda";',
} = {}) {
  const root = await mkdtemp(join(tmpdir(), "tutorhub-calendar-production-"));
  await mkdir(join(root, "docs", "calendar"), { recursive: true });
  await mkdir(join(root, "apps", "web", "src"), { recursive: true });
  await writeFile(
    join(root, "docs", "calendar", "P3_CAL_01_SPIKE_EVIDENCE.md"),
    evidence,
    "utf8",
  );
  await writeFile(
    join(root, "apps", "web", "package.json"),
    JSON.stringify(packageJson),
    "utf8",
  );
  await writeFile(join(root, "apps", "web", "src", "main.tsx"), source, "utf8");
  return root;
}

async function withRepository(options, assertion) {
  const root = await createRepository(options);
  try {
    await assertion(await checkCalendarProductionGate(root));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

test("parses pending, pass, and fail without accepting the template", () => {
  assert.equal(readNVDAGateStatus(pendingEvidence), "pending");
  assert.equal(readNVDAGateStatus(passedEvidence()), "pass");
  assert.equal(
    readNVDAGateStatus(
      pendingEvidence.replace("Result: PASS / FAIL", "Result: FAIL"),
    ),
    "fail",
  );
});

test("allows a Calendar shell without FullCalendar while NVDA is pending", async () => {
  await withRepository({}, (result) => {
    assert.equal(result.gateStatus, "pending");
    assert.deepEqual(result.issues, []);
  });
});

test("blocks a FullCalendar package while NVDA is pending", async () => {
  await withRepository(
    {
      packageJson: {
        dependencies: {
          "@fullcalendar/react": "7.0.1",
          "temporal-polyfill": "1.0.1",
        },
      },
    },
    (result) => {
      assert.match(
        result.issues.join("\n"),
        /blocked while NVDA gate is pending/,
      );
    },
  );
});

test("blocks a FullCalendar source import while NVDA is pending", async () => {
  await withRepository(
    { source: 'import FullCalendar from "@fullcalendar/react";' },
    (result) => {
      assert.match(
        result.issues.join("\n"),
        /blocked while NVDA gate is pending/,
      );
    },
  );
});

test("allows only exact Standard v7 and Temporal pins after NVDA passes", async () => {
  await withRepository(
    {
      evidence: passedEvidence(),
      packageJson: {
        dependencies: {
          "@fullcalendar/react": "7.0.1",
          "temporal-polyfill": "1.0.1",
        },
      },
      source: [
        'import FullCalendar from "@fullcalendar/react";',
        'import dayGridPlugin from "@fullcalendar/react/daygrid";',
        'import "@fullcalendar/react/skeleton.css";',
      ].join("\n"),
    },
    (result) => assert.deepEqual(result.issues, []),
  );
});

test("rejects non-exact production versions after NVDA passes", async () => {
  await withRepository(
    {
      evidence: passedEvidence(),
      packageJson: {
        dependencies: {
          "@fullcalendar/react": "^7.0.1",
          "temporal-polyfill": "^1.0.1",
        },
      },
      source: 'import FullCalendar from "@fullcalendar/react";',
    },
    (result) => {
      const issues = result.issues.join("\n");
      assert.match(
        issues,
        /@fullcalendar\/react must be pinned exactly to 7\.0\.1/,
      );
      assert.match(
        issues,
        /temporal-polyfill must be pinned exactly to 1\.0\.1/,
      );
    },
  );
});

test("rejects Premium/resource packages, telemetry, and remote assets", async () => {
  await withRepository(
    {
      evidence: passedEvidence(),
      packageJson: {
        dependencies: {
          "@fullcalendar/react": "7.0.1",
          "@fullcalendar/resource-timeline": "7.0.1",
          "@sentry/react": "1.0.0",
          "temporal-polyfill": "1.0.1",
        },
      },
      source: [
        'import FullCalendar from "@fullcalendar/react";',
        'import resource from "@fullcalendar/resource-timeline";',
        'import tracker from "https://example.invalid/calendar.js";',
        'const provider = "posthog";',
        '@import "https://example.invalid/calendar.css";',
      ].join("\n"),
    },
    (result) => {
      const issues = result.issues.join("\n");
      assert.match(issues, /Premium\/resource FullCalendar dependency/);
      assert.match(issues, /direct FullCalendar entrypoint/);
      assert.match(issues, /Telemetry dependency/);
      assert.match(issues, /telemetry reference/);
      assert.match(issues, /remote module import/);
      assert.match(issues, /remote stylesheet or script/);
    },
  );
});

test("fails closed when the NVDA marker is missing", async () => {
  await withRepository(
    { evidence: "# Calendar evidence\n\nNo rollout marker.\n" },
    (result) => {
      assert.equal(result.gateStatus, "missing");
      assert.match(result.issues.join("\n"), /fail closed/);
    },
  );
});
