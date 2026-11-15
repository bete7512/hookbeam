import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { environments, me, renderConsole, stubApi } from "@/test/console";

import { EndpointDetail } from "./endpoint-detail";

const nav = vi.hoisted(() => ({ replace: vi.fn(), search: "" }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: nav.replace }),
  usePathname: () => "/endpoints/12",
  useSearchParams: () => new URLSearchParams(nav.search),
}));

const canary = "Bearer canary-do-not-render";
const endpoint = {
  id: 12,
  applicationId: 72,
  url: "https://ledger.example/hooks",
  description: "Ledger",
  revision: 3,
  eventTypes: ["payment.succeeded"],
  headerNames: ["authorization", "x-tenant"],
  health: { state: "unhealthy", stale: false, consecutiveFailures: 4, observedAt: "2026-11-08T00:00:00Z" },
  pausedAt: null,
  archivedAt: null,
  createdAt: "2026-11-08T00:00:00Z",
  waiting: 2,
  secrets: [{ version: 1, fingerprint: "ab12cd34ef56ab78", createdAt: "2026-11-08T00:00:00Z", retireAt: null }],
};
const delivery = { id: 501, messageId: 40, endpointId: 12, state: "retry_scheduled", attemptCount: 2, endpointRevision: 3, deadlineAt: "2026-11-09T00:00:00Z", createdAt: "2026-11-08T00:00:00Z", dueAt: "2026-11-08T00:05:00Z" };
const deliveries = {
  items: [
    { delivery, message: { id: 40, webhookId: "msg_abc", type: "payment.succeeded", externalId: "pay_1", acceptedAt: "2026-11-08T00:00:00Z" }, lastAttempt: { number: 2, startedAt: "2026-11-08T00:01:00Z", latencyMs: 41, statusCode: 503 } },
    { delivery: { ...delivery, id: 500, state: "queued", attemptCount: 0, dueAt: null }, message: { id: 39, webhookId: "msg_def", type: "refund.issued", acceptedAt: "2026-11-08T00:00:00Z" }, lastAttempt: null },
  ],
  total: 2,
};

function routes(role: string) {
  return {
    "GET /v1/me": me(role),
    "GET /v1/environments": environments,
    "GET /v1/endpoints/12": endpoint,
    "GET /v1/applications/72": { id: 72, workspaceId: "ws_1", environmentId: 1, environmentKind: "development", name: "Checkout", createdBy: "u", responsePreviewsEnabled: false, createdAt: "2026-11-08T00:00:00Z" },
    "GET /v1/endpoints/12/deliveries": deliveries,
    "PATCH /v1/endpoints/12": { ...endpoint, revision: 4 },
    "POST /v1/endpoints/12/pause": { ...endpoint, pausedAt: "2026-11-08T00:10:00Z" },
  };
}

describe("EndpointDetail", () => {
  beforeEach(() => {
    nav.search = "";
    nav.replace.mockClear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("shows the destination, its health and what was sent to it", async () => {
    stubApi(routes("owner"));
    renderConsole(<EndpointDetail id={12} />);
    expect(await screen.findByTestId("endpoint-url")).toHaveTextContent("https://ledger.example/hooks");
    expect(screen.getByTestId("health-badge")).toHaveTextContent("unhealthy");
    expect(screen.getByTestId("waiting")).toHaveTextContent("2");
    const rows = await screen.findAllByTestId("endpoint-delivery-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("payment.succeeded");
    expect(rows[0]).toHaveTextContent("retry_scheduled");
    expect(rows[0]).toHaveTextContent("HTTP 503");
    expect(rows[0]).toHaveTextContent("41 ms");
    expect(within(rows[0]).getAllByRole("link")[0]).toHaveAttribute("href", "/events/40/deliveries/501");
    expect(rows[1]).toHaveTextContent("not attempted");
    expect(await screen.findByText("Checkout")).toHaveAttribute("href", "/applications/72");
  });

  it("selects a tab through the URL", async () => {
    stubApi(routes("owner"));
    renderConsole(<EndpointDetail id={12} />);
    fireEvent.click(await screen.findByTestId("tab-secrets"));
    expect(nav.replace).toHaveBeenCalledWith("/endpoints/12?tab=secrets", { scroll: false });
  });

  it("saves header changes as a merge under the loaded revision and never renders a stored value", async () => {
    nav.search = "tab=configuration";
    const calls = stubApi(routes("owner"));
    const { container } = renderConsole(<EndpointDetail id={12} />);
    const names = await screen.findByTestId("header-names");
    expect(names).toHaveTextContent("authorization");
    expect(names).toHaveTextContent("value hidden");
    await waitFor(() => expect(screen.getByTestId("save-endpoint")).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Remove header x-tenant" }));
    fireEvent.click(screen.getByTestId("add-header"));
    fireEvent.change(screen.getByLabelText("Header 1 name"), { target: { value: "Authorization" } });
    fireEvent.change(screen.getByLabelText("Header 1 value"), { target: { value: canary } });
    fireEvent.click(screen.getByTestId("save-endpoint"));
    expect(await screen.findByText("Saved as revision 4.")).toBeInTheDocument();
    const sent = calls.find((c) => c.method === "PATCH");
    expect(sent?.body).toEqual({
      url: "https://ledger.example/hooks",
      description: "Ledger",
      eventTypes: ["payment.succeeded"],
      headers: { "x-tenant": null, authorization: canary },
      expectedRevision: 3,
    });
    // Once saved, the form goes back to names only when the next revision loads; nothing echoes the value in text.
    expect(container.textContent).not.toContain(canary);
  });

  it("pauses only after the confirmation that says what holds", async () => {
    const calls = stubApi(routes("owner"));
    renderConsole(<EndpointDetail id={12} />);
    await waitFor(() => expect(screen.getByTestId("pause")).toBeEnabled());
    fireEvent.click(screen.getByTestId("pause"));
    expect(calls.some((c) => c.path.endsWith("/pause"))).toBe(false);
    expect(screen.getByTestId("pause-dialog")).toHaveTextContent("2 deliveries are waiting");
    fireEvent.click(screen.getByTestId("confirm-pause"));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/v1/endpoints/12/pause")).toBe(true));
  });

  it("lets a viewer read but not act", async () => {
    nav.search = "tab=configuration";
    stubApi(routes("viewer"));
    renderConsole(<EndpointDetail id={12} />);
    expect((await screen.findAllByText("a viewer cannot manage endpoints")).length).toBeGreaterThan(0);
    expect(screen.getByTestId("save-endpoint")).toBeDisabled();
    expect(screen.getByTestId("send-test")).toBeDisabled();
    expect(screen.getByTestId("pause")).toBeDisabled();
    expect(screen.getByTestId("archive-endpoint")).toBeDisabled();
  });
});
