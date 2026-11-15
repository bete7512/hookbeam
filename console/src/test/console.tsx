import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";

import { EnvironmentProvider } from "@/lib/environment";

export function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

export interface Call {
  method: string;
  path: string;
  body: unknown;
}

// stubApi answers fetch from `routes` ("METHOD /path" → a body, or a function of the call) and records every call,
// so a test can assert what a screen sent. An unknown route is a 404.
export function stubApi(routes: Record<string, unknown | ((call: Call) => Response)>) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string, init?: RequestInit) => {
      const req = typeof input === "string" ? new Request(new URL(input, "http://console.test"), init) : input;
      const url = new URL(req.url, "http://console.test");
      const text = req.method === "GET" || req.method === "HEAD" ? "" : await req.text();
      const call = { method: req.method, path: url.pathname, body: text ? JSON.parse(text) : undefined };
      calls.push(call);
      const route = routes[`${req.method} ${url.pathname}`];
      if (route === undefined) return json(404, { error: "RESOURCE_NOT_FOUND", message: "not found" });
      return typeof route === "function" ? (route as (call: Call) => Response)(call) : json(200, route);
    }),
  );
  return calls;
}

export function me(role: string) {
  return { userId: "u", workspaceId: "ws_1", authenticatedAt: "2026-11-08T00:00:00Z", expiresAt: null, methods: ["password"], role };
}

export const environments = { items: [{ id: 1, name: "Development", kind: "development", createdAt: "2026-11-08T00:00:00Z" }] };

export function renderConsole(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <EnvironmentProvider workspaceId="ws_1">{ui}</EnvironmentProvider>
    </QueryClientProvider>,
  );
}
