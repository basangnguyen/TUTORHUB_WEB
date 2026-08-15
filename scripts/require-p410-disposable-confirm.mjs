const requiredConfirmations = new Map([
  ["P4_10_DISPOSABLE_CONFIRM", "I_UNDERSTAND_P4_10_DISPOSABLE_ONLY"],
  [
    "P4_10_ACL_PROVISION_CONFIRM",
    "I_UNDERSTAND_P4_10_ACL_PROVISION_DISPOSABLE_ONLY",
  ],
  [
    "P4_10_FINAL_SNAPSHOT_CONFIRM",
    "I_UNDERSTAND_P4_10_FINAL_SNAPSHOT_READ_ONLY",
  ],
]);

const invalidConfirmations = [...requiredConfirmations].flatMap(
  ([name, expected]) => (process.env[name] === expected ? [] : [name]),
);

if (invalidConfirmations.length > 0) {
  console.error(
    `P4-10 PostgreSQL integration requires exact disposable confirmations: ${invalidConfirmations.join(", ")}.`,
  );
  process.exit(2);
}
