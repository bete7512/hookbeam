import { expect, test } from "@playwright/test";

import { firstLink, latestEmail, signIn, uniqueAccount } from "./helpers";

test.describe.configure({ mode: "serial" });

const account = uniqueAccount();

test("register, verify the email, sign in, select the workspace, sign out", async ({ page }) => {
  await page.goto("/signup");
  await page.getByLabel("Name").fill(account.name);
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(account.password);
  await page.getByRole("button", { name: "Create account" }).click();

  // Signup signs the person in and lands on the account page; the address is not verified yet.
  await expect(page).toHaveURL(/\/account/);
  await expect(page.getByTestId("email-verified")).toHaveText("not yet");

  // Signup sends only a welcome email; the account page asks for the verification link, which points at the API
  // and redirects back to the console with a status.
  await page.getByRole("button", { name: "Send verification email" }).click();
  await expect(page.getByText("Sent. Check your inbox.")).toBeVisible();
  const mail = await latestEmail(account.email, "verif");
  await page.goto(firstLink(mail.text, "verify-email"));
  await expect(page).toHaveURL(/\/verify-email\?status=success/);
  await expect(page.getByTestId("verify-result")).toContainText("Thanks");

  await page.goto("/account");
  await expect(page.getByTestId("email-verified")).toHaveText("yes");
  await expect(page.getByTestId("profile-name")).toHaveText(account.name);

  // Sign out, sign in again through the login form, then pick the workspace goauth created at signup.
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
  await signIn(page, account.email, account.password);

  const selector = page.getByTestId("workspace-selector");
  await expect(selector).toBeVisible();
  await expect(selector.locator("option", { hasText: `${account.name}'s workspace` })).toHaveCount(1);
  await selector.selectOption({ label: `${account.name}'s workspace` });
  await expect(page.getByTestId("environment-badge")).toContainText("Development");

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
  await page.goto("/account");
  await expect(page).toHaveURL(/\/login\?next=%2Faccount/);
});

test("a wrong password is told so and stays on the form", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill("not-the-password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Wrong email or password.")).toBeVisible();
  await expect(page).toHaveURL(/\/login/);
});

test("forgot password sends a reset link that sets a new password", async ({ page }) => {
  await page.goto("/forgot-password");
  await page.getByLabel("Email").fill(account.email);
  await page.getByRole("button", { name: "Send reset link" }).click();
  await expect(page.getByText(/the email is on its way/)).toBeVisible();
  const mail = await latestEmail(account.email, "reset");
  await page.goto(firstLink(mail.text, "reset-password"));
  await page.getByLabel("New password").fill("Passw0rd!longer");
  await page.getByRole("button", { name: "Set password" }).click();
  await expect(page.getByText(/Your password was changed/)).toBeVisible();
  await signIn(page, account.email, "Passw0rd!longer");
});
