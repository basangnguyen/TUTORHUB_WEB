interface Env {
  CORE_API_ORIGIN: string;
}

interface FunctionContext {
  request: Request;
  env: Env;
}

function errorResponse(status: number, title: string): Response {
  return new Response(
    JSON.stringify({ title, status }),
    {
      status,
      headers: {
        "content-type": "application/problem+json; charset=utf-8",
        "cache-control": "no-store",
      },
    },
  );
}

export async function onRequest(
  context: FunctionContext,
): Promise<Response> {
  const configuredOrigin = context.env.CORE_API_ORIGIN?.trim();

  if (!configuredOrigin) {
    return errorResponse(503, "CORE_API_ORIGIN is not configured");
  }

  let coreOrigin: URL;

  try {
    coreOrigin = new URL(configuredOrigin);
  } catch {
    return errorResponse(500, "CORE_API_ORIGIN is invalid");
  }

  if (coreOrigin.protocol !== "https:") {
    return errorResponse(500, "CORE_API_ORIGIN must use HTTPS");
  }

  const incomingURL = new URL(context.request.url);
  const edgePath = incomingURL.pathname.slice("/api".length);

  let upstreamPath = edgePath || "/";

  if (upstreamPath.startsWith("/v1/")) {
    upstreamPath = `/api${upstreamPath}`;
  }

  const upstreamURL = new URL(coreOrigin.origin);
  upstreamURL.pathname = upstreamPath;
  upstreamURL.search = incomingURL.search;

  const headers = new Headers(context.request.headers);
  headers.set("x-forwarded-host", incomingURL.host);
  headers.set("x-forwarded-proto", "https");

  try {
    const request = new Request(upstreamURL.toString(), {
      method: context.request.method,
      headers,
      body:
        context.request.method === "GET" ||
        context.request.method === "HEAD"
          ? undefined
          : context.request.body,
      redirect: "manual",
    });

    return await fetch(request);
  } catch {
    return errorResponse(502, "Core API is temporarily unavailable");
  }
}