import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { EnvironmentProvider } from "@/lib/environment";

import { Developers } from "./developers";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

const stored = { id: 7, name: "backend", prefix: "hb_9Xk2pQ4r", scopes: ["messages:write"], applicationId: null, createdBy: "u", workspaceId: "ws_1", createdAt: "2026-11-08T00:00:00Z", updatedAt: "2026-11-08T00:00:00Z", lastUsedAt: null, expiresAt: null, revokedAt: null };

function stub(role: string, keys: unknown[] = []) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const req = typeof input === "string" ? new Request(input) : input;
      const path = new URL(req.url, "http://console.test").pathname;
      if (path === "/v1/me") return json(200, { userId: "u", workspaceId: "ws_1", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"], role });
      if (path === "/v1/environments") return json(200, { items: [{ id: 1, name: "Development", kind: "development", createdAt: "2026-11-08T00:00:00Z" }] });
      if (path === "/v1/environments/1/applications") return json(200, { items: [] });
      if (path === "/v1/api-keys" && req.method === "POST") return json(201, { ...stored, key: "hb_9Xk2pQ4rT1uV8wX3yZ5aB7cD9eF1gH2jK4mN6pQ8sT0v" });
      if (path === "/v1/api-keys") return json(200, { items: keys, total: keys.length });
      return json(404, {});
    }),
  );
}

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <EnvironmentProvider workspaceId="ws_1">
        <Developers />
      </EnvironmentProvider>
    </QueryClientProvider>,
  );
}

describe("Developers", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("disables creation for a developer with the reason", async () => {
    stub("developer", [stored]);
    wrap();
    expect(await screen.findByTestId("create-key")).toBeDisabled();
    expect(screen.getByTestId("create-key-reason")).toHaveTextContent("a developer cannot manage keys");
    expect(screen.queryByTestId("revoke-key")).toBeNull();
  });

  it("shows the key once after creation; the list carries only the prefix", async () => {
    stub("owner", [stored]);
    wrap();
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "backend" } });
    fireEvent.click(screen.getByTestId("create-key"));
    const once = await screen.findByTestId("secret-once");
    expect(once).toHaveTextContent("hb_9Xk2pQ4rT1uV8wX3yZ5aB7cD9eF1gH2jK4mN6pQ8sT0v");
    expect(screen.getByTestId("snippet-curl")).toHaveTextContent("Authorization: Bearer hb_9Xk2pQ4rT1uV8wX3yZ5aB7cD9eF1gH2jK4mN6pQ8sT0v");
    expect(screen.getByTestId("key-prefix")).toHaveTextContent("hb_9Xk2pQ4r…");
    expect(screen.getByTestId("keys")).not.toHaveTextContent("hb_9Xk2pQ4rT1uV8wX3yZ5aB7cD9eF1gH2jK4mN6pQ8sT0v");
  });
});
