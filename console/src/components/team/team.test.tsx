import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Team } from "./team";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function stub(role: string) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const path = new URL(typeof input === "string" ? input : input.url, "http://console.test").pathname;
      if (path === "/v1/me") return json(200, { userId: "u_me", workspaceId: "ws_1", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"], role });
      if (path === "/v1/members")
        return json(200, {
          items: [
            { userId: "u_owner", role: "owner", joinedAt: "2026-11-01T00:00:00Z", name: "Owner", email: "owner@example.com" },
            { userId: "u_me", role, joinedAt: "2026-11-02T00:00:00Z", name: "Me", email: "me@example.com" },
          ],
        });
      if (path === "/v1/auth/org/ws_1/invitations") return json(200, { data: { list: [], total: 0 } });
      return json(404, {});
    }),
  );
}

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <Team />
    </QueryClientProvider>,
  );
}

describe("Team", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("shows a viewer the list read-only, with the reason", async () => {
    stub("viewer");
    wrap();
    const selects = await screen.findAllByTestId("member-role");
    expect(selects).toHaveLength(2);
    for (const s of selects) expect(s).toBeDisabled();
    expect(screen.getAllByTestId("role-reason")[0]).toHaveTextContent("a viewer cannot manage members");
    expect(screen.getByTestId("invite-denied")).toHaveTextContent("a viewer cannot manage members");
    expect(screen.queryByTestId("remove-member")).toBeNull();
  });

  it("lets an admin change members but not owners", async () => {
    stub("admin");
    wrap();
    const selects = await screen.findAllByTestId("member-role");
    expect(selects[0]).toBeDisabled();
    expect(screen.getByTestId("role-reason")).toHaveTextContent("a admin cannot manage owners");
    expect(selects[1]).toBeEnabled();
    expect(screen.getByTestId("send-invite")).toBeEnabled();
    expect(selects[1].querySelector('option[value="owner"]')).toBeNull();
  });

  it("offers an owner the owner role and removal", async () => {
    stub("owner");
    wrap();
    const selects = await screen.findAllByTestId("member-role");
    expect(selects[0]).toBeEnabled();
    expect(selects[0].querySelector('option[value="owner"]')).not.toBeNull();
    expect(screen.getAllByTestId("remove-member")).toHaveLength(1);
  });
});
