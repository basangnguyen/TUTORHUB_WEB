import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test.describe("P5-COLLAB-00 isolated engine matrix", () => {
  test("cùng fixture, restore callback smoke và corruption fail closed", async ({
    page,
  }) => {
    await page.goto("/");
    await expect(page.getByRole("status")).toContainText(
      "tldraw sẵn sàng với 500 đối tượng",
      { timeout: 45_000 },
    );

    await page.getByRole("button", { name: "Chụp snapshot" }).click();
    await expect(page.getByRole("status")).toContainText("Đã chụp snapshot");
    await page.getByRole("button", { name: "Khôi phục" }).click();
    await expect(page.getByRole("status")).toContainText(
      "Khôi phục snapshot 500 đối tượng thành công",
    );

    await page.getByRole("button", { name: "Làm hỏng snapshot" }).click();
    await page.getByRole("button", { name: "Khôi phục" }).click();
    await expect(page.getByRole("status")).toContainText("Từ chối snapshot");

    await page.getByRole("button", { name: "Excalidraw 0.18.1" }).click();
    await expect(page.getByRole("status")).toContainText(
      "excalidraw sẵn sàng với 500 đối tượng",
      { timeout: 45_000 },
    );
    await expect(page.getByTestId("excalidraw-canvas")).toBeVisible();

    await page.getByRole("button", { name: "Chụp snapshot" }).click();
    await expect(page.getByRole("status")).toContainText("Đã chụp snapshot");
    await page.getByRole("button", { name: "Khôi phục" }).click();
    await expect(page.getByRole("status")).toContainText(
      "Khôi phục snapshot 500 đối tượng thành công",
    );
    await page.getByRole("button", { name: "Làm hỏng snapshot" }).click();
    await page.getByRole("button", { name: "Khôi phục" }).click();
    await expect(page.getByRole("status")).toContainText("Từ chối snapshot");
  });

  test("capability view không thể gọi snapshot restore từ control shell", async ({
    page,
  }) => {
    await page.goto("/");
    await expect(page.getByRole("status")).toContainText("sẵn sàng", {
      timeout: 45_000,
    });
    await page.getByRole("button", { name: "Chụp snapshot" }).click();
    await page.getByLabel("Capability mô phỏng").selectOption("view");

    await expect(
      page.getByRole("button", { name: "Khôi phục" }),
    ).toBeDisabled();
  });

  test("2.000 shape fixture tải được trên cả hai engine", async ({ page }) => {
    await page.goto("/");
    await expect(page.getByRole("status")).toContainText(
      "tldraw sẵn sàng với 500 đối tượng",
      { timeout: 45_000 },
    );
    await page.getByLabel("Fixture").selectOption("2000");
    await expect(page.getByRole("status")).toContainText(
      "tldraw sẵn sàng với 2.000 đối tượng",
      { timeout: 60_000 },
    );

    await page.getByRole("button", { name: "Excalidraw 0.18.1" }).click();
    await expect(page.getByRole("status")).toContainText(
      "excalidraw sẵn sàng với 2.000 đối tượng",
      { timeout: 60_000 },
    );
  });

  test("control shell có keyboard focus và không có axe violation", async ({
    page,
  }) => {
    await page.goto("/");
    await expect(page.getByRole("status")).toContainText("sẵn sàng", {
      timeout: 45_000,
    });

    await page.keyboard.press("Tab");
    await expect(
      page.getByRole("button", { name: "tldraw 5.3.1" }),
    ).toBeFocused();

    const results = await new AxeBuilder({ page })
      .include(".control-panel")
      .include(".status-line")
      .analyze();
    expect(results.violations).toEqual([]);
  });

  test("200% zoom và forced colors giữ control shell hoạt động", async ({
    page,
  }) => {
    await page.emulateMedia({
      forcedColors: "active",
      reducedMotion: "reduce",
    });
    await page.goto("/");
    await expect(page.getByRole("status")).toContainText("sẵn sàng", {
      timeout: 45_000,
    });
    await page.evaluate(() => {
      document.documentElement.style.zoom = "2";
    });
    await expect(
      page.getByRole("button", { name: "Chụp snapshot" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Chụp snapshot" }).click();
    await expect(page.getByRole("status")).toContainText("Đã chụp snapshot");
  });
});
