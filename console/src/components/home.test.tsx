import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Home } from "./home";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function stubFetch(routes: Record<string, () => Response>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const path = new URL(typeof input === "string" ? input : input.url, "http://console.test").pathname;
      return routes[path]?.() ?? json(404, {});
    }),
  );
}

function renderHome() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <Home />
    </QueryClientProvider>,
  );
}

describe("Home", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("shows the readiness of the API and a sign-in link without a session", async () => {
    stubFetch({
      "/readyz": () => json(200, { status: "ok", checks: { db: "ok", delivery: "ok" } }),
      "/v1/me": () => json(401, { error: "UNAUTHORIZED", message: "authentication required", code: 401 }),
    });
    renderHome();
    expect(await screen.findByText("delivery")).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Sign in" })).toBeInTheDocument();
  });

  it("shows the caller when the cookie is accepted", async () => {
    stubFetch({
      "/readyz": () => json(200, { status: "ok", checks: {} }),
      "/v1/me": () => json(200, { userId: "u_ada", workspaceId: "ws_1", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"] }),
    });
    renderHome();
    expect(await screen.findByTestId("user-id")).toHaveTextContent("u_ada");
  });

  it("names an unreachable API with the unavailable state, never an empty one", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json(503, { error: "SERVICE_UNAVAILABLE", message: "down", code: 503 })));
    renderHome();
    expect((await screen.findAllByText("Service unavailable")).length).toBeGreaterThan(0);
  });
});
