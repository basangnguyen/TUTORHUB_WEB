import createClient from "openapi-fetch";
import type { components, paths } from "./generated/schema";

export type HealthResponse = components["schemas"]["HealthResponse"];
export type Problem = components["schemas"]["Problem"];

export class APIRequestError extends Error {
  readonly status: number;
  readonly problem?: Problem;

  constructor(status: number, problem?: Problem) {
    super(
      problem?.detail ?? problem?.title ?? `Core API phản hồi HTTP ${status}.`,
    );
    this.name = "APIRequestError";
    this.status = status;
    this.problem = problem;
  }
}

export interface HealthRequestOptions {
  baseUrl?: string;
  signal?: AbortSignal;
  fetch?: (request: Request) => Promise<Response>;
}

export function createTutorHubClient(options: HealthRequestOptions = {}) {
  return createClient<paths>({
    baseUrl: resolveBaseUrl(options.baseUrl ?? "/api"),
    fetch: options.fetch,
  });
}

export async function getHealth(
  options: HealthRequestOptions = {},
): Promise<HealthResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/health",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  if (!response.ok || data === undefined) {
    throw new APIRequestError(
      response.status,
      isProblem(error) ? error : undefined,
    );
  }

  return data;
}

function isProblem(value: unknown): value is Problem {
  if (typeof value !== "object" || value === null) {
    return false;
  }

  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.type === "string" &&
    typeof candidate.title === "string" &&
    typeof candidate.status === "number"
  );
}

function resolveBaseUrl(baseUrl: string): string {
  const normalizedBaseUrl = baseUrl.replace(/\/+$/, "");

  try {
    return new URL(normalizedBaseUrl).toString().replace(/\/$/, "");
  } catch {
    const runtimeOrigin = globalThis.location?.origin;
    const origin =
      runtimeOrigin && runtimeOrigin !== "null"
        ? runtimeOrigin
        : "http://localhost";

    return new URL(normalizedBaseUrl, `${origin}/`)
      .toString()
      .replace(/\/$/, "");
  }
}
