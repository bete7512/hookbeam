import { createHmac, timingSafeEqual } from "node:crypto";

// verifyWebhook checks a Hookbeam delivery in Node.js: the raw body as received, the three webhook headers, any
// signature under any of your active secrets (two during a rotation), and at most five minutes of clock skew.
// Verify before parsing the body; parsing and re-serialising would change the bytes the signature covers.
export function verifyWebhook(input: {
  secrets: string[]; // whsec_… values
  id: string | null; // the webhook-id header
  timestamp: string | null; // the webhook-timestamp header
  signature: string | null; // the webhook-signature header
  body: Buffer; // the raw request body
  now?: number; // unix seconds; the clock by default
}): boolean {
  const { id, timestamp, signature } = input;
  if (!id || !timestamp || !signature) return false;
  const unix = Number(timestamp);
  const now = input.now ?? Math.floor(Date.now() / 1000);
  if (!Number.isSafeInteger(unix) || Math.abs(now - unix) > 5 * 60) return false;

  const signed = Buffer.concat([Buffer.from(`${id}.${unix}.`), input.body]);
  const offered = signature
    .split(" ")
    .filter((s) => s.startsWith("v1,"))
    .map((s) => Buffer.from(s.slice(3), "base64"));

  return input.secrets.some((secret) => {
    if (!secret.startsWith("whsec_")) return false;
    const key = Buffer.from(secret.slice("whsec_".length), "base64");
    const expected = createHmac("sha256", key).update(signed).digest();
    return offered.some((sig) => sig.length === expected.length && timingSafeEqual(sig, expected));
  });
}
