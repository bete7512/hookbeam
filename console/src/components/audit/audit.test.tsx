import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Audit, parseAuditFilters, serializeAuditFilters } from "./audit";

vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams("resource_type=endpoint"),
  useRouter: () => ({ replace: vi.fn() }),
  usePathname: () => "/audit",
}));

describe("audit filters", () => {
  it("round-trip through the URL", () => {
    const f = parseAuditFilters(new URLSearchParams("actor_id=u_1&resource_type=endpoint&resource_id=5&offset=50"));
    expect(f.resourceType).toBe("endpoint");
    expect(serializeAuditFilters(f)).toBe("?actor_id=u_1&resource_type=endpoint&resource_id=5&offset=50");
  });
});

describe("Audit", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("renders a direct 403 as the permission-denied state", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "FORBIDDEN", message: "a viewer cannot read audit", code: 403 }), { status: 403, headers: { "content-type": "application/json" } })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <Audit />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/a viewer cannot read audit/)).toBeInTheDocument();
  });
});
