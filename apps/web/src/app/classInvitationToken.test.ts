import { describe, expect, it } from "vitest";
import { parseClassInvitationToken } from "./classInvitationToken";

describe("parseClassInvitationToken", () => {
  const token = `thciv1_${"Ab0_-".repeat(8)}Ab0`;

  it("accepts a raw token and a copied fragment-only invitation URL", () => {
    expect(token).toHaveLength(50);
    expect(parseClassInvitationToken(`  ${token}  `)).toBe(token);
    expect(
      parseClassInvitationToken(
        `https://staging.tutorhub.example/class-invite#token=${token}`,
      ),
    ).toBe(token);
  });

  it("rejects malformed values and tokens exposed through a query string", () => {
    expect(parseClassInvitationToken("SEC101")).toBeNull();
    expect(
      parseClassInvitationToken(
        `https://staging.tutorhub.example/class-invite?token=${token}`,
      ),
    ).toBeNull();
    expect(
      parseClassInvitationToken(
        "https://staging.tutorhub.example/class-invite#token=thciv1_short",
      ),
    ).toBeNull();
  });
});
