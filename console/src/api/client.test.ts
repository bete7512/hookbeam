import { afterEach, describe, expect, it, vi } from "vitest";

import { createApi } from "./client";

describe("createApi", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("calls the API on the same origin with the session cookie", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ userId: "u_ada", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"] }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    const { data, response } = await createApi().GET("/v1/me");

    expect(response.status).toBe(200);
    expect(data?.userId).toBe("u_ada");
    const [request] = fetchMock.mock.calls[0] as unknown as [Request];
    expect(new URL(request.url).pathname).toBe("/v1/me");
    expect(request.credentials).toBe("include");
  });

  it("returns the error body instead of throwing", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "UNAUTHORIZED", message: "authentication required", code: 401 }), { status: 401, headers: { "content-type": "application/json" } })));

    const { data, error, response } = await createApi().GET("/v1/me");

    expect(response.status).toBe(401);
    expect(data).toBeUndefined();
    expect(error?.error).toBe("UNAUTHORIZED");
  });
});
