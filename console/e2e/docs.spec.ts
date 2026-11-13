import { expect, test } from "@playwright/test";

import { docLinks } from "../src/components/docs/nav";

// Every documentation page renders without a client error (a hydration mismatch throws the server's HTML away and
// resets the theme), keeps the reader's theme, and draws its diagrams.
for (const link of docLinks) {
  test(`${link.href} renders cleanly in the light theme`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(String(e)));
    await page.addInitScript(() => localStorage.setItem("hookbeam.theme", "light"));
    await page.goto(link.href);
    if (link.fullPage) {
      await expect(page.getByText("Hookbeam API").first()).toBeVisible();
    } else {
      await expect(page.getByRole("heading", { level: 1, name: link.title })).toBeVisible();
    }
    await page.waitForLoadState("networkidle");
    expect(errors).toEqual([]);
    expect(await page.evaluate(() => document.documentElement.classList.contains("dark"))).toBe(false);
  });
}

test("the diagrams draw their nodes and the lifecycle player steps", async ({ page }) => {
  await page.goto("/docs/lifecycle");
  await expect(page.locator(".react-flow__node").first()).toBeVisible();
  await expect(page.getByText("Step 1 of 10")).toBeVisible();
  await page.getByRole("button", { name: "Next step" }).click();
  await expect(page.getByText("Step 2 of 10")).toBeVisible();

  await page.goto("/docs/data-model");
  await expect(page.locator(".react-flow__node").filter({ hasText: "deliveries" }).first()).toBeVisible();
  await page.getByRole("button", { name: /Control plane/ }).click();
  await expect(page.locator(".react-flow__node").filter({ hasText: "api_keys" }).first()).toBeVisible();
});

test("an old link to the API reference's anchors still lands on it", async ({ page }) => {
  await page.goto("/docs#tag/me");
  await expect(page).toHaveURL(/\/docs\/api#tag\/me$/);
  await expect(page.getByText("Hookbeam API").first()).toBeVisible();
});
