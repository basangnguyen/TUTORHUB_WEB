const expectedConfirmation = "I_UNDERSTAND_P4_11_ISOLATED_LIVEKIT_LOAD";
const allowedProfiles = new Set(["25", "50"]);
const conflictingEnvironment = [
  "DATABASE_MIGRATION_URL",
  "DATABASE_POOL_URL",
  "DATABASE_POLL_MAINTENANCE_URL",
  "P4_10_DISPOSABLE_CONFIRM",
  "P4_10_OWNER_PREFLIGHT",
  "P4_10_ACL_PROVISION_CONFIRM",
  "P4_10_SHARED_CONFIRM",
  "P4_10_SHARED_ACL_PROVISION_CONFIRM",
  "P4_10_SHARED_SNAPSHOT_CONFIRM",
];

function fail(message) {
  console.error(`P4-11 provider gate refused: ${message}`);
  process.exit(2);
}

const confirmation = process.env.P4_11_PROVIDER_CONFIRM?.trim() ?? "";
if (confirmation !== expectedConfirmation) {
  fail("exact isolated-provider confirmation is missing");
}

const profile = process.env.P4_11_LOAD_PROFILE?.trim() ?? "";
if (!allowedProfiles.has(profile)) {
  fail("P4_11_LOAD_PROFILE must be exactly 25 or 50");
}
if ((process.env.P4_11_SUSTAIN_SECONDS?.trim() ?? "") !== "120") {
  fail("P4_11_SUSTAIN_SECONDS must be exactly 120");
}

const expectedQuotaConfirmation =
  profile === "25"
    ? "I_CONFIRMED_P4_11_PROVIDER_QUOTA_FOR_25"
    : "I_CONFIRMED_P4_11_PROVIDER_QUOTA_FOR_50";
if (
  (process.env.P4_11_PROVIDER_QUOTA_CONFIRM?.trim() ?? "") !==
  expectedQuotaConfirmation
) {
  fail(`exact provider quota confirmation for profile ${profile} is missing`);
}

for (const name of conflictingEnvironment) {
  if ((process.env[name]?.trim() ?? "") !== "") {
    fail("database or stale shared-stage confirmation is present");
  }
}

for (const name of ["LIVEKIT_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"]) {
  if ((process.env[name]?.trim() ?? "") === "") {
    fail(`${name} is missing`);
  }
}
if (
  (process.env.P4_11_CORE_API_HEALTH_CONFIRM?.trim() ?? "") !==
  "I_CONFIRMED_P4_11_READ_ONLY_CORE_API_HEALTH"
) {
  fail("exact read-only Core API health confirmation is missing");
}

let providerUrl;
try {
  providerUrl = new URL(process.env.LIVEKIT_URL.trim());
} catch {
  fail("LIVEKIT_URL is invalid");
}
if (
  providerUrl.protocol !== "wss:" ||
  providerUrl.username !== "" ||
  providerUrl.password !== "" ||
  providerUrl.pathname !== "/" ||
  providerUrl.search !== "" ||
  providerUrl.hash !== ""
) {
  fail("LIVEKIT_URL must be a credential-free secure WebSocket origin");
}

let coreApiUrl;
try {
  coreApiUrl = new URL(process.env.P4_11_CORE_API_BASE_URL?.trim() ?? "");
} catch {
  fail("P4_11_CORE_API_BASE_URL is invalid");
}
if (
  coreApiUrl.protocol !== "https:" ||
  coreApiUrl.username !== "" ||
  coreApiUrl.password !== "" ||
  coreApiUrl.pathname !== "/" ||
  coreApiUrl.search !== "" ||
  coreApiUrl.hash !== ""
) {
  fail("P4_11_CORE_API_BASE_URL must be a credential-free HTTPS origin");
}

console.log(`P4-11 isolated provider gate accepted profile ${profile}.`);
