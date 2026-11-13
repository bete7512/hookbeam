import { describe, expect, it } from "vitest";

import { verifyWebhook } from "./verify-webhook";

// The Standard Webhooks specification's vector, the same one pkg/webhooks tests in Go.
const secret = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw";
const id = "msg_p5jXN8AQM9LWM0D4loKWxJek";
const body = Buffer.from('{"test": 2432232314}');
const timestamp = "1614265330";
const signature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=";
const now = 1614265330;

describe("verifyWebhook", () => {
  const ok = { secrets: [secret], id, timestamp, signature, body, now };

  it("accepts the specification's vector", () => {
    expect(verifyWebhook(ok)).toBe(true);
  });

  it("verifies the request shown on the Life of a message page", () => {
    expect(
      verifyWebhook({
        secrets: ["whsec_AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="],
        id: "msg_2bBrkQ9mVv0yXW4sJxTq3A",
        timestamp: "1794562867",
        signature: "v1,aUuBicZx08+nmfB/bcHNWrwiBG5oLqVaEFfUpmpvKJ4=",
        body: Buffer.from('{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817","carrier":"dhl"}}'),
        now: 1794562867,
      }),
    ).toBe(true);
  });

  it("accepts any of several signatures and secrets, as during a rotation", () => {
    const other = "whsec_" + Buffer.alloc(32, 7).toString("base64");
    expect(verifyWebhook({ ...ok, secrets: [other, secret], signature: `v1,AAAA ${signature}` })).toBe(true);
  });

  it.each([
    ["a changed body", { body: Buffer.from('{"test":2432232314}') }],
    ["another secret", { secrets: ["whsec_" + Buffer.alloc(32, 1).toString("base64")] }],
    ["a secret without the whsec_ prefix", { secrets: [secret.slice("whsec_".length)] }],
    ["a timestamp six minutes old", { now: now + 6 * 60 }],
    ["a timestamp six minutes ahead", { now: now - 6 * 60 }],
    ["a malformed timestamp", { timestamp: "yesterday" }],
    ["a missing signature header", { signature: null }],
    ["another signature version", { signature: signature.replace("v1,", "v2,") }],
  ])("refuses %s", (_, change) => {
    expect(verifyWebhook({ ...ok, ...change })).toBe(false);
  });
});
