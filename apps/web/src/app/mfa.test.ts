import { describe, expect, it } from "vitest";
import {
  normalizeRecoveryCode,
  recoveryCodesFileText,
  usableMfaMethods,
} from "./mfa";

describe("normalizeRecoveryCode", () => {
  it("accepts the displayed and the compact forms in any case", () => {
    expect(normalizeRecoveryCode("ABCD-EFGH-JKMN-PQRS")).toBe(
      "ABCDEFGHJKMNPQRS",
    );
    expect(normalizeRecoveryCode(" abcd efgh jkmn pqrs ")).toBe(
      "ABCDEFGHJKMNPQRS",
    );
    expect(normalizeRecoveryCode("abcdefghjkmnpqrs")).toBe("ABCDEFGHJKMNPQRS");
  });

  it("maps Crockford look-alikes and rejects anything else", () => {
    expect(normalizeRecoveryCode("O1IL-0000-1111-2222")).toBe(
      "0111000011112222",
    );
    expect(normalizeRecoveryCode("")).toBeNull();
    expect(normalizeRecoveryCode("ABCD-EFGH-JKMN")).toBeNull();
    expect(normalizeRecoveryCode("ABCD-EFGH-JKMN-PQRSX")).toBeNull();
    // U is outside the server alphabet.
    expect(normalizeRecoveryCode("UUUU-UUUU-UUUU-UUUU")).toBeNull();
  });
});

describe("usableMfaMethods", () => {
  it("orders the methods and drops a passkey this browser cannot use", () => {
    expect(
      usableMfaMethods(["recovery_code", "passkey", "totp"], true),
    ).toEqual(["totp", "passkey", "recovery_code"]);
    expect(usableMfaMethods(["recovery_code", "passkey"], false)).toEqual([
      "recovery_code",
    ]);
    expect(usableMfaMethods(["passkey"], false)).toEqual([]);
  });
});

describe("recoveryCodesFileText", () => {
  it("writes a heading, a warning and one code per line", () => {
    expect(
      recoveryCodesFileText(["AAAA-BBBB-CCCC-DDDD", "EEEE-FFFF-GGGG-HHHH"], {
        heading: "TutorHub recovery codes",
        warning: "Each code works only once.",
      }),
    ).toBe(
      "TutorHub recovery codes\nEach code works only once.\n\nAAAA-BBBB-CCCC-DDDD\nEEEE-FFFF-GGGG-HHHH\n",
    );
  });
});
