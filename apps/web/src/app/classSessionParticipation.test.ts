import { describe, expect, it } from "vitest";
import {
  classSessionParticipationQueryKeys,
  participationSourceFingerprint,
} from "./classSessionParticipation";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const seriesID = "4d7cd279-452e-48f8-913b-7897f71785a7";

describe("class session participation source identity", () => {
  it("keeps session, series, and occurrence caches separate", () => {
    const session = { kind: "session", sessionID: seriesID } as const;
    const series = { kind: "series", seriesID } as const;
    const firstOccurrence = {
      kind: "occurrence",
      occurrenceKey: "2026-07-27T02:00:00Z",
      seriesID,
    } as const;
    const secondOccurrence = {
      ...firstOccurrence,
      occurrenceKey: "2026-08-03T02:00:00Z",
    } as const;

    const keys = [session, series, firstOccurrence, secondOccurrence].map(
      (source) =>
        classSessionParticipationQueryKeys
          .audience(tenantID, userID, classID, source)
          .join("|"),
    );

    expect(new Set(keys)).toHaveLength(4);
  });

  it("includes the occurrence key in the stable source fingerprint", () => {
    expect(
      participationSourceFingerprint({
        kind: "occurrence",
        occurrenceKey: "2026-07-27T02:00:00Z",
        seriesID,
      }),
    ).toBe(`occurrence:${seriesID}:2026-07-27T02:00:00Z`);
  });
});
