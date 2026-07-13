import createClient from "openapi-fetch";
import type { components, paths } from "./generated/schema";

export type HealthResponse = components["schemas"]["HealthResponse"];
export type CurrentUser = components["schemas"]["MeResponse"];
export type CSRFResponse = components["schemas"]["CSRFResponse"];
export type LogoutResponse = components["schemas"]["LogoutResponse"];
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

export interface APIRequestOptions {
  baseUrl?: string;
  signal?: AbortSignal;
  fetch?: (request: Request) => Promise<Response>;
}

export type HealthRequestOptions = APIRequestOptions;

export function createTutorHubClient(options: APIRequestOptions = {}) {
  return createClient<paths>({
    baseUrl: resolveBaseUrl(options.baseUrl ?? "/api"),
    credentials: "include",
    fetch: options.fetch,
  });
}

export function getLoginURL(
  returnTo = "/app/home",
  options: Pick<APIRequestOptions, "baseUrl"> = {},
): string {
  const baseUrl = resolveBaseUrl(options.baseUrl ?? "/api");
  const loginURL = new URL(`${baseUrl}/api/v1/auth/login`);
  loginURL.searchParams.set("return_to", returnTo);
  return loginURL.toString();
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

export async function getCurrentUser(
  options: APIRequestOptions = {},
): Promise<CurrentUser> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/me",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CurrentUser>(
    data as CurrentUser | undefined,
    error,
    response,
  );
}

export async function rotateCSRFToken(
  options: APIRequestOptions = {},
): Promise<CSRFResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/auth/csrf",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CSRFResponse>(
    data as CSRFResponse | undefined,
    error,
    response,
  );
}

export async function logout(
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<LogoutResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/auth/logout",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<LogoutResponse>(
    data as LogoutResponse | undefined,
    error,
    response,
  );
}

function requireData<T>(
  data: T | undefined,
  error: unknown,
  response: Response,
): T {
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
