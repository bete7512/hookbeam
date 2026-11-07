# 0009. The planes talk over gRPC with mTLS; the workspace is trusted metadata

Date: 2026-11-07 · Status: accepted

## Context
The control plane calls the data plane synchronously and passes along the workspace it already authenticated. Without proof of who is calling, any process that reaches port 9090 could act on any tenant.

## Options
- **Internal REST between the planes**: a second OpenAPI document and hand-written client. Rejected: weaker typed errors and a second client to maintain for an internal call.
- **gRPC in plaintext on a private network**: Rejected: any process that reaches port 9090 can act on any tenant, so trust rests entirely on a security group.
- **gRPC forwarding the end user's JWT for the data plane to verify**: Rejected: API keys are not JWTs, and it teaches the data plane about identity.
- **gRPC with mutual TLS from one CA, the workspace passed as request metadata**: the client certificate proves the caller is the control plane, which already authenticated the user or key. Chosen.

## Decision
The contract is `hookbeam.delivery.v1`, generated with buf. The connection is mutual TLS from one CA, so the server accepts only callers holding the control plane's client certificate. Workspace, actor and request id travel as gRPC metadata and are read by one interceptor.

## Consequences
- Every repo read in the data plane is scoped by `workspace_id`, so another tenant's row is "not found".
- Proto types stay inside `delivery/rpc` and `api/services/delivery.go`; `apperr` errors map to gRPC status codes in `pkg/grpcx`.
- Plaintext is allowed only with `*_TLS_INSECURE=true`, which staging and production refuse.
