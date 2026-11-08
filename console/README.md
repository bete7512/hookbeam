# Hookbeam console

Next.js (App Router) + TypeScript + Tailwind + shadcn/ui + TanStack Query. The API client in `src/api/schema.d.ts` is generated from `../api/gen/openapi/openapi.bundle.yaml` (`make gen-client`); never edit it.

Run it from the repository root: `make console-dev` starts the local reverse proxy (Caddy, `deploy/proxy/Caddyfile`) on `http://localhost:3000` and `next dev` behind it, so the console, `/v1/*` and `/v1/auth/*` share one origin and goauth's cookies just work. Start the API with `make dev` in another terminal.

`make console-check` runs lint, typecheck and the unit tests, the same as CI.
