# 0002. Two planes in one repository, one module, own databases

Date: 2026-11-07 · Status: accepted

## Context
The data plane must scale and fail independently of the control plane, and the spec forbids one service from reading another's database.

## Options
- **One service**: keep delivery as packages inside `api/` with one database. Rejected: there is no boundary to defend and the spec requires the data plane to stand alone.
- **Two services, two Go modules joined by `go.work`**: each plane builds and versions on its own. Rejected: `go mod tidy` ignores workspaces, so every module needs `replace` lines and CI needs a module matrix, for a split nothing needs yet.
- **Two services at the repository root in one Go module**: `api/` and `delivery/` side by side, sharing `pkg/` and `contracts/`. Chosen.

## Decision
`api/` is the control plane and `delivery/` is the data plane. Each has its own database, migrations and config. Both live at the repository root in one Go module and share only `pkg/` and `contracts/`.

## Consequences
- The two services talk only through the gRPC contract and events; a lint rule stops `pkg/` from importing either service.
- A new business area with its own data becomes a new root folder, never a folder inside an existing service.
- Locally `make db-up` creates both databases; in production Terraform creates them.
