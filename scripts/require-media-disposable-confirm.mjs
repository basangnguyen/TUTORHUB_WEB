const expected = "I_UNDERSTAND_P4_02_DISPOSABLE_ONLY";

if (process.env.P4_02_DISPOSABLE_CONFIRM !== expected) {
  console.error(
    "Media PostgreSQL integration requires explicit disposable-database confirmation.",
  );
  process.exit(2);
}
