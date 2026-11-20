import type { PhoneSummary } from "@tutorhub/api-client";
import { describe, expect, it } from "vitest";
import {
  boundedPhoneSeconds,
  compactPhoneNumber,
  formatPhoneClock,
  hasAnotherSignInMethod,
  isCompletePhoneCode,
  isPhoneUsableSignInMethod,
  isPlausiblePhoneNumber,
  phoneCountryLabel,
  phoneCountryOptions,
  sanitizePhoneCode,
  type SignInMethodInventory,
} from "./phone";

const phone: PhoneSummary = {
  calling_code: 84,
  country_code: "VN",
  id: "0d6f3c1e-7b0e-4f43-9b3f-1f0c3a0d9e21",
  last_used_at: "2026-11-19T08:00:00Z",
  last4: "5678",
  masked_number: "+84 ***5678",
  stale: false,
  verified_at: "2026-09-01T07:00:00Z",
};

const nothingElse: SignInMethodInventory = {
  emailSignIn: false,
  enrolledPasskeys: 0,
  hasTotp: false,
  mfaTotpEnabled: false,
  passkeyCount: 0,
  phone: null,
};

describe("phone helpers (AUTH-10B)", () => {
  it("labels known regions with their calling code and falls back to ISO", () => {
    expect(phoneCountryLabel("VN")).toBe("VN (+84)");
    expect(phoneCountryLabel("vn")).toBe("VN (+84)");
    expect(phoneCountryLabel("US")).toBe("US (+1)");
    expect(phoneCountryLabel("ZZ")).toBe("ZZ");
    expect(phoneCountryLabel("ZZ", 999)).toBe("ZZ (+999)");
  });

  it("keeps distinct, valid ISO regions in server order", () => {
    expect(phoneCountryOptions(["VN", "sg", "VN", "", "USA", " th "])).toEqual([
      "VN",
      "SG",
      "TH",
    ]);
    expect(phoneCountryOptions(undefined)).toEqual([]);
  });

  it("accepts national, international and spaced input without judging it", () => {
    expect(compactPhoneNumber(" 0912 345 678 ")).toBe("0912345678");
    expect(compactPhoneNumber("+84 (912) 345-678")).toBe("+84912345678");
    expect(compactPhoneNumber("84.912.345.678")).toBe("84912345678");
    for (const value of ["0912 345 678", "84912345678", "+84912345678"]) {
      expect(isPlausiblePhoneNumber(value)).toBe(true);
    }
    // A landline is syntactically fine: the server refuses it (400).
    expect(isPlausiblePhoneNumber("024 3822 1234")).toBe(true);
    for (const value of ["", "12345", "09a2345678", "++84912345678"]) {
      expect(isPlausiblePhoneNumber(value)).toBe(false);
    }
  });

  it("keeps six digits of a typed or pasted code", () => {
    expect(sanitizePhoneCode("12 34-56")).toBe("123456");
    expect(sanitizePhoneCode("1234567")).toBe("123456");
    expect(sanitizePhoneCode("abc")).toBe("");
    expect(isCompletePhoneCode("123456")).toBe(true);
    expect(isCompletePhoneCode("12345")).toBe(false);
  });

  it("bounds countdowns and formats the clock", () => {
    expect(boundedPhoneSeconds(undefined)).toBe(0);
    expect(boundedPhoneSeconds(-1)).toBe(0);
    expect(boundedPhoneSeconds(29.2)).toBe(30);
    expect(boundedPhoneSeconds(99_999)).toBe(3600);
    expect(formatPhoneClock(300)).toBe("5:00");
    expect(formatPhoneClock(61)).toBe("1:01");
    expect(formatPhoneClock(-5)).toBe("0:00");
  });

  describe("sign-in method inventory", () => {
    it("counts an active number on an account without a second factor", () => {
      const inventory = { ...nothingElse, phone };
      expect(isPhoneUsableSignInMethod(inventory)).toBe(true);
      expect(hasAnotherSignInMethod(inventory)).toBe(true);
    });

    it("never counts a stale or missing number", () => {
      expect(
        isPhoneUsableSignInMethod({
          ...nothingElse,
          phone: { ...phone, stale: true },
        }),
      ).toBe(false);
      expect(hasAnotherSignInMethod(nothingElse)).toBe(false);
    });

    it("needs a runnable second step when TOTP or a passkey exists", () => {
      const withTotp = { ...nothingElse, hasTotp: true, phone };
      expect(isPhoneUsableSignInMethod(withTotp)).toBe(false);
      expect(
        isPhoneUsableSignInMethod({ ...withTotp, mfaTotpEnabled: true }),
      ).toBe(true);

      const withPasskey = {
        ...nothingElse,
        enrolledPasskeys: 1,
        mfaTotpEnabled: true,
        phone,
      };
      // Passkey sign-in off: the passkey cannot finish the second step.
      expect(isPhoneUsableSignInMethod(withPasskey)).toBe(false);
      expect(
        isPhoneUsableSignInMethod({ ...withPasskey, passkeyCount: 1 }),
      ).toBe(true);
    });

    it("counts a passkey or the account's email on their own", () => {
      expect(hasAnotherSignInMethod({ ...nothingElse, passkeyCount: 1 })).toBe(
        true,
      );
      expect(
        hasAnotherSignInMethod({ ...nothingElse, emailSignIn: true }),
      ).toBe(true);
    });
  });
});
