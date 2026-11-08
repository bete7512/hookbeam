import { describe, expect, it, vi } from "vitest";

import { login, myWorkspaces, providers } from "./auth";
import { ApiError, errorFromResponse } from "./errors";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

describe("goauth helpers", () => {
  it("unwraps the data envelope and sends the cookie", async () => {
    const fetcher = vi.fn(async () => json(200, { data: [{ id: "ws_1", name: "Ada's workspace", slug: "adas", owner_id: "u_ada", active: true }] }));
    const list = await myWorkspaces(fetcher as unknown as typeof fetch);
    expect(list[0].name).toBe("Ada's workspace");
    const [url, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/v1/auth/org/my");
    expect(init.credentials).toBe("include");
  });

  it("turns goauth's error envelope into an ApiError and softens a wrong password", async () => {
    const fetcher = vi.fn(async () => json(401, { data: { code: "INVALID_CREDENTIALS", message: "invalid credentials", status_code: 401 } }));
    await expect(login({ email: "a@b.c", password: "x" }, fetcher as unknown as typeof fetch)).rejects.toMatchObject({ status: 401, message: "Wrong email or password." });
  });

  it("keeps only enabled providers", async () => {
    const fetcher = vi.fn(async () => json(200, { data: { providers: [{ name: "google", enabled: true }, { name: "github", enabled: false }] } }));
    expect(await providers(fetcher as unknown as typeof fetch)).toEqual([{ name: "google", enabled: true }]);
  });

  it("reads the control plane's error envelope too", async () => {
    const err = await errorFromResponse(json(422, { error: "VALIDATION_FAILED", message: "validation failed", code: 422, fields: { url: "must be https" } }));
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe("VALIDATION_FAILED");
    expect(err.fields.url).toBe("must be https");
  });
});
