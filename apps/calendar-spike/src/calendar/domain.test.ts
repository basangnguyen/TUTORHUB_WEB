import { describe, expect, it, vi } from "vitest";
import { commitWithRevert } from "./domain";

describe("optimistic calendar mutations", () => {
  it("reverts and announces a server conflict", async () => {
    const revert = vi.fn();
    const announce = vi.fn();
    const result = await commitWithRevert(
      { revert },
      {
        itemId: "conflict",
        startsAt: "2026-07-23T01:00:00.000Z",
        endsAt: "2026-07-23T02:00:00.000Z",
        timeZone: "Asia/Ho_Chi_Minh",
        expectedVersion: 4,
        source: "drag",
      },
      async () => ({
        accepted: false,
        code: "conflict",
        message: "409 conflict",
      }),
      announce,
    );

    expect(result.accepted).toBe(false);
    expect(revert).toHaveBeenCalledOnce();
    expect(announce).toHaveBeenCalledWith({
      tone: "error",
      message: "409 conflict",
    });
  });

  it("does not revert an accepted keyboard mutation", async () => {
    const revert = vi.fn();
    const announce = vi.fn();
    const result = await commitWithRevert(
      { revert },
      {
        itemId: "ok",
        startsAt: "2026-07-23T01:00:00.000Z",
        endsAt: "2026-07-23T02:00:00.000Z",
        timeZone: "Asia/Ho_Chi_Minh",
        expectedVersion: 1,
        source: "keyboard",
      },
      async () => ({
        accepted: true,
        item: {
          id: "ok",
          title: "OK",
          startsAt: "2026-07-23T01:00:00.000Z",
          endsAt: "2026-07-23T02:00:00.000Z",
          timeZone: "Asia/Ho_Chi_Minh",
          category: "class",
          status: "scheduled",
          version: 2,
        },
      }),
      announce,
    );

    expect(result.accepted).toBe(true);
    expect(revert).not.toHaveBeenCalled();
    expect(announce).toHaveBeenCalledWith({
      tone: "success",
      message: "Đã cập nhật thời gian bằng bàn phím.",
    });
  });
});
