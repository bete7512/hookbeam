import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Home } from "./home";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
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

  it("shows the login form when there is no session and the readiness of the API", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request | string) => {
        const path = new URL(typeof input === "string" ? input : input.url, "http://console.test").pathname;
        if (path === "/readyz") return json(200, { status: "ok", checks: { db: "ok", delivery: "ok" } });
        if (path === "/v1/me") return json(401, { error: "UNAUTHORIZED", message: "authentication required", code: 401 });
        return json(404, {});
      }),
    );
    renderHome();
    expect(await screen.findByLabelText("Email")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(await screen.findByText("delivery")).toBeInTheDocument();
  });

  it("shows the caller when the cookie is accepted", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request | string) => {
        const path = new URL(typeof input === "string" ? input : input.url, "http://console.test").pathname;
        if (path === "/readyz") return json(200, { status: "ok", checks: {} });
        if (path === "/v1/me") return json(200, { userId: "u_ada", workspaceId: "ws_1", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"] });
        return json(404, {});
      }),
    );
    renderHome();
    expect(await screen.findByTestId("user-id")).toHaveTextContent("u_ada");
    expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument();
  });

  it("names an unreachable API instead of showing an empty state", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json(503, { error: "SERVICE_UNAVAILABLE", message: "down", code: 503 })));
    renderHome();
    expect(await screen.findByText("The API is not reachable right now.")).toBeInTheDocument();
  });
});
