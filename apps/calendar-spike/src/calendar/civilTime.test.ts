import { describe, expect, it } from "vitest";
import { resolveCivilDateTime } from "./civilTime";

describe("civil time and DST contract", () => {
  it("rejects a New York spring-forward gap", () => {
    expect(() =>
      resolveCivilDateTime({
        local: "2026-03-08T02:30",
        timeZone: "America/New_York",
        disambiguation: "reject",
      }),
    ).toThrow();
  });

  it("keeps New York overlap choices one hour apart", () => {
    const earlier = resolveCivilDateTime({
      local: "2026-11-01T01:30",
      timeZone: "America/New_York",
      disambiguation: "earlier",
    });
    const later = resolveCivilDateTime({
      local: "2026-11-01T01:30",
      timeZone: "America/New_York",
      disambiguation: "later",
    });

    expect(earlier.offset).toBe("-04:00");
    expect(later.offset).toBe("-05:00");
    expect(Date.parse(later.instant) - Date.parse(earlier.instant)).toBe(
      3_600_000,
    );
  });

  it("round-trips a Vietnam civil time without host timezone assumptions", () => {
    const result = resolveCivilDateTime({
      local: "2026-07-23T15:00",
      timeZone: "Asia/Ho_Chi_Minh",
      disambiguation: "reject",
    });
    expect(result.local.startsWith("2026-07-23T15:00")).toBe(true);
    expect(result.offset).toBe("+07:00");
  });
});
