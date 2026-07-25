import { describe, expect, it } from "vitest";
import { notificationTarget } from "./notificationTarget";

const classID = "8d694f21-e20c-4bc7-9f4d-8d233dd71c55";

describe("notificationTarget", () => {
  it("derives only application-owned routes from allowlisted resources", () => {
    expect(
      notificationTarget({
        context: {},
        resource_id: classID,
        resource_type: "class",
      }),
    ).toBe(`/app/classrooms/${classID}`);
    expect(
      notificationTarget({
        context: { class_id: classID },
        resource_id: "aadae4ad-e78e-4c4a-876a-6b023f18e7b5",
        resource_type: "class_session",
      }),
    ).toBe(`/app/classrooms/${classID}`);
    expect(notificationTarget({ context: {}, resource_type: "tenant" })).toBe(
      "/app/workspace",
    );
  });

  it("rejects unknown resources and malformed identifiers", () => {
    expect(
      notificationTarget({
        context: {},
        resource_id: "https://attacker.example",
        resource_type: "class",
      }),
    ).toBeNull();
    expect(
      notificationTarget({
        context: {},
        resource_id: classID,
        resource_type: "external_url",
      }),
    ).toBeNull();
    expect(
      notificationTarget({
        context: { class_id: "../settings" },
        resource_type: "class_session",
      }),
    ).toBeNull();
  });
});
