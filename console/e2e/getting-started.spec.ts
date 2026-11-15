import { spawn, type ChildProcess } from "node:child_process";
import path from "node:path";

import { expect, test } from "@playwright/test";

import { createApplication, registerEndpoint, signIn, uniqueAccount } from "./helpers";

// The getting-started journey against the demo receiver: a new user creates an application, registers an endpoint
// at the receiver, creates a bound key, sends a test delivery and watches its attempt succeed. The receiver is
// started here with the secret the console showed once, as a developer would.
test.describe.configure({ mode: "serial" });
test.setTimeout(120_000);

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
  await page.goto("/overview");
  await expect(page.getByTestId("workspace-selector")).toBeVisible();

  // The overview shows the getting-started steps while the environment is empty.
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(page.getByTestId("getting-started")).toBeVisible();
  await expect(page.getByTestId("computed-at")).toContainText("Read");

  // 1. Create an application; the dialog opens its page.
  await createApplication(page, "Getting started");

  // 2. Register an endpoint at the demo receiver and keep the secret the console shows once.
  const secret = await registerEndpoint(page, receiverURL, "order.shipped");
  expect(secret).toMatch(/^whsec_/);
  receiver = spawn(receiverBin, [], { env: { ...process.env, RECEIVER_SECRET: secret, RECEIVER_ADDR: receiverAddr }, stdio: "inherit" });

  // 3. Create a bound key; it is shown once and the curl example now carries it.
  await page.getByTestId("tab-keys").click();
  await page.getByTestId("create-key").click();
  await page.getByLabel("Key name").fill("backend");
  await page.getByTestId("submit-key").click();
  const keyPanel = page.getByTestId("secret-once").filter({ hasText: "API key" });
  await expect(keyPanel).toBeVisible();
  const key = await keyPanel.getByTestId("secret-value").textContent();
  expect(key).toMatch(/^hb_/);
  await page.getByTestId("key-done").click();
  await expect(page.getByTestId("curl-example")).toContainText(key!);
  await expect(page.getByTestId("keys")).not.toContainText(key!);

  // A second key is revoked from the same tab, as one would after a leak; it leaves the list of live keys.
  await page.getByTestId("create-key").click();
  await page.getByLabel("Key name").fill("leaked");
  await page.getByTestId("submit-key").click();
  await page.getByTestId("key-done").click();
  await expect(page.getByTestId("key-row")).toHaveCount(2);
  await page.getByTestId("key-row").filter({ hasText: "leaked" }).getByTestId("revoke-key").click();
  await expect(page.getByTestId("revoke-dialog")).toContainText("leaked");
  await page.getByTestId("confirm-revoke").click();
  await expect(page.getByTestId("key-row")).toHaveCount(1);
  await page.getByTestId("toggle-revoked").click();
  await expect(page.getByTestId("key-row").filter({ hasText: "leaked" })).toContainText("revoked");

  // 4. Send a test delivery from the endpoint page and read the first attempt.
  await page.getByTestId("tab-endpoints").click();
  await page.getByTestId("endpoint-link").first().click();
  await expect(page.getByTestId("endpoint-url")).toHaveText(receiverURL);
  await page.getByTestId("send-test").click();
  await expect(page.getByTestId("test-state")).toHaveText("succeeded", { timeout: 30_000 });
  await expect(page.getByTestId("test-result")).toContainText("HTTP 204");

  // The delivery is listed under the endpoint with the event it carried and what the receiver answered.
  const sent = page.getByTestId("endpoint-delivery-row").first();
  await expect(sent).toContainText("hookbeam.test");
  await expect(sent).toContainText("succeeded");
  await expect(sent).toContainText("HTTP 204");

  // Pause asks first and says what holds; the paused page shows the waiting count; resume brings it back.
  await page.getByTestId("pause").click();
  await expect(page.getByTestId("pause-dialog")).toContainText("attempts already claimed may still finish");
  await page.getByTestId("confirm-pause").click();
  await expect(page.getByTestId("resume")).toBeVisible();
  await expect(page.getByTestId("waiting-notice")).toContainText("waiting");
  await page.getByTestId("resume").click();
  await expect(page.getByTestId("pause")).toBeVisible();

  // Rotation shows the raw secret once, then only fingerprints.
  await page.getByTestId("tab-secrets").click();
  await page.getByTestId("rotate").click();
  await expect(page.getByTestId("secret-once")).toBeVisible();
  const rotated = await page.getByTestId("secret-value").textContent();
  expect(rotated).toMatch(/^whsec_/);
  await expect(page.getByTestId("secret-versions").locator("tbody tr")).toHaveCount(2);
  await page.reload();
  await expect(page.getByTestId("secret-versions")).toBeVisible();
  await expect(page.getByTestId("secret-once")).toHaveCount(0);
  await expect(page.getByTestId("secret-versions")).not.toContainText(rotated!);

  // Edit with the expected revision: saving bumps it.
  await page.getByTestId("tab-configuration").click();
  await page.getByLabel("Description").fill("first endpoint");
  await page.getByTestId("save-endpoint").click();
  await expect(page.getByText(/Saved as revision 2/)).toBeVisible();

  // A custom header is stored by name; its value is never shown again.
  await page.getByTestId("add-header").click();
  await page.getByLabel("Header 1 name").fill("X-Demo-Token");
  await page.getByLabel("Header 1 value").fill("demo-value-123");
  await page.getByTestId("save-endpoint").click();
  await expect(page.getByText(/Saved as revision 3/)).toBeVisible();
  await expect(page.getByTestId("header-names")).toContainText("x-demo-token");
  await expect(page.locator("main")).not.toContainText("demo-value-123");

  // The receiver's fail-twice scenario: two 503s, retried on the schedule, then a success at the third attempt.
  await page.getByLabel("URL", { exact: true }).fill(`${receiverURL}?scenario=fail_twice`);
  await page.getByTestId("save-endpoint").click();
  await expect(page.getByText(/Saved as revision 4/)).toBeVisible();
  // The first attempt fails, the retry comes 15 s later and fails too; the third (a minute later) would succeed.
  await page.getByTestId("send-test").click();
  await expect(page.getByTestId("test-state")).toHaveText("retry_scheduled", { timeout: 30_000 });
  await expect(page.getByTestId("test-result")).toContainText("HTTP 503");
  await expect(page.getByTestId("test-result")).toContainText("#2", { timeout: 40_000 });
  await expect(page.getByTestId("test-state")).toHaveText("retry_scheduled");

  // The overview tiles moved: accepted 1, succeeded 1, one healthy endpoint.
  await page.goto("/overview");
  await expect(page.getByTestId("tile-accepted")).toContainText("2");
  await expect(page.getByTestId("tile-endpoint-health")).toContainText("1 healthy");

  // Archiving the application retires its endpoint; the list keeps it behind the archived filter.
  await page.goto("/applications");
  await page.getByTestId("application-link").first().click();
  await page.getByTestId("tab-settings").click();
  await expect(page.getByTestId("delete-application")).toBeDisabled();
  await page.getByTestId("archive-application").click();
  await expect(page.getByTestId("archive-dialog")).toContainText("Getting started");
  await page.getByTestId("confirm-archive").click();
  await expect(page.getByTestId("archive-result")).toContainText("Archived 1 endpoint");
  await expect(page.getByTestId("archive-application")).toHaveCount(0);
  await page.goto("/applications");
  await expect(page.getByTestId("application-row")).toHaveCount(0);
  await page.getByTestId("filter-archived").click();
  await expect(page.getByTestId("application-row").first()).toContainText("archived");

  // An archived application can be deleted for good, once its name is typed.
  await page.getByTestId("row-delete").first().click();
  await expect(page.getByTestId("confirm-delete")).toBeDisabled();
  await page.getByTestId("confirm-name").fill("Getting started");
  await page.getByTestId("confirm-delete").click();
  await expect(page.getByText(/Deleted “Getting started”/)).toBeVisible();
  await expect(page.getByTestId("application-row")).toHaveCount(0);
});

test("sign out after the journey", async ({ page }) => {
  await signIn(page, account.email, account.password);
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
});
