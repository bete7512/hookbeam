import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { environments, json, me, renderConsole, stubApi } from "@/test/console";

import { Applications } from "./applications";

const nav = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: nav.push }) }));

const base = { workspaceId: "ws_1", environmentId: 1, environmentKind: "development", createdBy: "u", responsePreviewsEnabled: false, createdAt: "2026-11-08T00:00:00Z" };
const checkout = { ...base, id: 72, name: "Checkout", archivedAt: null };
const legacy = { ...base, id: 60, name: "Legacy billing", archivedAt: "2026-11-09T00:00:00Z" };
const limits = {
  limits: { backlogPerWorkspace: 10000, endpointsPerApplication: 20, fanoutPerMessage: 20 },
  retention: { auditSeconds: 1, idempotencyWindowSeconds: 1, payloadSeconds: 1 },
  usage: { backlog: 0, applications: [{ applicationId: 72, name: "Checkout", endpoints: 2 }] },
};

function routes(role: string) {
  return {
    "GET /v1/me": me(role),
    "GET /v1/environments": environments,
    "GET /v1/environments/1/applications": { items: [legacy, checkout] },
    "GET /v1/workspaces/current/limits": limits,
    "POST /v1/environments/1/applications": () => json(201, { ...base, id: 73, name: "Payouts", archivedAt: null }),
  };
}

describe("Applications", () => {
  beforeEach(() => nav.push.mockClear());
  afterEach(() => vi.unstubAllGlobals());

  it("shows active applications, and archived ones behind their filter with the delete action", async () => {
    stubApi(routes("owner"));
    renderConsole(<Applications />);
    const row = await screen.findByTestId("application-row");
    expect(row).toHaveTextContent("Checkout");
    expect(row).toHaveTextContent("2 endpoints");
    expect(await screen.findByTestId("row-archive")).toBeInTheDocument();
    expect(screen.queryByText("Legacy billing")).toBeNull();
    fireEvent.click(screen.getByTestId("filter-archived"));
    expect(screen.getByTestId("application-row")).toHaveTextContent("Legacy billing");
    expect(screen.getByTestId("row-delete")).toBeInTheDocument();
    expect(screen.queryByTestId("row-archive")).toBeNull();
  });

  it("filters by name", async () => {
    stubApi(routes("owner"));
    renderConsole(<Applications />);
    await screen.findByTestId("application-row");
    fireEvent.change(screen.getByLabelText("Search applications"), { target: { value: "zzz" } });
    expect(screen.queryByTestId("application-row")).toBeNull();
    expect(screen.getByText("No application matches")).toBeInTheDocument();
  });

  it("creates an application and opens it", async () => {
    const calls = stubApi(routes("owner"));
    renderConsole(<Applications />);
    await screen.findByTestId("application-row");
    await waitFor(() => expect(screen.getByTestId("new-application")).toBeEnabled());
    fireEvent.click(screen.getByTestId("new-application"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: " Payouts " } });
    fireEvent.click(screen.getByRole("button", { name: "Create application" }));
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith("/applications/73"));
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "Payouts" });
  });

  it("gives a developer no create, archive or delete, with the reason", async () => {
    stubApi(routes("developer"));
    renderConsole(<Applications />);
    await screen.findByTestId("application-row");
    await waitFor(() => expect(screen.getByText("a developer cannot manage endpoints")).toBeInTheDocument());
    expect(screen.getByTestId("new-application")).toBeDisabled();
    expect(screen.queryByTestId("row-archive")).toBeNull();
  });
});
