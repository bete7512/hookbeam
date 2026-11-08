import { expect, type Page } from "@playwright/test";

export const mailpit = process.env.MAILPIT_URL ?? "http://localhost:8025";

// uniqueAccount returns credentials no earlier run used; the display name is unique too, because goauth derives
// the workspace slug from it.
export function uniqueAccount() {
  const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;
  return { name: `E2E ${stamp}`, email: `e2e-${stamp}@example.com`, password: "Passw0rd!long" };
}

interface MailpitMessage {
  ID: string;
  Subject: string;
  To: { Address: string }[];
}

// latestEmail polls Mailpit for the newest message to the address whose subject contains the given text.
export async function latestEmail(to: string, subjectContains: string): Promise<{ id: string; text: string }> {
  for (let attempt = 0; attempt < 30; attempt++) {
    const res = await fetch(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:${to}`)}&limit=20`);
    const body = (await res.json()) as { messages: MailpitMessage[] };
    const hit = (body.messages ?? []).find((m) => m.Subject.toLowerCase().includes(subjectContains.toLowerCase()));
    if (hit) {
      const full = await fetch(`${mailpit}/api/v1/message/${hit.ID}`);
      const msg = (await full.json()) as { Text: string };
      return { id: hit.ID, text: msg.Text };
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`no email to ${to} with subject containing "${subjectContains}"`);
}

// firstLink finds the first URL in the text whose path contains the given fragment.
export function firstLink(text: string, containing: string): string {
  const match = text.match(/https?:\/\/[^\s"<>]+/g)?.find((u) => u.includes(containing));
  if (!match) throw new Error(`no link containing "${containing}" in:\n${text}`);
  return match;
}

export async function signIn(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/overview|\/account/);
}
