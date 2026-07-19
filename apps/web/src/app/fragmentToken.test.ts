import { afterEach, describe, expect, it } from "vitest";
import {
  clearFragmentTokenEscrow,
  consumeFragmentToken,
} from "./fragmentToken";

const escrowKey = "fragment-token-test";

afterEach(() => {
  clearFragmentTokenEscrow(escrowKey);
  window.history.replaceState({}, "", "/");
});

describe("fragment bearer-token handling", () => {
  it("consumes and removes the token fragment without persisting it in the URL", () => {
    window.history.replaceState(
      { preserved: true },
      "",
      "/class-invite?source=share#token=thciv1_example",
    );

    expect(consumeFragmentToken(escrowKey)).toBe("thciv1_example");
    expect(window.location.pathname + window.location.search).toBe(
      "/class-invite?source=share",
    );
    expect(window.location.hash).toBe("");
    expect(window.history.state).toEqual({ preserved: true });
  });

  it("returns the in-memory value to a repeated Strict Mode initializer", () => {
    window.history.replaceState({}, "", "/invite#token=short-lived");

    expect(consumeFragmentToken(escrowKey)).toBe("short-lived");
    expect(consumeFragmentToken(escrowKey)).toBe("short-lived");
  });

  it("rejects empty and oversized values while still cleaning the fragment", () => {
    window.history.replaceState({}, "", "/class-invite#token=");
    expect(consumeFragmentToken(escrowKey)).toBeNull();
    expect(window.location.hash).toBe("");

    clearFragmentTokenEscrow(escrowKey);
    window.history.replaceState({}, "", `/class-invite#token=${"x".repeat(9)}`);
    expect(consumeFragmentToken(escrowKey, 8)).toBeNull();
    expect(window.location.hash).toBe("");
  });

  it("does not reuse an escrowed token on a different clean URL", () => {
    window.history.replaceState({}, "", "/invite#token=one-time");
    expect(consumeFragmentToken(escrowKey)).toBe("one-time");

    window.history.replaceState({}, "", "/class-invite");
    expect(consumeFragmentToken(escrowKey)).toBeNull();
  });
});
