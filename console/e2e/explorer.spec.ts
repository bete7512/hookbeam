import { spawn, type ChildProcess } from "node:child_process";
import path from "node:path";

import { expect, test } from "@playwright/test";

import { uniqueAccount } from "./helpers";

// The explorer journey: one endpoint at the demo receiver succeeds, then its URL is told to answer 400 and a second
// test delivery fails for good. The explorer filters by outcome from the URL, the detail pages show both cycles,
// and a replay of the failed one is confirmed with the revision, outcome and retention in view.
test.describe.configure({ mode: "serial" });

const account = uniqueAccount();
const receiverBin = process.env.RECEIVER_BIN ?? path.resolve(__dirname, "../../bin/receiver");
const receiverAddr = process.env.RECEIVER_ADDR ?? ":8084";
const receiverURL = process.env.RECEIVER_URL ?? "http://localhost:8084/hooks";
let receiver: ChildProcess | null = null;

test.afterAll(() => {
  receiver?.kill();
});

test("filters live in the URL and a failed delivery can be replayed", async ({ page, context }) => {
  await page.goto("/signup");
  await page.getByLabel("Name").fill(account.name);
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(account.password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/account/);
  await page.goto("/");
  await expect(page.getByTestId("workspace-selector")).toBeVisible();

  // An empty explorer first.
  await page.goto("/events");
  await expect(page.getByRole("heading", { level: 1, name: "Events" })).toBeVisible();
  await expect(page.getByText("No events match")).toBeVisible();

  // One application, one endpoint at the receiver, started with the secret shown once.
  await page.goto("/applications");
  await page.getByLabel("Name").fill("Explorer");
  await page.getByRole("button", { name: "Create application" }).click();
  await page.getByTestId("application-link").first().click();
  await page.getByLabel("URL").fill(receiverURL);
  await page.getByLabel("Event types").fill("order.shipped");
  await page.getByRole("button", { name: "Register endpoint" }).click();
  const secretPanel = page.getByTestId("secret-once").filter({ hasText: "Signing secret" });
  const secret = await secretPanel.getByTestId("secret-value").textContent();
  receiver = spawn(receiverBin, [], { env: { ...process.env, RECEIVER_SECRET: secret!, RECEIVER_ADDR: receiverAddr }, stdio: "inherit" });

  // A succeeded delivery, then a failed one after the URL change (revision 2).
  await page.getByTestId("endpoint-link").first().click();
  await page.getByTestId("send-test").click();
  await expect(page.getByTestId("test-state")).toHaveText("succeeded", { timeout: 30_000 });
  await page.getByLabel("URL").fill(`${receiverURL}?status=400`);
  await page.getByTestId("save-endpoint").click();
  await expect(page.getByText(/Saved as revision 2/)).toBeVisible();
  await page.getByTestId("send-test").click();
  await expect(page.getByTestId("test-state")).toHaveText("failed", { timeout: 30_000 });

  // The explorer lists both; the outcome filter narrows to one and lands in the URL.
  await page.goto("/events");
  await expect(page.getByTestId("event-row")).toHaveCount(2);
  await page.getByTestId("outcome-filter").selectOption("failed");
  await expect(page).toHaveURL(/outcome=failed/);
  await expect(page.getByTestId("event-row")).toHaveCount(1);
  await page.getByLabel("Event type").fill("hookbeam.test");
  await page.getByTestId("apply-filters").click();
  await expect(page).toHaveURL(/type=hookbeam\.test/);
  await expect(page.getByTestId("event-row")).toHaveCount(1);

  // A reload keeps the filters, and the copied URL in a fresh tab reproduces the list.
  await page.reload();
  await expect(page.getByTestId("outcome-filter")).toHaveValue("failed");
  await expect(page.getByLabel("Event type")).toHaveValue("hookbeam.test");
  await expect(page.getByTestId("event-row")).toHaveCount(1);
  const copied = await context.newPage();
  await copied.goto(page.url());
  await expect(copied.getByTestId("event-row")).toHaveCount(1);
  await expect(copied.getByTestId("outcome-filter")).toHaveValue("failed");
  await copied.close();

  // Event detail: metadata, retention, one failed cycle at revision 2, and the payload for an owner.
  await page.getByTestId("event-row").getByRole("link").first().click();
  await expect(page.getByTestId("event-type")).toHaveText("hookbeam.test");
  await expect(page.getByTestId("retention")).not.toHaveText("unknown");
  const row = page.getByTestId("delivery-row");
  await expect(row).toHaveCount(1);
  await expect(row).toContainText("failed");
  await expect(row).toContainText("revision 2");
  await page.getByTestId("payload-tab").click();
  await expect(page.getByTestId("payload-text")).toContainText("hookbeam.test");

  // Delivery detail: the attempt with its classified outcome, then a confirmed replay adds a second cycle.
  await page.getByRole("tab", { name: /Deliveries/ }).click();
  await row.getByRole("link").first().click();
  await expect(page.getByTestId("delivery-state")).toHaveText("failed");
  await expect(page.getByTestId("failure")).toHaveText("http_400");
  await expect(page.getByTestId("attempt")).toHaveCount(1);
  await page.getByTestId("attempt").getByRole("button").click();
  await expect(page.getByTestId("attempt")).toContainText("secret version");
  await expect(page.getByTestId("cancel")).toBeDisabled();
  await page.getByTestId("replay").click();
  const dialog = page.getByTestId("confirm-dialog");
  await expect(dialog).toContainText("revision 2");
  await expect(dialog).toContainText("http_400");
  await expect(dialog).toContainText("processes the event twice");
  await page.getByTestId("confirm").click();
  await expect(dialog).toHaveCount(0);
  await page.goBack();
  await expect(page.getByTestId("delivery-row")).toHaveCount(2);
  await expect(page.getByText(/replay of/)).toBeVisible();
});
