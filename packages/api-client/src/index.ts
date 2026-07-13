export interface HealthResponse {
  status: "ok";
  service: string;
  environment: string;
  timestamp: string;
}

export interface HealthRequestOptions {
  baseUrl?: string;
  signal?: AbortSignal;
}

export async function getHealth(
  options: HealthRequestOptions = {},
): Promise<HealthResponse> {
  const response = await fetch(`${options.baseUrl ?? "/api"}/health`, {
    headers: { Accept: "application/json" },
    signal: options.signal,
  });

  if (!response.ok) {
    throw new Error(`Core API phản hồi HTTP ${response.status}.`);
  }

  return (await response.json()) as HealthResponse;
}
