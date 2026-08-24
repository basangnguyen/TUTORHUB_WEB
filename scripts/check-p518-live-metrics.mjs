const RUNTIME_URL = "https://tutorhub-p5-f3-runtime-bs-20260819.onrender.com";
const REQUIRED_DEPENDENCIES = [
  "authority_guard",
  "control_plane",
  "persistence",
  "snapshot",
];

function metricSamples(text, metricName) {
  return text
    .split("\n")
    .filter((line) => line.startsWith(metricName))
    .map((line) => {
      const match = line.match(
        /^(\S+)\s+(-?(?:\d+\.?\d*|\.\d+)(?:e[+-]?\d+)?)$/iu,
      );
      if (!match) return undefined;
      return { labels: match[1], value: Number(match[2]) };
    })
    .filter(Boolean);
}

function sum(text, metricName) {
  return metricSamples(text, metricName).reduce(
    (total, sample) => total + sample.value,
    0,
  );
}

async function main() {
  const token = process.env.COLLAB_METRICS_TOKEN?.trim();
  if (!token) throw new Error("COLLAB_METRICS_TOKEN is required");

  const response = await fetch(`${RUNTIME_URL}/metrics`, {
    headers: { Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok) throw new Error("metrics request failed");
  const text = await response.text();

  for (const dependency of REQUIRED_DEPENDENCIES) {
    const sample = metricSamples(text, "collab_dependency_up").find((entry) =>
      entry.labels.includes(`dependency="${dependency}"`),
    );
    if (!sample || sample.value !== 1) {
      throw new Error("runtime dependency is not ready");
    }
  }

  const connections = sum(text, "collab_connections_current");
  const documents = sum(text, "collab_documents_current");
  const dirtyDocuments = sum(text, "collab_dirty_documents");
  const drainActive = sum(text, "collab_drain_active");
  if (
    connections !== 0 ||
    documents !== 0 ||
    dirtyDocuments !== 0 ||
    drainActive !== 0
  ) {
    throw new Error("runtime cleanup is not zero");
  }

  process.stdout.write(
    "P5-COLLAB-18 live metrics: PASS (dependencies=4, connections=0, documents=0, dirty=0, drain=0).\n",
  );
}

try {
  await main();
} catch {
  process.stderr.write("P5-COLLAB-18 live metrics: FAIL.\n");
  process.exitCode = 1;
}
