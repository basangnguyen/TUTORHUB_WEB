import { describe, expect, it } from "vitest";
import {
  isOrderedSessionRange,
  resolveCivilDateTime,
} from "./classSessionTime";

describe("class session civil time", () => {
  it("serializes an ordinary Ho Chi Minh civil time with its real offset", () => {
    expect(
      resolveCivilDateTime("2026-07-24T09:15", "Asia/Ho_Chi_Minh"),
    ).toEqual({
      kind: "resolved",
      value: "2026-07-24T09:15:00+07:00",
    });
  });

  it("rejects a New York spring-forward gap", () => {
    expect(
      resolveCivilDateTime("2026-03-08T02:30", "America/New_York"),
    ).toEqual({ kind: "invalid", reason: "gap" });
  });

  it("requires an explicit choice for a New York fall overlap", () => {
    expect(
      resolveCivilDateTime("2026-11-01T01:30", "America/New_York"),
    ).toEqual({
      kind: "overlap",
      earlier: "2026-11-01T01:30:00-04:00",
      later: "2026-11-01T01:30:00-05:00",
    });
    expect(
      resolveCivilDateTime("2026-11-01T01:30", "America/New_York", "later"),
    ).toEqual({
      kind: "resolved",
      value: "2026-11-01T01:30:00-05:00",
    });
  });

  it("compares instants instead of local wall-clock strings", () => {
    expect(
      isOrderedSessionRange(
        "2026-11-01T01:30:00-04:00",
        "2026-11-01T01:15:00-05:00",
      ),
    ).toBe(true);
  });
});
