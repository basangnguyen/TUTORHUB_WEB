import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));

function source(relativePath) {
  return readFileSync(resolve(root, relativePath), "utf8");
}

function requireMatch(relativePath, pattern, description) {
  if (!pattern.test(source(relativePath))) {
    throw new Error(`${relativePath}: missing ${description}`);
  }
}

const example = source(".env.example");
if (
  !/^FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false$/mu.test(example) ||
  !/^FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARD_RAMP=false$/mu.test(example) ||
  !/^FEATURE_CONTROL_CLASSROOM_WHITEBOARD_CANARY_TENANT_IDS=$/mu.test(example)
) {
  throw new Error(
    ".env.example: whiteboard and exact-two ramp must remain disabled with an empty allowlist",
  );
}

requireMatch(
  "services/core-api/internal/config/config.go",
  /EnableClassroomWhiteboardRamp:\s*boolValue\([\s\S]*?"FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARD_RAMP",\s*false,/u,
  "default-off P5-COLLAB-20 ramp flag",
);
requireMatch(
  "services/core-api/internal/config/config.go",
  /EnableClassroomWhiteboards\s*&&[\s\S]*?EnableClassroomWhiteboardRamp\s*&&[\s\S]*?len\(configuration\.ClassroomWhiteboardCanaryTenantIDs\)\s*!=\s*2/u,
  "exact-two authorized ramp validation",
);
requireMatch(
  "services/core-api/internal/config/config.go",
  /EnableClassroomWhiteboardRamp\s*&&\s*!configuration\.EnableClassroomWhiteboards/u,
  "ramp-without-whiteboard rejection",
);
requireMatch(
  "services/core-api/cmd/api/main.go",
  /EnableClassroomWhiteboardRamp\s*&&[\s\S]*?len\(configuration\.ClassroomWhiteboardCanaryTenantIDs\)\s*==\s*2[\s\S]*?p520RampQuotaCeilings/u,
  "exact-two ramp quota wiring",
);
requireMatch(
  "services/core-api/cmd/api/main.go",
  /func p520RampQuotaCeilings\([\s\S]*?DocumentsPerTenant\]\[tenantID\]\s*=\s*2[\s\S]*?ConnectionsPerTenant\]\[tenantID\]\s*=\s*10[\s\S]*?StorageBytesPerTenant\]\[tenantID\]\s*=\s*64\s*\*\s*1024\s*\*\s*1024[\s\S]*?OperationsPerMinute\]\[tenantID\]\s*=\s*600/u,
  "low-quota profile for every ramp tenant",
);

console.log(
  "[P5-COLLAB-20] exact-two ramp static guard: PASS (default off, explicit ramp flag, two tenants, low quotas)",
);
