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

const envExample = source(".env.example");
if (
  !/^FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false$/mu.test(envExample)
) {
  throw new Error(
    ".env.example: classroom whiteboards must remain globally disabled by default",
  );
}
if (/^FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=true$/mu.test(envExample)) {
  throw new Error(
    ".env.example: classroom whiteboards must not be globally enabled by default",
  );
}
if (
  !/^FEATURE_CONTROL_CLASSROOM_WHITEBOARD_CANARY_TENANT_IDS=$/mu.test(
    envExample,
  )
) {
  throw new Error(
    ".env.example: canary tenant allowlist must exist and be empty by default",
  );
}

requireMatch(
  "services/core-api/internal/config/config.go",
  /uuidListValue\(\s*lookup,\s*"FEATURE_CONTROL_CLASSROOM_WHITEBOARD_CANARY_TENANT_IDS"/u,
  "canary tenant UUID allowlist parsing",
);
requireMatch(
  "services/core-api/internal/config/config.go",
  /EnableClassroomWhiteboards\s*&&\s*len\(configuration\.ClassroomWhiteboardCanaryTenantIDs\)\s*!=\s*1/u,
  "fail-closed exact-one validation for an enabled canary deployment",
);
requireMatch(
  "services/core-api/cmd/api/main.go",
  /TenantAllowlists:\s*map\[featurecontrol\.FeatureKey\]\[\]uuid\.UUID\{[\s\S]*?featurecontrol\.FeatureClassroomWhiteboards:[\s\S]*?configuration\.ClassroomWhiteboardCanaryTenantIDs\.\.\./u,
  "whiteboard canary allowlist wiring",
);
requireMatch(
  "services/core-api/cmd/api/main.go",
  /if len\(configuration\.ClassroomWhiteboardCanaryTenantIDs\)\s*==\s*1\s*\{[\s\S]*?p518InternalCanaryQuotaCeilings\(/u,
  "exact-one tenant quota guardrail wiring",
);
requireMatch(
  "services/core-api/cmd/api/main.go",
  /TenantQuotaCeilings:\s*tenantQuotaCeilings/u,
  "tenant quota ceilings catalog binding",
);
requireMatch(
  "services/core-api/cmd/api/main.go",
  /func p518InternalCanaryQuotaCeilings\([\s\S]*?QuotaWhiteboardDocumentsPerTenant:[\s\S]*?tenantID:\s*2[\s\S]*?QuotaWhiteboardConnectionsPerTenant:[\s\S]*?tenantID:\s*10[\s\S]*?QuotaWhiteboardStorageBytesPerTenant:[\s\S]*?tenantID:\s*64\s*\*\s*1024\s*\*\s*1024[\s\S]*?QuotaWhiteboardOperationsPerMinute:[\s\S]*?tenantID:\s*600/u,
  "exact P5-COLLAB-18 quota profile",
);
requireMatch(
  "services/core-api/internal/modules/featurecontrol/catalog.go",
  /func \(catalog \*Catalog\) EvaluateFeatureForTenant\([\s\S]*?func \(catalog \*Catalog\) evaluateFeature\([\s\S]*?if allowlist, guarded := catalog\.tenantAllowlists\[key\]; guarded \{[\s\S]*?if tenantOverride != nil \{/u,
  "tenant allowlist enforcement before tenant overrides",
);

const repositorySource = source(
  "services/core-api/internal/modules/featurecontrol/postgres_repository.go",
);
const tenantEvaluationCalls =
  repositorySource.match(/catalog\.EvaluateFeatureForTenant\(/gu) ?? [];
if (tenantEvaluationCalls.length < 2) {
  throw new Error(
    "services/core-api/internal/modules/featurecontrol/postgres_repository.go: effective feature reads must use tenant-aware evaluation",
  );
}
const tenantQuotaEvaluationCalls =
  repositorySource.match(/catalog\.EvaluateQuotaForTenant\(/gu) ?? [];
if (tenantQuotaEvaluationCalls.length < 2) {
  throw new Error(
    "services/core-api/internal/modules/featurecontrol/postgres_repository.go: effective quota reads must use tenant-aware evaluation",
  );
}

requireMatch(
  "apps/web/src/pages/MediaSpaceRoomPage.tsx",
  /useTenantCapabilities\(\s*tenantId,\s*Boolean\(tenantId\),?\s*\)/u,
  "tenant capability projection",
);
requireMatch(
  "apps/web/src/pages/MediaSpaceRoomPage.tsx",
  /features\.classroom_whiteboards\.enabled\s*===\s*true/u,
  "fail-closed whiteboard capability check",
);
requireMatch(
  "apps/web/src/pages/MediaSpaceRoomPage.tsx",
  /enabled=\{whiteboardEnabled\}/u,
  "whiteboard drawer capability binding",
);

console.log(
  "[P5-COLLAB-18] internal-canary static guard: PASS (global default off, empty allowlist default, exact-one tenant fail-closed wiring, quota 2 docs/10 connections/64 MiB/600 ops per minute, capability-gated UI)",
);
