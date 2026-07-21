interface Env {
  CORE_API_ORIGIN: string;
  EDGE_CONTEXT_SECRET: string;
}

interface FunctionContext {
  request: Request;
  env: Env;
}

function errorResponse(status: number, title: string): Response {
  return new Response(
    JSON.stringify({
      type: `urn:tutorhub:problem:http-${status}`,
      title,
      status,
    }),
    {
      status,
      headers: {
        "content-type": "application/problem+json; charset=utf-8",
        "cache-control": "no-store",
      },
    },
  );
}

export async function onRequest(context: FunctionContext): Promise<Response> {
  const configuredOrigin = context.env.CORE_API_ORIGIN?.trim();

  if (!configuredOrigin) {
    return errorResponse(503, "CORE_API_ORIGIN is not configured");
  }

  const edgeContextKey = decodeEdgeContextKey(
    context.env.EDGE_CONTEXT_SECRET?.trim(),
  );
  if (!edgeContextKey) {
    return errorResponse(
      503,
      "EDGE_CONTEXT_SECRET is not configured correctly",
    );
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
  for (const name of [
    "cf-connecting-ip",
    "x-forwarded-for",
    "x-real-ip",
    "x-tutorhub-edge-version",
    "x-tutorhub-edge-timestamp",
    "x-tutorhub-client-prefix",
    "x-tutorhub-edge-signature",
  ]) {
    headers.delete(name);
  }
  headers.set("x-forwarded-host", incomingURL.host);
  headers.set("x-forwarded-proto", "https");

  const clientPrefix = canonicalClientPrefix(
    context.request.headers.get("cf-connecting-ip") ?? "",
  );
  if (!clientPrefix) {
    return errorResponse(400, "Cloudflare client address is unavailable");
  }
  const timestamp = Math.floor(Date.now() / 1000).toString();
  const requestURI = `${upstreamURL.pathname}${upstreamURL.search}`;
  const canonical = canonicalEdgeContext(
    "v1",
    timestamp,
    context.request.method,
    requestURI,
    clientPrefix,
  );
  const signature = await signEdgeContext(edgeContextKey, canonical);
  headers.set("x-tutorhub-edge-version", "v1");
  headers.set("x-tutorhub-edge-timestamp", timestamp);
  headers.set("x-tutorhub-client-prefix", clientPrefix);
  headers.set("x-tutorhub-edge-signature", signature);

  try {
    const request = new Request(upstreamURL.toString(), {
      method: context.request.method,
      headers,
      body:
        context.request.method === "GET" || context.request.method === "HEAD"
          ? undefined
          : context.request.body,
      redirect: "manual",
    });

    return await fetch(request);
  } catch {
    return errorResponse(502, "Core API is temporarily unavailable");
  }
}

function decodeEdgeContextKey(
  value: string | undefined,
): Uint8Array<ArrayBuffer> | null {
  if (!value) {
    return null;
  }
  try {
    const normalized = value.replaceAll("-", "+").replaceAll("_", "/");
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
    const decoded = Uint8Array.from(atob(padded), (character) =>
      character.charCodeAt(0),
    );
    return decoded.length >= 32 ? decoded : null;
  } catch {
    return null;
  }
}

export function canonicalEdgeContext(
  version: string,
  timestamp: string,
  method: string,
  requestURI: string,
  clientPrefix: string,
): string {
  return [
    version,
    timestamp,
    method.trim().toUpperCase(),
    requestURI,
    clientPrefix,
  ].join("\n");
}

export async function signEdgeContext(
  keyBytes: Uint8Array<ArrayBuffer>,
  canonical: string,
): Promise<string> {
  const key = await crypto.subtle.importKey(
    "raw",
    keyBytes,
    { hash: "SHA-256", name: "HMAC" },
    false,
    ["sign"],
  );
  const signature = new Uint8Array(
    await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(canonical)),
  );
  let binary = "";
  for (const byte of signature) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary)
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}

export function canonicalClientPrefix(value: string): string | null {
  const address = value.trim().split("%", 1)[0] ?? "";
  const ipv4 = address.split(".");
  if (ipv4.length === 4) {
    const octets = ipv4.map((part) => Number(part));
    if (
      octets.every(
        (part, index) =>
          Number.isInteger(part) &&
          part >= 0 &&
          part <= 255 &&
          String(part) === ipv4[index],
      )
    ) {
      return `${octets[0]}.${octets[1]}.${octets[2]}.0/24`;
    }
    return null;
  }

  const words = parseIPv6Words(address);
  if (!words) {
    return null;
  }
  words[3] &= 0xff00;
  for (let index = 4; index < words.length; index += 1) {
    words[index] = 0;
  }
  return `${compressIPv6(words)}/56`;
}

function parseIPv6Words(address: string): number[] | null {
  if (!address || (address.match(/::/g)?.length ?? 0) > 1) {
    return null;
  }
  const halves = address.split("::");
  const left = halves[0] ? halves[0].split(":") : [];
  const right = halves.length === 2 && halves[1] ? halves[1].split(":") : [];
  if (halves.length === 1 && left.length !== 8) {
    return null;
  }
  const missing = 8 - left.length - right.length;
  if (missing < (halves.length === 2 ? 1 : 0)) {
    return null;
  }
  const parts = [
    ...left,
    ...Array.from({ length: missing }, () => "0"),
    ...right,
  ];
  if (
    parts.length !== 8 ||
    parts.some((part) => !/^[0-9a-fA-F]{1,4}$/.test(part))
  ) {
    return null;
  }
  return parts.map((part) => Number.parseInt(part, 16));
}

function compressIPv6(words: number[]): string {
  const parts = words.map((word) => word.toString(16));
  let bestStart = -1;
  let bestLength = 0;
  for (let start = 0; start < parts.length; start += 1) {
    if (parts[start] !== "0") continue;
    let end = start;
    while (end < parts.length && parts[end] === "0") end += 1;
    if (end - start > bestLength) {
      bestStart = start;
      bestLength = end - start;
    }
    start = end - 1;
  }
  if (bestLength < 2) return parts.join(":");
  const left = parts.slice(0, bestStart).join(":");
  const right = parts.slice(bestStart + bestLength).join(":");
  return `${left}::${right}`;
}
