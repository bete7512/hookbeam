import { spawn, type ChildProcess } from "node:child_process";
import path from "node:path";

import { expect, test, type Browser } from "@playwright/test";

import { firstLink, latestEmail, signIn, uniqueAccount } from "./helpers";

// The team journey: an owner invites a second person, who accepts from the emailed link as a new account; the owner
// turns them into a viewer, mints and revokes a key, and reads the notification an endpoint failing three times
// produces. The last part needs both workers on the queue (LocalStack), as CI runs them.
test.describe.configure({ mode: "serial" });

const owner = uniqueAccount();
const invitee = uniqueAccount();
const receiverBin = process.env.RECEIVER_BIN ?? path.resolve(__dirname, "../../bin/receiver");
const receiverAddr = process.env.TEAM_RECEIVER_ADDR ?? ":8085";
const receiverURL = process.env.TEAM_RECEIVER_URL ?? "http://localhost:8085/hooks";
let receiver: ChildProcess | null = null;
let inviteLink = "";

test.afterAll(() => {
  receiver?.kill();
});

async function freshSession(browser: Browser) {
  const context = await browser.newContext();
  return { context, page: await context.newPage() };
}

test("the owner invites a developer", async ({ page }) => {
  await page.goto("/signup");
  await page.getByLabel("Name").fill(owner.name);
  await page.getByLabel("Email").fill(owner.email);
  await page.getByLabel("Password").fill(owner.password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/account/);
  await page.goto("/");
  await expect(page.getByTestId("workspace-selector")).toBeVisible();

  await page.goto("/team");
  await expect(page.getByTestId("member-row")).toHaveCount(1);
  await expect(page.getByTestId("member-role")).toHaveValue("owner");
  await page.getByLabel("Email").fill(invitee.email);
  await page.getByTestId("send-invite").click();
  await expect(page.getByText(/Invitation sent to/)).toBeVisible();
  await expect(page.getByTestId("invitation-row")).toHaveCount(1);
  await expect(page.getByTestId("invitation-row")).toContainText("developer");

  const mail = await latestEmail(invitee.email, "invited");
  inviteLink = firstLink(mail.text, "invitations/accept");
});

test("the invitee creates an account from the link, accepts and sees the team read-only", async ({ browser }) => {
  const { context, page } = await freshSession(browser);
  await page.goto(inviteLink);
  await page.getByTestId("accept-signup").click();
  await expect(page).toHaveURL(/\/signup\?next=/);
  await page.getByLabel("Name").fill(invitee.name);
  await page.getByLabel("Email").fill(invitee.email);
  await page.getByLabel("Password").fill(invitee.password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/invitations\/accept\?token=/);
  await page.getByTestId("accept-invitation").click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByTestId("workspace-selector")).toHaveValue(/./);
  await expect(page.getByTestId("workspace-selector").locator("option:checked")).toHaveText(`${owner.name}'s workspace`);
  await page.goto("/team");
  await expect(page.getByTestId("member-row")).toHaveCount(2);
  await expect(page.getByTestId("invite-denied")).toContainText("a developer cannot manage members");
  for (const select of await page.getByTestId("member-role").all()) await expect(select).toBeDisabled();
  await page.goto("/developers");
  await expect(page.getByTestId("create-key")).toBeDisabled();
  await expect(page.getByTestId("create-key-reason")).toContainText("a developer cannot manage keys");
  await context.close();
});

test("the owner makes the invitee a viewer, then mints and revokes a key", async ({ page }) => {
  await signIn(page, owner.email, owner.password);
  await page.goto("/team");
  const row = page.getByTestId("member-row").filter({ hasText: invitee.email });
  await expect(row).toHaveCount(1);
  await row.getByTestId("member-role").selectOption("viewer");
  await expect(row.getByTestId("member-role")).toHaveValue("viewer");
  await page.reload();
  await expect(page.getByTestId("member-row").filter({ hasText: invitee.email }).getByTestId("member-role")).toHaveValue("viewer");

  await page.goto("/developers");
  await page.getByLabel("Name").fill("ci");
  await page.getByTestId("scope-messages:read").check();
  await page.getByTestId("create-key").click();
  const once = page.getByTestId("secret-once");
  await expect(once).toBeVisible();
  const key = await once.getByTestId("secret-value").textContent();
  expect(key).toMatch(/^hb_/);
  await expect(page.getByTestId("snippet-curl")).toContainText(key!);
  const keyRow = page.getByTestId("key-row").filter({ hasText: "ci" });
  await expect(keyRow.getByTestId("key-prefix")).toHaveText(`${key!.slice(0, 11)}…`);
  await page.reload();
  await expect(page.getByTestId("secret-once")).toHaveCount(0);
  await expect(page.getByTestId("keys")).not.toContainText(key!);
  await page.getByTestId("key-row").filter({ hasText: "ci" }).getByTestId("revoke-key").click();
  await page.getByTestId("confirm-revoke").click();
  await expect(page.getByTestId("key-row").filter({ hasText: "ci" })).toContainText("revoked");

  await page.goto("/settings");
  await expect(page.getByTestId("my-role")).toHaveText("owner");
  await expect(page.getByTestId("retention")).toContainText("7 days");
  await expect(page.getByTestId("limits")).toContainText("endpoints per application");
});

test("three failures notify the owner, who reads the notice", async ({ page }) => {
  await signIn(page, owner.email, owner.password);
  await page.goto("/applications");
  await page.getByLabel("Name").fill("Alerts");
  await page.getByRole("button", { name: "Create application" }).click();
  await page.getByTestId("application-link").first().click();
  await page.getByLabel("URL").fill(`${receiverURL}?status=400`);
  await page.getByLabel("Event types").fill("order.failed");
  await page.getByRole("button", { name: "Register endpoint" }).click();
  const secretPanel = page.getByTestId("secret-once").filter({ hasText: "Signing secret" });
  const secret = await secretPanel.getByTestId("secret-value").textContent();
  receiver = spawn(receiverBin, [], { env: { ...process.env, RECEIVER_SECRET: secret!, RECEIVER_ADDR: receiverAddr }, stdio: "inherit" });

  await page.getByTestId("endpoint-link").first().click();
  for (let i = 0; i < 3; i++) {
    await page.getByTestId("send-test").click();
    await expect(page.getByTestId("test-state")).toHaveText("failed", { timeout: 30_000 });
    await expect(page.getByTestId("test-result")).toContainText("HTTP 400");
  }

  await page.goto("/notifications");
  const notice = page.getByTestId("notice").filter({ hasText: "failure" });
  await expect(notice).toHaveCount(1, { timeout: 45_000 });
  await expect(notice).toHaveAttribute("data-unread", "true");
  await expect(page.getByTestId("unread-count")).toHaveText("1 unread");
  await notice.getByTestId("mark-read").click();
  await expect(page.getByTestId("unread-count")).toHaveText("0 unread");
  // The checkbox follows the URL, so the state changes after the navigation, not on the click itself.
  await page.getByTestId("unread-only").click();
  await expect(page).toHaveURL(/unread=1/);
  await expect(page.getByText("Nothing unread")).toBeVisible();

  await page.goto("/audit?resource_type=api_key");
  await expect(page.getByTestId("audit-row").first()).toContainText("api_key");
});
