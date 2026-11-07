# 0004. Workspaces are goauth organizations; API keys and scopes are Hookbeam's

Date: 2026-11-07 · Status: accepted

## Context
Every resource belongs to a workspace, humans join workspaces with roles, and sender backends need a credential that is not a user.

## Options
- **Hookbeam-owned workspaces and keys**: our own tables for workspaces, members and invitations. Rejected: goauth already validates membership and the active organization on every request, so we would keep two ideas of who belongs where.
- **Wait for goauth to ship an API key module**: cleanest long term. Rejected: it blocks delivery on library work, and key semantics (scopes, prefix, workspace binding) are product decisions anyway.
- **goauth organizations as workspaces, plus a Hookbeam `api_keys` table**: Chosen.

## Decision
goauth organizations are the workspaces. API keys are Hookbeam's own `api_keys` table: an `hb_` prefix, only the hash stored, scopes, optional expiry, revocation. The scopes an operation requires are written in the spec as the values of `apiKeyAuth` and checked by the middleware.

## Consequences
- An `hb_` token is accepted only on operations that declare `apiKeyAuth`; a key missing a required scope gets 403.
- Roles are read through goauth's storage, and there are three fixed roles until a customer needs more.
