# Security regression suite

`make test-security` runs every test named `TestSecurity*` under the `security` build tag with the race detector,
and CI runs it on every push next to gitleaks (secrets in the history, configured by `.gitleaks.toml`) and, on
pull requests, the dependency review. Trivy (images), govulncheck (modules) and gitleaks block a merge on a
critical finding. The table maps each row of the specification's security table to the tests that pin it; the
untagged tests listed run in `make test` as well.

| Specification row | Behaviour pinned | Tests |
|---|---|---|
| Browser authentication | Cookie-authenticated writes accepted from the console's origin only; bearer and reads unaffected | `TestSecurityCSRFOriginOnCookieRoutes` (api/handlers); `TestAuthentication` (missing, invalid, bearer, cookie, public routes) |
| Browser authentication | Session and key are different credential kinds; a key never acts as a person | `TestSecurityCredentialKinds` (api/handlers); `TestAuthentication` |
| Tenant authorization | Deny by default: capabilities by role, non-members read nothing | `TestSecurityRoleMatrix` (api/services); `TestRoleMatrix`, `TestAuthorizerRoleAndCan` |
| Tenant authorization | Every data-plane read is scoped by workspace; another tenant's row is not found, never forbidden | `TestGetDelivery`, `TestGetApplication`, `TestGetEndpoint`, `TestGetMessage`, `TestGetOverview`, `TestHealth*`, `TestAudit*` cases with `ws_other` (delivery/repos, integration); `scoped`/`Unauthenticated` cases in every RPC test (delivery/rpc) |
| Tenant authorization | Payload and replay need the capability or the scope | `TestGetMessagePayload`, `TestReplayDelivery` (api/handlers); `TestDeliveryGetMessagePayload` (api/services) |
| Machine credentials | Keys are refused on session routes, scopes are enforced before the data plane, bound keys submit only to their application, submissions are rate limited | `TestSecurityCredentialKinds`; `TestSubmitMessage`, `TestSubmitMessageKeyBindingAndRate`, `TestAPIKeys` (api/handlers) |
| Machine credentials | Only prefix and hash are stored; revoked and expired keys fail | `TestAPIKeyService*` (api/services), `TestAPIKeyRepo*` (api/repos, integration) |
| Signing secrets | Tampered body, id, timestamp, signature or secret is refused; expiry window; rotation overlap | `TestSecuritySignatureTampering` (pkg/webhooks); `TestVerify`, `TestSign` |
| Signing secrets | Encrypted at rest with a key id; never in logs or spans | `TestCipher*` (pkg/secrets); `TestAttemptSpanCarriesNoPayloadOrSecret` (delivery/services) |
| Customer bodies | Payload bytes never reach spans or logs; permitted reads are audited; expired payloads are 410 | `TestSubmitCanaryNeverReachesTelemetry` (api/handlers); `TestGetMessagePayload` (delivery/rpc, api/handlers) |
| Receiver responses | Bounded preview, redirects recorded not followed, only an allowlist of response headers is kept | `TestSecurityResponseHeaderAllowlist`, `TestSend` (delivery/sender) |
| Sensitive operations | Key creation and revocation, member changes, endpoint changes, payload reads, replay, archive are audited | `TestAudit*` (api/services, delivery/services), `TestOrgRouteGuard` (api/handlers) |
| Privilege escalation | Nobody grants above their own role; only owners touch owners; one owner always remains | `TestSecurityPrivilegeEscalation` (api/services); `TestOrgPolicy*`, `TestOrgRouteGuard` |
| Outbound SSRF | Private, loopback, link-local, metadata, multicast and IPv4-mapped forms refused in URLs and at dial time (rebinding); no redirects; https on allowed ports; no credentials | `TestSecuritySSRFDestinations`, `TestSecuritySSRFRebinding`, `TestSecuritySSRFRedirects` (pkg/safehttp); `TestValidateURL`, `TestBlocked`, `TestClient` |
| Outbound SSRF | The development exception is refused in staging and prod | `TestConfigValidate*` (delivery/config) |
| Secret leakage | Internal errors answer a generic body; the cause stays in the operator's log | `TestSecurityErrorBodiesLeakNothing` (api/handlers); `TestLogValueMasksPassword` (pkg/config) |
| Dependency and runtime protection | Pinned tool versions, distroless non-root images scanned by Trivy, govulncheck, gitleaks, dependency review | CI jobs `image`, `vuln`, `security` |
