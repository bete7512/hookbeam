import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { DeliveryDetail } from "./delivery-detail";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function stub(role: string, state: string, payloadExpiresAt: string) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const path = new URL(typeof input === "string" ? input : input.url, "http://console.test").pathname;
      if (path === "/v1/me") return json(200, { userId: "u", workspaceId: "ws_1", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"], role });
      if (path === "/v1/messages/42")
        return json(200, {
          message: { id: 42, webhookId: "msg_x", applicationId: 7, environmentId: 2, environmentKind: "development", type: "order.paid", externalId: null, acceptedAt: "2026-11-08T00:00:00Z", deliveryCount: 1, payloadExpiresAt },
          deliveries: [{ id: 5, messageId: 42, endpointId: 3, state, attemptCount: 8, endpointRevision: 2, dueAt: null, deadlineAt: "2026-11-09T00:00:00Z", failureReason: state === "failed" ? "attempts_exhausted" : null, replayOfDeliveryId: null, createdAt: "2026-11-08T00:00:00Z", completedAt: "2026-11-08T01:00:00Z" }],
        });
      if (path === "/v1/deliveries/5/attempts") return json(200, { items: [] });
      return json(404, {});
    }),
  );
}

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DeliveryDetail messageId={42} deliveryId={5} />
    </QueryClientProvider>,
  );
}

describe("DeliveryDetail actions", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("disables replay for a viewer with the reason", async () => {
    stub("viewer", "failed", "2099-01-01T00:00:00Z");
    wrap();
    expect(await screen.findByTestId("replay")).toBeDisabled();
    expect(screen.getByTestId("replay-reason")).toHaveTextContent("a viewer cannot replay or cancel deliveries");
  });

  it("disables replay once the payload expired", async () => {
    stub("developer", "failed", "2020-01-01T00:00:00Z");
    wrap();
    expect(await screen.findByTestId("replay")).toBeDisabled();
    expect(screen.getByTestId("replay-reason")).toHaveTextContent("the payload has expired");
  });

  it("lets a developer replay a failed delivery after confirming", async () => {
    stub("developer", "failed", "2099-01-01T00:00:00Z");
    wrap();
    const replay = await screen.findByTestId("replay");
    expect(replay).toBeEnabled();
    replay.click();
    expect(await screen.findByTestId("confirm-dialog")).toHaveTextContent("revision 2");
    expect(screen.getByTestId("confirm-dialog")).toHaveTextContent("attempts_exhausted");
    expect(screen.getByRole("note")).toHaveTextContent("processes the event twice");
  });

  it("a succeeded delivery cannot be replayed or cancelled", async () => {
    stub("owner", "succeeded", "2099-01-01T00:00:00Z");
    wrap();
    expect(await screen.findByTestId("replay")).toBeDisabled();
    expect(screen.getByTestId("replay-reason")).toHaveTextContent("a succeeded delivery cannot be replayed");
    expect(screen.getByTestId("cancel")).toBeDisabled();
  });
});
