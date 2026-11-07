# 0013. Outbound requests resolve-then-dial through an SSRF guard, no redirects

Date: 2026-11-07 · Status: accepted

## Context
Tenants type the endpoint URLs. A public hostname can resolve to a private or metadata address, and a redirect can lead anywhere after the URL was checked.

## Options
- **Validate the URL string only**: cheap. Rejected: a public hostname can resolve to a private address later (DNS rebinding), and a redirect can point anywhere after the check.
- **An egress proxy that enforces the policy**: Rejected: infrastructure that does not exist yet, and the rules still have to live somewhere.
- **Check the resolved address inside the dialer and refuse all redirects**: the address that was checked is the address that is connected. Chosen.

## Decision
The worker's HTTP client resolves the host inside the dialer, refuses private, loopback, link-local, multicast, metadata and mapped addresses, and connects to the address it checked. Redirects are never followed; a 3xx is recorded as a terminal attempt.

## Consequences
- Only https on allow-listed ports (443, 8443), with bounded timeouts and body size, and no proxy from the environment.
- A blocked destination is a terminal failure and is not retried.
- `DELIVERY_SENDER_ALLOW_PRIVATE=true` exists for the local demo receiver and is refused in staging and production.
