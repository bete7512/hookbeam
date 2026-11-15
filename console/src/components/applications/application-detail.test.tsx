import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { environments, json, me, renderConsole, stubApi } from "@/test/console";

import { ApplicationDetail } from "./application-detail";

const nav = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn(), search: "" }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: nav.push, replace: nav.replace }),
  usePathname: () => "/applications/72",
  useSearchParams: () => new URLSearchParams(nav.search),
}));

const application = { id: 72, workspaceId: "ws_1", environmentId: 1, environmentKind: "development", name: "Checkout", createdBy: "u", responsePreviewsEnabled: false, createdAt: "2026-11-08T00:00:00Z", archivedAt: null };
const endpoint = {
  id: 12,
  applicationId: 72,
  url: "https://ledger.example/hooks",
  description: "Ledger",
  revision: 3,
  eventTypes: ["payment.succeeded"],
  headerNames: [],
  health: { state: "healthy", stale: false, consecutiveFailures: 0, observedAt: "2026-11-08T00:00:00Z" },
  createdAt: "2026-11-08T00:00:00Z",
};
const boundKey = { id: 7, name: "backend", prefix: "hb_9Xk2pQ4r", scopes: ["messages:write"], applicationId: 72, createdBy: "u", workspaceId: "ws_1", createdAt: "2026-11-08T00:00:00Z", updatedAt: "2026-11-08T00:00:00Z", lastUsedAt: null, expiresAt: null, revokedAt: null };
const otherKey = { ...boundKey, id: 8, name: "elsewhere", applicationId: 99 };

function routes(role: string, app = application) {
  return {
    "GET /v1/me": me(role),
    "GET /v1/environments": environments,
    "GET /v1/applications/72": app,
    "GET /v1/applications/72/endpoints": { items: [endpoint] },
    "GET /v1/applications/72/messages": { items: [], total: 0 },
    "GET /v1/api-keys": { items: [boundKey, otherKey], total: 2 },
    "DELETE /v1/api-keys/7": () => new Response(null, { status: 204 }),
    "POST /v1/applications/72/purge": { applicationId: 72, deletedEndpoints: 1, deletedMessages: 4, deletedDeliveries: 4, revokedKeys: 1 },
    "POST /v1/applications/72/endpoints": () => json(201, { endpoint: { ...endpoint, id: 13 }, signingSecret: "whsec_c2VjcmV0", secretVersion: 1 }),
  };
}

describe("ApplicationDetail", () => {
  beforeEach(() => {
    nav.search = "";
    nav.push.mockClear();
    nav.replace.mockClear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("summarises the application and lists its endpoints", async () => {
    stubApi(routes("owner"));
    renderConsole(<ApplicationDetail id={72} />);
    expect(await screen.findByTestId("application-name")).toHaveTextContent("Checkout");
    const link = await screen.findByTestId("endpoint-link");
    expect(link).toHaveAttribute("href", "/endpoints/12");
    expect(link).toHaveTextContent("https://ledger.example/hooks");
    expect(link).toHaveTextContent("payment.succeeded");
  });

  it("registers an endpoint with its event types and custom headers, then shows the secret once", async () => {
    const calls = stubApi(routes("owner"));
    renderConsole(<ApplicationDetail id={72} />);
    await waitFor(() => expect(screen.getByTestId("add-endpoint")).toBeEnabled());
    fireEvent.click(screen.getByTestId("add-endpoint"));
    fireEvent.change(screen.getByLabelText("URL"), { target: { value: "https://risk.example/events" } });
    const types = screen.getByLabelText("Event types");
    fireEvent.change(types, { target: { value: "dispute.opened" } });
    fireEvent.blur(types);
    fireEvent.click(screen.getByTestId("add-header"));
    fireEvent.change(screen.getByLabelText("Header 1 name"), { target: { value: "X-Api-Key" } });
    fireEvent.change(screen.getByLabelText("Header 1 value"), { target: { value: "s3cret" } });
    fireEvent.click(screen.getByRole("button", { name: "Register endpoint" }));
    expect(await screen.findByTestId("secret-value")).toHaveTextContent("whsec_c2VjcmV0");
    const sent = calls.find((c) => c.method === "POST" && c.path === "/v1/applications/72/endpoints");
    expect(sent?.body).toEqual({ url: "https://risk.example/events", eventTypes: ["dispute.opened"], headers: { "x-api-key": "s3cret" } });
  });

  it("lists only the keys bound to it and revokes one after a confirmation", async () => {
    nav.search = "tab=keys";
    const calls = stubApi(routes("owner"));
    renderConsole(<ApplicationDetail id={72} />);
    const table = await screen.findByTestId("keys");
    expect(within(table).getAllByTestId("key-row")).toHaveLength(1);
    expect(table).toHaveTextContent("backend");
    expect(table).not.toHaveTextContent("elsewhere");
    fireEvent.click(await screen.findByTestId("revoke-key"));
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    expect(screen.getByTestId("revoke-dialog")).toHaveTextContent("hb_9Xk2pQ4r…");
    fireEvent.click(screen.getByTestId("confirm-revoke"));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/v1/api-keys/7")).toBe(true));
  });

  it("gives a developer no way to create or revoke a key, with the reason", async () => {
    nav.search = "tab=keys";
    stubApi(routes("developer"));
    renderConsole(<ApplicationDetail id={72} />);
    expect(await screen.findByTestId("keys")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId("create-key")).toBeDisabled());
    expect(screen.getByText("a developer cannot manage keys")).toBeInTheDocument();
    expect(screen.queryByTestId("revoke-key")).toBeNull();
  });

  it("offers the permanent delete only once the application is archived", async () => {
    nav.search = "tab=settings";
    stubApi(routes("owner"));
    renderConsole(<ApplicationDetail id={72} />);
    await waitFor(() => expect(screen.getByTestId("archive-application")).toBeEnabled());
    expect(screen.getByTestId("delete-application")).toBeDisabled();
  });

  it("deletes an archived application only after its name is typed", async () => {
    nav.search = "tab=settings";
    const calls = stubApi(routes("owner", { ...application, archivedAt: "2026-11-09T00:00:00Z" } as never));
    renderConsole(<ApplicationDetail id={72} />);
    const button = await screen.findByTestId("delete-application");
    await waitFor(() => expect(button).toBeEnabled());
    expect(screen.queryByTestId("archive-application")).toBeNull();
    fireEvent.click(button);
    const confirm = screen.getByTestId("confirm-delete");
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByTestId("confirm-name"), { target: { value: "checkout" } });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByTestId("confirm-name"), { target: { value: "Checkout" } });
    fireEvent.click(confirm);
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith("/applications"));
    expect(calls.some((c) => c.method === "POST" && c.path === "/v1/applications/72/purge")).toBe(true);
  });
});
