import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { buildLocalEnvironment, parseArguments } from "./local-dev.mjs";

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);

test("local environment always targets loopback services", () => {
  const environment = buildLocalEnvironment({
    DATABASE_POOL_URL: "postgresql://production.example.invalid/tutorhub",
    B2_APPLICATION_KEY: "must-not-survive",
    LIVEKIT_API_SECRET: "must-not-survive",
  });

  assert.match(environment.DATABASE_POOL_URL, /localhost:5432\/tutorhub/);
  assert.equal(
    environment.DATABASE_POOL_URL,
    environment.DATABASE_MIGRATION_URL,
  );
  assert.equal(environment.REDIS_URL, "redis://localhost:6379/0");
  assert.equal(environment.APP_ENV, "development");
  assert.equal(environment.B2_APPLICATION_KEY, "");
  assert.equal(environment.LIVEKIT_API_SECRET, "");
  assert.equal(environment.OIDC_CLIENT_SECRET, "");
});

test("reset requires explicit destructive confirmation", () => {
  assert.throws(() => parseArguments(["reset"]), /Re-run with --yes/);
  assert.deepEqual(parseArguments(["reset", "--yes"]), { command: "reset" });
  assert.deepEqual(parseArguments(["reset", "--", "--yes"]), {
    command: "reset",
  });
});

test("unknown commands fail instead of being ignored", () => {
  assert.throws(
    () => parseArguments(["typo"]),
    /Unknown local environment command/,
  );
  assert.deepEqual(parseArguments([]), { command: "help" });
});

test("compose and example environment keep local services consistent", async () => {
  const [compose, exampleEnvironment] = await Promise.all([
    readFile(path.join(repositoryRoot, "compose.yaml"), "utf8"),
    readFile(path.join(repositoryRoot, ".env.example"), "utf8"),
  ]);

  assert.match(compose, /postgres:17-alpine/);
  assert.match(compose, /redis:7\.4-alpine/);
  assert.match(compose, /127\.0\.0\.1:5432:5432/);
  assert.match(compose, /127\.0\.0\.1:6379:6379/);
  assert.match(
    exampleEnvironment,
    /DATABASE_POOL_URL=.*localhost:5432\/tutorhub/,
  );
  assert.match(exampleEnvironment, /REDIS_URL=redis:\/\/localhost:6379\/0/);
});
