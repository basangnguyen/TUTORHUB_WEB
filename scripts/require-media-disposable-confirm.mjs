const requiredConfirmations = new Map([
  ["P4_02_DISPOSABLE_CONFIRM", "I_UNDERSTAND_P4_02_DISPOSABLE_ONLY"],
  ["P4_04_DISPOSABLE_CONFIRM", "I_UNDERSTAND_P4_04_DISPOSABLE_ONLY"],
  [
    "P4_04_ACL_PROVISION_CONFIRM",
    "I_UNDERSTAND_P4_04_ACL_PROVISION_DISPOSABLE_ONLY",
  ],
  ["P4_06_DISPOSABLE_CONFIRM", "I_UNDERSTAND_P4_06_DISPOSABLE_ONLY"],
  [
    "P4_06_ACL_PROVISION_CONFIRM",
    "I_UNDERSTAND_P4_06_ACL_PROVISION_DISPOSABLE_ONLY",
  ],
]);

const invalidConfirmations = [...requiredConfirmations].flatMap(
  ([name, expected]) => (process.env[name] === expected ? [] : [name]),
);

if (invalidConfirmations.length > 0) {
  console.error(
    `Media PostgreSQL integration requires exact disposable confirmations: ${invalidConfirmations.join(", ")}.`,
  );
  process.exit(2);
}
