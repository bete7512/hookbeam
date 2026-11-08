import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AttemptRow } from "./delivery-detail";
import { EventDetail } from "./event-detail";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function wrap(ui: React.ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe("AttemptRow", () => {
  it("renders the response preview as text, never as markup", async () => {
    const { getByRole } = wrap(
      <AttemptRow attempt={{ id: 1, deliveryId: 5, number: 1, startedAt: "2026-11-08T00:00:00Z", latencyMs: 12, secretVersion: 1, endpointRevision: 1, requestUrl: "https://r", statusCode: 503, responsePreview: "<script>alert(1)</script><b>bold</b>" }} />,
    );
    getByRole("button").click();
    const preview = await screen.findByTestId("preview");
    expect(preview.textContent).toBe("<script>alert(1)</script><b>bold</b>");
    expect(preview.querySelector("script")).toBeNull();
    expect(preview.querySelector("b")).toBeNull();
  });
});

describe("EventDetail", () => {
  afterEach(() => vi.unstubAllGlobals());

  function stub(role: string, payloadExpiresAt: string) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request | string) => {
        const path = new URL(typeof input === "string" ? input : input.url, "http://console.test").pathname;
        if (path === "/v1/me") return json(200, { userId: "u", workspaceId: "ws_1", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"], role });
        if (path === "/v1/messages/42")
          return json(200, {
            message: { id: 42, webhookId: "msg_x", applicationId: 7, environmentId: 2, environmentKind: "development", type: "order.paid", externalId: null, acceptedAt: "2026-11-08T00:00:00Z", deliveryCount: 1, payloadExpiresAt },
            deliveries: [{ id: 5, messageId: 42, endpointId: 3, state: "failed", attemptCount: 8, endpointRevision: 2, dueAt: null, deadlineAt: "2026-11-09T00:00:00Z", failureReason: "attempts_exhausted", replayOfDeliveryId: null, createdAt: "2026-11-08T00:00:00Z", completedAt: "2026-11-08T01:00:00Z" }],
          });
        return json(404, {});
      }),
    );
  }

  it("hides the payload tab from a viewer and says why", async () => {
    stub("viewer", "2099-01-01T00:00:00Z");
    wrap(<EventDetail id={42} />);
    expect(await screen.findByTestId("event-type")).toHaveTextContent("order.paid");
    expect(screen.queryByTestId("payload-tab")).toBeNull();
    expect(screen.getByTestId("payload-denied")).toHaveTextContent("a viewer cannot read payloads");
  });

  it("offers the payload tab to a developer", async () => {
    stub("developer", "2099-01-01T00:00:00Z");
    wrap(<EventDetail id={42} />);
    expect(await screen.findByTestId("payload-tab")).toBeInTheDocument();
  });

  it("marks an expired payload in the retention line", async () => {
    stub("developer", "2020-01-01T00:00:00Z");
    wrap(<EventDetail id={42} />);
    expect(await screen.findByTestId("retention")).toHaveTextContent("expired");
  });
});
