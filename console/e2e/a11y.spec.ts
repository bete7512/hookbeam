import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

// Every public form must be operable without a mouse: labelled controls, visible focus, no serious axe findings.
for (const path of ["/", "/login", "/signup", "/forgot-password", "/reset-password?token=x", "/verify-email?status=error"]) {
  test(`${path} has no serious accessibility violations and is keyboard-operable`, async ({ page }) => {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
    const serious = results.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
    expect(serious, JSON.stringify(serious, null, 2)).toEqual([]);

    // Tab reaches a focusable control and the focus is visible (a ring or outline, never nothing).
    await page.keyboard.press("Tab");
    const focused = page.locator(":focus");
    await expect(focused).toBeVisible();
    const visible = await focused.evaluate((el) => {
      const s = getComputedStyle(el);
      return s.outlineStyle !== "none" || s.boxShadow !== "none";
    });
    expect(visible).toBe(true);
  });
}
