import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test("Gate E keyboard flow, focus handoff, semantic fallback, and Axe pass", async ({
  page,
}) => {
  await page.goto("/excalidraw.html?fixture=500");

  const semantic = page.getByTestId("semantic-canvas");
  await expect(semantic).toBeVisible();
  await expect(semantic.getByRole("listitem")).toHaveCount(50);
  await expect(page.getByTestId("semantic-page-status")).toContainText(
    "trang 1 trên 10",
  );
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

  const openButton = page.getByRole("button", { name: "Mở bảng Excalidraw" });
  await openButton.focus();
  await page.keyboard.press("Enter");
  const board = page.locator(".board-frame");
  await expect(board).toBeFocused();
  await expect(page.getByRole("status")).toContainText(
    "Excalidraw sẵn sàng với 500 đối tượng.",
  );
  const patchedFooter = page.locator(
    'footer[data-accessibility-patch="excalidraw-0.18.1-nested-contentinfo"]',
  );
  await expect(patchedFooter).toHaveAttribute("role", "group");
  await expect(patchedFooter).toHaveAttribute(
    "aria-label",
    "Điều khiển bảng vẽ",
  );

  const semanticButton = page.getByRole("button", {
    name: "Đọc nội dung bảng",
  });
  await semanticButton.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "Nội dung bảng có thể truy cập" }),
  ).toBeFocused();

  const nextPage = page.getByRole("button", { name: "Trang sau" });
  await nextPage.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("semantic-page-status")).toContainText(
    "trang 2 trên 10",
  );
  await expect(semantic.getByRole("listitem").first()).toContainText(
    "51. Hình chữ nhật",
  );

  await page
    .getByRole("button", { name: "Chuyển tiêu điểm tới canvas" })
    .press("Enter");
  await expect(board).toBeFocused();

  const closeButton = page.getByRole("button", {
    name: "Đóng bảng Excalidraw",
  });
  await closeButton.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("candidate-placeholder")).toBeVisible();
  await expect(openButton).toBeFocused();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test("Gate E reflows at a 200% equivalent viewport without horizontal overflow", async ({
  page,
}) => {
  await page.setViewportSize({ width: 640, height: 720 });
  await page.goto("/excalidraw.html?fixture=2000");
  await page.getByRole("button", { name: "Mở bảng Excalidraw" }).click();
  await expect(page.locator(".board-frame")).toBeFocused();
  await expect(
    page.getByTestId("semantic-canvas").getByRole("listitem"),
  ).toHaveCount(50);

  const overflow = await page.evaluate(() => ({
    body: document.body.scrollWidth - document.body.clientWidth,
    document:
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  }));
  expect(overflow.body).toBeLessThanOrEqual(1);
  expect(overflow.document).toBeLessThanOrEqual(1);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test("Gate E honors forced colors and reduced motion on the exact candidate", async ({
  page,
}) => {
  await page.emulateMedia({ forcedColors: "active", reducedMotion: "reduce" });
  await page.goto("/excalidraw.html?fixture=500");
  const semantic = page.getByTestId("semantic-canvas");
  await expect(semantic).toBeVisible();
  const styles = await semantic.evaluate((element) => {
    const semanticStyle = getComputedStyle(element);
    const buttonStyle = getComputedStyle(
      element.querySelector("button") as HTMLButtonElement,
    );
    return {
      animationDuration: semanticStyle.animationDuration,
      borderStyle: semanticStyle.borderTopStyle,
      transitionDuration: buttonStyle.transitionDuration,
    };
  });
  expect(styles.borderStyle).not.toBe("none");
  expect(parseCssDuration(styles.animationDuration)).toBeLessThanOrEqual(0.01);
  expect(parseCssDuration(styles.transitionDuration)).toBeLessThanOrEqual(0.01);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

function parseCssDuration(value: string): number {
  const first = value.split(",")[0]?.trim() ?? "0s";
  if (first.endsWith("ms")) return Number.parseFloat(first);
  if (first.endsWith("s")) return Number.parseFloat(first) * 1_000;
  return Number.parseFloat(first);
}
