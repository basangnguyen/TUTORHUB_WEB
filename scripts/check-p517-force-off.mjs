import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));

function source(path) {
  return readFileSync(resolve(root, path), "utf8");
}

function requireMatch(path, pattern, description) {
  if (!pattern.test(source(path))) {
    throw new Error(`${description} is missing in ${path}`);
  }
}

const example = source(".env.example");
if (!/^FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=false$/mu.test(example)) {
  throw new Error(".env.example must keep classroom whiteboards force-off");
}
if (/^FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS=true$/mu.test(example)) {
  throw new Error(".env.example must not enable classroom whiteboards");
}

requireMatch(
  "services/core-api/internal/config/config.go",
  /EnableClassroomWhiteboards:\s*boolValue\([\s\S]*?"FEATURE_CONTROL_ENABLE_CLASSROOM_WHITEBOARDS",\s*false,/u,
  "Core API force-off default",
);
requireMatch(
  "services/core-api/cmd/api/main.go",
  /if !configuration\.EnableClassroomWhiteboards \{\s*forcedOff\[featurecontrol\.FeatureClassroomWhiteboards\] = true/u,
  "Core API deployment guardrail",
);
requireMatch(
  "services/core-api/internal/modules/featurecontrol/catalog.go",
  /FeatureClassroomWhiteboards:[\s\S]*?DefaultEnabled:\s*false/u,
  "feature catalog force-off default",
);
requireMatch(
  "apps/web/src/features/collaboration/ClassroomWhiteboardTool.tsx",
  /if \(!enabled\) \{\s*return <WhiteboardState message=\{t\("whiteboard\.featureOff"\)\} \/>;/u,
  "web force-off state",
);
requireMatch(
  "apps/web/src/features/collaboration/ClassroomWhiteboardTool.tsx",
  /if \(error\.status === 503\) return "featureOff";/u,
  "web 503 fail-closed mapping",
);

process.stdout.write("[P5-COLLAB-17] static force-off guard PASS\n");
