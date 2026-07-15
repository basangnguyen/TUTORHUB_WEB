import createClient from "openapi-fetch";
import type { components, paths } from "./generated/schema";

export type HealthResponse = components["schemas"]["HealthResponse"];
export type CurrentUser = components["schemas"]["MeResponse"];
export type CSRFResponse = components["schemas"]["CSRFResponse"];
export type LogoutResponse = components["schemas"]["LogoutResponse"];
export type CreateTenantRequest = components["schemas"]["CreateTenantRequest"];
export type SwitchActiveTenantRequest =
  components["schemas"]["SwitchActiveTenantRequest"];
export type ClassroomClass = components["schemas"]["Class"];
export type ClassListResponse = components["schemas"]["ClassListResponse"];
export type CreateClassRequest = components["schemas"]["CreateClassRequest"];
export type MediaTokenResponse = components["schemas"]["MediaTokenResponse"];
export type MediaEventRequest = components["schemas"]["MediaEventRequest"];
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

export async function createTenant(
  input: CreateTenantRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CurrentUser> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/tenants",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: input,
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

export async function switchActiveTenant(
  tenantID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CurrentUser> {
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/session/active-tenant",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: { tenant_id: tenantID },
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

export async function listClasses(
  limit = 50,
  options: APIRequestOptions = {},
): Promise<ClassListResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes",
    {
      params: { query: { limit } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassListResponse>(
    data as ClassListResponse | undefined,
    error,
    response,
  );
}

export async function getClass(
  classID: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}",
    {
      params: { path: { class_id: classID } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function createClass(
  input: CreateClassRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function issueClassMediaToken(
  classID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaTokenResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/media-token",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MediaTokenResponse>(
    data as MediaTokenResponse | undefined,
    error,
    response,
  );
}

export async function recordClassMediaEvent(
  classID: string,
  input: MediaEventRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<void> {
  const { error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/media-events",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      signal: options.signal,
    },
  );

  if (!response.ok) {
    throw new APIRequestError(
      response.status,
      isProblem(error) ? error : undefined,
    );
  }
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
  const normalizedBaseUrl = stripTrailingSlashes(baseUrl);

  try {
    return stripTrailingSlashes(new URL(normalizedBaseUrl).toString());
  } catch {
    const runtimeOrigin = globalThis.location?.origin;
    const origin =
      runtimeOrigin && runtimeOrigin !== "null"
        ? runtimeOrigin
        : "http://localhost";

    return stripTrailingSlashes(
      new URL(normalizedBaseUrl, `${origin}/`).toString(),
    );
  }
}

function stripTrailingSlashes(value: string): string {
  let end = value.length;
  while (end > 0 && value.charCodeAt(end - 1) === 47) {
    end -= 1;
  }

  return value.slice(0, end);
}
