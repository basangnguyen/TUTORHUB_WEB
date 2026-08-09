import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page, type Request } from "@playwright/test";

const LOCAL_ORIGIN = "http://127.0.0.1:4176";

function recordExternalRequests(page: Page): readonly string[] {
  const externalRequests: string[] = [];
  const inspectRequest = (request: Request) => {
    const url = request.url();
    if (
      !url.startsWith(`${LOCAL_ORIGIN}/`) &&
      !url.startsWith("data:") &&
      !url.startsWith("blob:")
    ) {
      externalRequests.push(url);
    }
  };
  page.on("request", inspectRequest);
  return externalRequests;
}

async function chooseFixture(page: Page, count: 2 | 5 | 25 | 50) {
  await page.getByRole("button", { name: `${count} người` }).click();
}

test("passes axe and never requests a non-local resource", async ({ page }) => {
  const externalRequests = recordExternalRequests(page);
  await page.goto("/");
  await expect(
    page.getByRole("heading", {
      name: "Classroom media UX research harness",
    }),
  ).toBeVisible();
  for (const effect of ["Làm mờ", "Studio ấm", "Lớp học", "Rừng dịu"]) {
    await page.getByRole("button", { name: effect }).click();
  }
  await page.getByText("Feature-detect advisory của browser hiện tại").click();

  const accessibilityScanResults = await new AxeBuilder({ page }).analyze();
  expect(accessibilityScanResults.violations).toEqual([]);
  expect(externalRequests).toEqual([]);
});

test("supports keyboard-only viewport, layout, page and pin controls", async ({
  page,
}) => {
  await page.goto("/");
  const fixture50 = page.getByRole("button", { name: "50 người" });
  await fixture50.focus();
  await page.keyboard.press("Enter");
  await expect(fixture50).toHaveAttribute("aria-pressed", "true");

  const compact = page.getByRole("button", { name: "Hẹp 320px · 4 tile" });
  await compact.focus();
  await page.keyboard.press("Enter");
  await expect(compact).toHaveAttribute("aria-pressed", "true");
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Lưới" })).toBeFocused();

  await page.keyboard.press("Tab");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: "Người đang nói" }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByText("Giới hạn 4/50 video mock")).toBeVisible();

  const pin = page.getByRole("button", { name: /^Ghim Học viên 02$/ });
  await pin.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: /^Bỏ ghim Học viên 02$/ }),
  ).toBeFocused();

  const nextPage = page.getByRole("button", { name: "Trang sau" });
  await nextPage.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByText("2 / 17")).toBeVisible();
  await expect(nextPage).toBeFocused();
});

test("keeps fixture and layout rendering inside declared subscription caps", async ({
  page,
}) => {
  await page.goto("/");
  await chooseFixture(page, 50);

  await expect(page.getByText("Giới hạn 12/50 video mock")).toBeVisible();
  await expect(page.getByText("1 / 5")).toBeVisible();

  await page.getByRole("button", { name: "Trung bình · 6 tile" }).click();
  await expect(page.getByText("Giới hạn 6/50 video mock")).toBeVisible();
  await expect(page.getByText("1 / 9")).toBeVisible();

  await page.getByRole("button", { name: "Hẹp 320px · 4 tile" }).click();
  await expect(page.getByText("Giới hạn 4/50 video mock")).toBeVisible();
  await expect(page.getByText("1 / 13")).toBeVisible();

  await page.getByRole("button", { name: "Người đang nói" }).click();
  await expect(page.getByText("Giới hạn 4/50 video mock")).toBeVisible();
  await expect(page.getByText("1 / 17")).toBeVisible();

  await page.getByRole("button", { name: "Trình chiếu" }).click();
  await expect(page.getByText("Giới hạn 3/50 video mock")).toBeVisible();
  await expect(page.getByText("1 / 17")).toBeVisible();
});

test("reflows at 320 CSS pixels without horizontal document overflow", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  await page.goto("/");
  await chooseFixture(page, 50);
  await page.getByRole("button", { name: "Hẹp 320px · 4 tile" }).click();

  const geometry = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    bodyScrollWidth: document.body.scrollWidth,
  }));
  expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.clientWidth);
  expect(geometry.bodyScrollWidth).toBeLessThanOrEqual(geometry.clientWidth);
  await expect(page.getByRole("button", { name: "Trang sau" })).toBeVisible();
});

test("honors reduced motion and forced colors without losing state text", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await page.getByRole("button", { name: "Vỗ tay" }).click();
  const reaction = page.getByText("Vỗ tay × 1").locator("..");
  await expect(reaction).toBeVisible();
  expect(
    await page.evaluate(
      () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  const animationDurationSeconds = await reaction.evaluate((element) => {
    const duration = window.getComputedStyle(element).animationDuration;
    if (duration.endsWith("ms")) {
      return Number.parseFloat(duration) / 1_000;
    }
    return Number.parseFloat(duration);
  });
  expect(animationDurationSeconds).toBeLessThanOrEqual(0.001);

  await page.emulateMedia({ forcedColors: "active", reducedMotion: "reduce" });
  expect(
    await page.evaluate(
      () => window.matchMedia("(forced-colors: active)").matches,
    ),
  ).toBe(true);
  const safetyBadge = page.getByRole("note");
  await expect(safetyBadge).toBeVisible();
  expect(
    await safetyBadge.evaluate(
      (element) => window.getComputedStyle(element).borderTopStyle,
    ),
  ).not.toBe("none");
  await expect(page.getByText("Vỗ tay × 1")).toBeVisible();
});
