const expected = "I_UNDERSTAND_P4_08_DISPOSABLE_ONLY";

if (process.env.P4_08_DISPOSABLE_CONFIRM !== expected) {
  console.error(
    "P4-08 PostgreSQL integration requires the exact disposable-only confirmation.",
  );
  process.exit(2);
}

for (const name of [
  "P4_08_OWNER_PREFLIGHT",
  "P4_08_FINAL_SNAPSHOT_CONFIRM",
  "P4_08_SHARED_CONFIRM",
]) {
  if ((process.env[name] ?? "").trim() !== "") {
    console.error(`P4-08 disposable integration refuses confirmation ${name}.`);
    process.exit(2);
  }
}
