import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test.describe("P3-CAL-01 calendar renderer spike", () => {
  test("passes keyboard, agenda alternative and axe checks", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(
      page.getByRole("heading", {
        name: /Calendar renderer & recurrence spike/i,
      }),
    ).toBeVisible();
    await expect(page.locator("[data-calendar-renderer]")).toBeVisible();
    await expect(page.getByTestId("calendar-ready-ms")).not.toHaveText(
      "Đang đo…",
    );

    const axeResults = await new AxeBuilder({ page }).analyze();
    expect(axeResults.violations).toEqual([]);

    const firstMove = page
      .getByRole("button", { name: "Dời sau 30 phút" })
      .first();
    await firstMove.focus();
    await expect(firstMove).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.getByTestId("calendar-announcement")).toContainText(
      "Đã cập nhật thời gian",
    );
  });

  test("reverts the conflict fixture through the keyboard alternative", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    const conflict = page.locator('[data-agenda-event-id="hcm-conflict"]');
    await conflict.getByRole("button", { name: "Dời sau 30 phút" }).click();
    await expect(page.getByTestId("calendar-announcement")).toContainText(
      "409",
    );
    await expect(page.getByTestId("calendar-announcement")).toHaveAttribute(
      "class",
      /error/,
    );
  });

  test("uses agenda as the mobile-first alternative", async ({ page }) => {
    await page.setViewportSize({ width: 640, height: 900 });
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(page.locator(".calendar-spike__calendar")).toBeHidden();
    await expect(
      page.getByRole("heading", { name: /Chương trình thay thế/ }),
    ).toBeVisible();
  });

  for (const fixtureCount of [500, 1000, 2000]) {
    test(`records bounded render time for ${fixtureCount} visible fixtures`, async ({
      page,
    }) => {
      await page.goto(`/?events=${fixtureCount}`, {
        waitUntil: "domcontentloaded",
      });
      const readyText = await page.getByTestId("calendar-ready-ms").innerText();
      const readyMs = Number.parseInt(readyText, 10);
      expect(Number.isFinite(readyMs)).toBe(true);

      // These are intentionally conservative CI guardrails. ADR evidence
      // records the measured p50/p95; this test catches catastrophic growth.
      const budget =
        fixtureCount === 500 ? 5_000 : fixtureCount === 1000 ? 8_000 : 12_000;
      expect(readyMs).toBeLessThan(budget);
      console.log(JSON.stringify({ fixtureCount, readyMs }));
    });
  }
});
