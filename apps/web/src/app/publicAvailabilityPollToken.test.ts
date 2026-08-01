import { afterEach, describe, expect, it } from "vitest";
import {
  clearPublicAvailabilityPollToken,
  consumePublicAvailabilityPollToken,
  primePublicAvailabilityPollToken,
} from "./publicAvailabilityPollToken";

afterEach(() => {
  clearPublicAvailabilityPollToken();
  window.history.replaceState({}, "", "/");
});

describe("public availability poll bootstrap", () => {
  it("erases the complete fragment synchronously and keeps only the token in memory", () => {
    window.history.replaceState(
      { preserved: true },
      "",
      "/availability/8818c018-b6c5-4f44-a844-7cbec84a986d?source=share#token=v1.secret&tracking=discard",
    );

    primePublicAvailabilityPollToken();

    expect(window.location.hash).toBe("");
    expect(window.location.pathname + window.location.search).toBe(
      "/availability/8818c018-b6c5-4f44-a844-7cbec84a986d?source=share",
    );
    expect(window.history.state).toEqual({ preserved: true });
    expect(consumePublicAvailabilityPollToken()).toBe("v1.secret");
  });

  it("does not consume fragments belonging to a different route", () => {
    window.history.replaceState({}, "", "/app/home#token=not-a-poll-token");

    primePublicAvailabilityPollToken();

    expect(window.location.hash).toBe("#token=not-a-poll-token");
    expect(consumePublicAvailabilityPollToken()).toBe("not-a-poll-token");
  });
});
