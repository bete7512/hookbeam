# 0001. The OpenAPI spec is the only way to add a route

Date: 2026-09-14 · Status: accepted

## Context
Server, generated clients, console and docs must share one API contract, and the four must not drift apart.

## Options
- **Code first**: write the Go handlers, then generate the spec from comments on them. Rejected: comments are not compiled, so the published spec drifts from the code and nobody notices.
- **Spec first with the strict generated server**: write the spec, generate typed request and response objects, handlers return those objects. Rejected: the handler no longer sees `http.ResponseWriter`, which we need for headers, cookies, streaming and raw bodies.
- **Spec first with the non-strict generated server**: write the spec, generate the router, parameter parsing and DTOs; handlers still get `(w, r, params)` and write the response themselves. Chosen.

## Decision
We write the spec first in `<service>/openapi/` and generate the router, parameter parsing and DTOs from it. A route exists only when it is in the spec, and the build fails until a handler implements the new operation.

## Consequences
- `make gen-check` fails any change that edits the spec without regenerating the code.
- Authentication is declared per operation in the spec's `security` block and enforced by one middleware, never in handlers.
- The generator does not check which status a handler writes; the per-route tests do.
