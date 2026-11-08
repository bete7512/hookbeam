import { spawn, type ChildProcess } from "node:child_process";
import path from "node:path";

import { expect, test } from "@playwright/test";

import { signIn, uniqueAccount } from "./helpers";

// The getting-started journey against the demo receiver: a new user creates an application, registers an endpoint
// at the receiver, creates a bound key, sends a test delivery and watches its attempt succeed. The receiver is
// started here with the secret the console showed once, as a developer would.
test.describe.configure({ mode: "serial" });

const account = uniqueAccount();
const receiverBin = process.env.RECEIVER_BIN ?? path.resolve(__dirname, "../../bin/receiver");
const receiverAddr = process.env.RECEIVER_ADDR ?? ":8083";
const receiverURL = process.env.RECEIVER_URL ?? "http://localhost:8083/hooks";
let receiver: ChildProcess | null = null;

test.afterAll(() => {
  receiver?.kill();
});

test("a new user reaches a succeeded attempt from the overview", async ({ page }) => {
  // Sign up (signup signs in) and wait for the workspace goauth creates.
  await page.goto("/signup");
  await page.getByLabel("Name").fill(account.name);
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(account.password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/account/);
  await page.goto("/");
  await expect(page.getByTestId("workspace-selector")).toBeVisible();

  // The overview shows the getting-started steps while the environment is empty.
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(page.getByTestId("getting-started")).toBeVisible();
  await expect(page.getByTestId("computed-at")).toContainText("Read");

  // 1. Create an application.
  await page.goto("/applications");
  await page.getByLabel("Name").fill("Getting started");
  await page.getByRole("button", { name: "Create application" }).click();
  await page.getByTestId("application-link").first().click();
  await expect(page.getByTestId("application-name")).toHaveText("Getting started");

  // 2. Register an endpoint at the demo receiver and keep the secret the console shows once.
  await page.getByLabel("URL").fill(receiverURL);
  await page.getByLabel("Event types").fill("order.shipped");
  await page.getByRole("button", { name: "Register endpoint" }).click();
  const secretPanel = page.getByTestId("secret-once").filter({ hasText: "Signing secret" });
  await expect(secretPanel).toBeVisible();
  const secret = await secretPanel.getByTestId("secret-value").textContent();
  expect(secret).toMatch(/^whsec_/);
  receiver = spawn(receiverBin, [], { env: { ...process.env, RECEIVER_SECRET: secret!, RECEIVER_ADDR: receiverAddr }, stdio: "inherit" });

  // 3. Create a bound key; the curl example now carries it.
  await page.getByLabel("Key name").fill("backend");
  await page.getByRole("button", { name: "Create key" }).click();
  const keyPanel = page.getByTestId("secret-once").filter({ hasText: "API key" });
  await expect(keyPanel).toBeVisible();
  const key = await keyPanel.getByTestId("secret-value").textContent();
  expect(key).toMatch(/^hb_/);
  await expect(page.getByTestId("curl-example")).toContainText(key!);

  // 4. Send a test delivery from the endpoint page and read the first attempt.
  await page.getByTestId("endpoint-link").first().click();
  await expect(page.getByTestId("endpoint-url")).toHaveText(receiverURL);
  await page.getByTestId("send-test").click();
  await expect(page.getByTestId("test-state")).toHaveText("succeeded", { timeout: 30_000 });
  await expect(page.getByTestId("test-result")).toContainText("HTTP 204");

  // Pause shows the waiting count and the claimed-requests notice; resume brings it back.
  await page.getByTestId("pause").click();
  await expect(page.getByTestId("resume")).toBeVisible();
  await expect(page.getByTestId("waiting-notice")).toContainText("deliveries are waiting");
  await page.getByTestId("resume").click();
  await expect(page.getByTestId("pause")).toBeVisible();
  await expect(page.getByText("attempts already claimed may still finish")).toBeVisible();

  // Rotation shows the raw secret once, then only fingerprints.
  await page.getByTestId("rotate").click();
  await expect(page.getByTestId("secret-once")).toBeVisible();
  const rotated = await page.getByTestId("secret-value").textContent();
  expect(rotated).toMatch(/^whsec_/);
  await expect(page.getByTestId("secret-versions").locator("tbody tr")).toHaveCount(2);
  await page.reload();
  await expect(page.getByTestId("secret-once")).toHaveCount(0);
  await expect(page.getByTestId("secret-versions")).not.toContainText(rotated!);

  // Edit with the expected revision: saving bumps it.
  await page.getByLabel("Description").fill("first endpoint");
  await page.getByTestId("save-endpoint").click();
  await expect(page.getByText(/Saved as revision 2/)).toBeVisible();

  // The overview tiles moved: accepted 1, succeeded 1, one healthy endpoint.
  await page.goto("/");
  await expect(page.getByText("Accepted").locator("..")).toContainText("1");
  await expect(page.getByText("Endpoint health").locator("..")).toContainText("1 healthy");
});

test("sign out after the journey", async ({ page }) => {
  await signIn(page, account.email, account.password);
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
});
