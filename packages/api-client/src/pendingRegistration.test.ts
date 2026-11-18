import { describe, expect, it, vi } from "vitest";
import {
  buildOAuthStartURL,
  cancelPendingRegistration,
  completePendingRegistration,
  getCurrentCSRFToken,
  getFacebookDeletionStatus,
  getPendingRegistration,
  resendPendingRegistration,
  rotateCSRFToken,
  startPendingRegistration,
} from "./index";

const challenge = {
  challenge_id: "85e92a95-5482-4766-b91f-2ce9c4e70044",
  cooldown_seconds: 60,
  expires_in_seconds: 300,
};

const completeRequest = {
  challenge_id: challenge.challenge_id,
  code: "123456",
  terms_version: "2026-11",
  keep_signed_in: true,
};

describe("Facebook registration", () => {
  it("opens the Facebook provider and reads a preview with no email hint", async () => {
    const preview = { provider: "facebook", expires_in_seconds: 600 };
    const fetch = vi.fn(async (request: Request) => {
      expect(new URL(request.url).pathname).toBe(
        "/api/v1/auth/pending-registration",
      );
      expect(request.credentials).toBe("include");
      expect(request.cache).toBe("no-store");
      expect(request.referrerPolicy).toBe("no-referrer");
      return Response.json(preview);
    });
    const options = { baseUrl: "https://api.example/api", fetch };
    expect(new URL(buildOAuthStartURL("facebook", {}, options)).pathname).toBe(
      "/api/v1/auth/oauth/facebook/start",
    );
    await expect(getPendingRegistration(options)).resolves.toEqual(preview);
  });

  it("sends the submitted address once and resends or cancels with the cookie", async () => {
    const fetch = vi
      .fn<(request: Request) => Promise<Response>>()
      .mockResolvedValueOnce(Response.json(challenge, { status: 202 }))
      .mockResolvedValueOnce(Response.json(challenge, { status: 202 }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    const options = { fetch };
    const email = { email: "learner@example.test" };
    await expect(startPendingRegistration(email, options)).resolves.toEqual(
      challenge,
    );
    await expect(resendPendingRegistration(options)).resolves.toEqual(
      challenge,
    );
    await expect(cancelPendingRegistration(options)).resolves.toBeUndefined();
    const requests = fetch.mock.calls.map(([request]) => request);
    expect(requests.map((request) => new URL(request.url).pathname)).toEqual([
      "/api/v1/auth/pending-registration/start",
      "/api/v1/auth/pending-registration/resend",
      "/api/v1/auth/pending-registration/cancel",
    ]);
    for (const request of requests) {
      expect(request.method).toBe("POST");
      expect(request.credentials).toBe("include");
      expect(request.headers.has("Origin")).toBe(true);
      expect(request.headers.has("X-CSRF-Token")).toBe(false);
    }
    expect(
      await Promise.all(requests.map((request) => request.json())),
    ).toEqual([email, {}, {}]);
  });

  it.each(["link_pending", "authenticated"] as const)(
    "preserves or clears cached CSRF when completion returns %s",
    async (status) => {
      const result = {
        status,
        return_to: status === "link_pending" ? "/account-link" : "/app/home",
      };
      const fetch = vi
        .fn<(request: Request) => Promise<Response>>()
        .mockResolvedValueOnce(Response.json({ csrf_token: "prior-session" }))
        .mockResolvedValueOnce(Response.json(result));
      await rotateCSRFToken({ fetch });
      await expect(
        completePendingRegistration(completeRequest, { fetch }),
      ).resolves.toEqual(result);
      const [request] = fetch.mock.calls[1]!;
      expect(new URL(request.url).pathname).toBe(
        "/api/v1/auth/pending-registration/complete",
      );
      expect(await request.json()).toEqual(completeRequest);
      expect(getCurrentCSRFToken()).toBe(
        status === "link_pending" ? "prior-session" : null,
      );
    },
  );

  it("retains OTP retry information and session CSRF after a rejected complete", async () => {
    const fetch = vi
      .fn<(request: Request) => Promise<Response>>()
      .mockResolvedValueOnce(Response.json({ csrf_token: "prior-session" }))
      .mockResolvedValueOnce(
        Response.json(
          { type: "about:blank", title: "Please wait", status: 429 },
          { status: 429, headers: { "Retry-After": "60" } },
        ),
      );
    await rotateCSRFToken({ fetch });
    await expect(
      completePendingRegistration(completeRequest, { fetch }),
    ).rejects.toMatchObject({
      status: 429,
      retryAfterSeconds: 60,
    });
    expect(getCurrentCSRFToken()).toBe("prior-session");
  });

  it("uses a public receipt path without sending credentials or referrer", async () => {
    const code = "1adbf543ee7123458917a955560134bd";
    const fetch = vi.fn(async (request: Request) => {
      expect(new URL(request.url).pathname).toBe(
        `/api/v1/auth/oauth/facebook/data-deletion/${code}`,
      );
      expect(request.credentials).toBe("omit");
      expect(request.cache).toBe("no-store");
      expect(request.referrerPolicy).toBe("no-referrer");
      return Response.json({ status: "completed" });
    });
    await expect(getFacebookDeletionStatus(code, { fetch })).resolves.toEqual({
      status: "completed",
    });
    await expect(getFacebookDeletionStatus("..", { fetch })).rejects.toThrow(
      TypeError,
    );
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
