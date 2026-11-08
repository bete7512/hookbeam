import createClient from "openapi-fetch";

import type { paths } from "./schema";

// createApi returns a typed client over the generated OpenAPI schema. The console runs behind deploy/proxy on the
// same origin as the API, so the default base URL is the page's own origin and the session cookie travels with
// every call. openapi-fetch needs an absolute URL, hence the origin rather than "/".
export function createApi(baseUrl = defaultBaseUrl()) {
  // fetch is looked up per call rather than captured at creation, so a page can create the client at import time
  // and tests can still stub the global.
  return createClient<paths>({ baseUrl, credentials: "include", fetch: (input: Request) => globalThis.fetch(input) });
}

function defaultBaseUrl() {
  return typeof window === "undefined" ? "http://localhost:3000" : window.location.origin;
}

export const api = createApi();

export type Api = ReturnType<typeof createApi>;
