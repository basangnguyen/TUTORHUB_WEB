import { spawnSync } from "node:child_process";

const isGitRepository =
  spawnSync("git", ["rev-parse", "--git-dir"], {
    stdio: "ignore",
  }).status === 0;

if (isGitRepository) {
  const result = spawnSync("git", ["config", "core.hooksPath", ".githooks"], {
    stdio: "inherit",
  });

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
