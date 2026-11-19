import type { AuthProvidersResponse } from "@tutorhub/api-client";
import { describe, expect, it } from "vitest";
import {
  CALLBACK_ERROR_CODES,
  LINK_EMAIL_UNVERIFIED_TARGET,
  linkPendingTarget,
  linkProviderFrom,
  readCallbackError,
  registrationPendingTarget,
  registrationProviderFrom,
  sanitizeLinkReturnTo,
  sanitizeRegistrationReturnTo,
  sanitizeReturnTo,
  searchWithoutCallbackError,
  socialAvailabilityFor,
} from "./SignInPage.shared";

const providers: AuthProvidersResponse = {
  email_code_enabled: false,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: false,
  oauth_providers: [],
  oidc_default_enabled: false,
  passkeys_enabled: false,
  password_enabled: true,
  password_min_length: 8,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: true,
  sso_domains_enabled: false,
};

describe("readCallbackError", () => {
  it("maps each fixed OAuth callback code to itself", () => {
    for (const code of CALLBACK_ERROR_CODES) {
      expect(readCallbackError(`?error=${code}`)).toBe(code);
    }
  });

  it("never returns the raw query value", () => {
    expect(readCallbackError("?error=%3Cimg%20src%3Dx%3E")).toBe("unknown");
    expect(readCallbackError("?error=")).toBe("unknown");
    // A duplicated parameter is ambiguous, so it is treated as unknown.
    expect(readCallbackError("?error=rate_limited&error=signup_disabled")).toBe(
      "unknown",
    );
    expect(readCallbackError("?link=pending")).toBeNull();
    expect(readCallbackError("")).toBeNull();
  });

  it("removes only the error parameter", () => {
    expect(searchWithoutCallbackError("?error=rate_limited")).toBe("");
    expect(searchWithoutCallbackError("?error=x&lang=vi")).toBe("?lang=vi");
  });
});

describe("linkPendingTarget", () => {
  it("moves a pending link to the confirmation screen", () => {
    expect(linkPendingTarget("?link=pending&provider=google")).toBe(
      "/account-link?provider=google",
    );
    expect(linkPendingTarget("?link=pending&provider=oidc")).toBe(
      "/account-link?provider=oidc",
    );
  });

  it("allowlists the provider and ignores other link values", () => {
    expect(linkPendingTarget("?link=pending&provider=%2F%2Fevil")).toBe(
      "/account-link?provider=google",
    );
    expect(linkPendingTarget("?link=pending")).toBe(
      "/account-link?provider=google",
    );
    expect(linkPendingTarget("?link=done&provider=google")).toBeNull();
    expect(linkPendingTarget("")).toBeNull();
  });
});

describe("return paths", () => {
  it("keeps same-origin paths and rejects everything else", () => {
    expect(sanitizeReturnTo("/app/classrooms?tab=1")).toBe(
      "/app/classrooms?tab=1",
    );
    for (const value of [
      "https://evil.example",
      "//evil.example",
      "/\\evil",
      "/app\u0000",
      "/sign-in",
      "/sign-in/callback",
    ]) {
      expect(sanitizeReturnTo(value)).toBe("/app/home");
    }
  });

  it("never returns to the consumed linking screen after a confirmation", () => {
    expect(sanitizeLinkReturnTo("/app/messages")).toBe("/app/messages");
    expect(sanitizeLinkReturnTo("/account-link?provider=google")).toBe(
      "/app/home",
    );
    expect(sanitizeLinkReturnTo("/account-link/x")).toBe("/app/home");
    expect(sanitizeLinkReturnTo("//evil.example/app")).toBe("/app/home");
    // A path that only shares the prefix is still a normal destination.
    expect(sanitizeLinkReturnTo("/account-links")).toBe("/account-links");
  });
});

describe("socialAvailabilityFor", () => {
  it("starts the Google flow only when the server lists Google", () => {
    expect(
      socialAvailabilityFor({ ...providers, oauth_providers: ["google"] }),
    ).toEqual({
      disabled: false,
      facebook: false,
      facebookFederation: false,
      google: true,
      googleFederation: true,
      microsoft: false,
    });
    expect(
      socialAvailabilityFor({ ...providers, oidc_default_enabled: true }),
    ).toEqual({
      disabled: false,
      facebook: true,
      facebookFederation: false,
      google: true,
      googleFederation: false,
      microsoft: false,
    });
    expect(socialAvailabilityFor(providers)).toEqual({
      disabled: false,
      facebook: false,
      facebookFederation: false,
      google: false,
      googleFederation: false,
      microsoft: false,
    });
  });

  it("starts the Facebook flow only when the server lists Facebook", () => {
    expect(
      socialAvailabilityFor({ ...providers, oauth_providers: ["facebook"] }),
    ).toEqual({
      disabled: false,
      facebook: true,
      facebookFederation: true,
      google: false,
      googleFederation: false,
      microsoft: false,
    });
    expect(socialAvailabilityFor(null)).toMatchObject({
      disabled: true,
      facebookFederation: false,
      googleFederation: false,
    });
  });

  it("shows Microsoft only when the server lists it, never as a portal fallback", () => {
    expect(
      socialAvailabilityFor({ ...providers, oauth_providers: ["microsoft"] }),
    ).toEqual({
      disabled: false,
      facebook: false,
      facebookFederation: false,
      google: false,
      googleFederation: false,
      microsoft: true,
    });
    expect(
      socialAvailabilityFor({ ...providers, oidc_default_enabled: true })
        .microsoft,
    ).toBe(false);
    // While the capabilities load, Microsoft stays hidden (not a placeholder).
    expect(socialAvailabilityFor(null).microsoft).toBe(false);
  });
});

describe("pending registration helpers", () => {
  it("moves only registration=pending to the Facebook registration screen", () => {
    expect(
      registrationPendingTarget("?registration=pending&provider=facebook"),
    ).toBe("/complete-registration?provider=facebook");
    expect(registrationPendingTarget("?registration=pending")).toBe(
      "/complete-registration?provider=facebook",
    );
    expect(registrationPendingTarget("?registration=done")).toBeNull();
    expect(registrationPendingTarget("")).toBeNull();
  });

  it("never returns to the consumed registration or linking screens", () => {
    expect(sanitizeRegistrationReturnTo("/app/classes?x=1")).toBe(
      "/app/classes?x=1",
    );
    for (const value of [
      "/complete-registration",
      "/complete-registration?provider=facebook",
      "/account-link?provider=facebook",
      "/sign-in",
      "//evil.example/app",
      "https://evil.example",
    ]) {
      expect(sanitizeRegistrationReturnTo(value)).toBe("/app/home");
    }
    expect(sanitizeRegistrationReturnTo("/complete-registrations")).toBe(
      "/complete-registrations",
    );
  });

  it("moves a Microsoft pending registration to its own registration screen", () => {
    expect(
      registrationPendingTarget("?registration=pending&provider=microsoft"),
    ).toBe("/complete-registration?provider=microsoft");
    expect(
      registrationPendingTarget("?registration=pending&provider=%2F%2Fevil"),
    ).toBe("/complete-registration?provider=facebook");
    expect(registrationProviderFrom("microsoft")).toBe("microsoft");
    expect(registrationProviderFrom("google")).toBe("facebook");
    expect(registrationProviderFrom(null)).toBe("facebook");
  });

  it("allowlists Microsoft link intents and names the Settings hand-off", () => {
    expect(linkProviderFrom("microsoft")).toBe("microsoft");
    expect(linkPendingTarget("?link=pending&provider=microsoft")).toBe(
      "/account-link?provider=microsoft",
    );
    expect(LINK_EMAIL_UNVERIFIED_TARGET).toBe(
      "/app/settings?identity=email_unverified",
    );
  });

  it("allowlists the linking provider, including Facebook", () => {
    expect(linkProviderFrom("facebook")).toBe("facebook");
    expect(linkProviderFrom("oidc")).toBe("oidc");
    expect(linkProviderFrom("google")).toBe("google");
    expect(linkProviderFrom("javascript:alert(1)")).toBe("google");
    expect(linkProviderFrom(null)).toBe("google");
  });
});
