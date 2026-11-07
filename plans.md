# From scaffold to a complete open-source product

> Foundation update (2026-10-07): R01–R14 from the review have implementations; R15/R16 are partly addressed. Follow [architectural-defeit.md](architectural-defeit.md) for current debt, upgrade notes and verification. Historical checklists below remain planning context; Hookbeam is the selected product direction.

Date: 2026-10-06. Status: proposed implementation plan; the work below is pending.
The code review is recorded in [review.md](review.md). Creating these documents
does not mean the findings have been fixed or the release gates have passed.

**Product direction updated:** Hookbeam, a webhook delivery platform, is now the
selected product. Its [product README](product/README.md) supersedes the ImportDesk
scope and product-specific steps below. The foundation fixes and general operational
guidance remain relevant; Hookbeam's milestones define its product implementation
order, including an earlier second service and gRPC integration.

This plan develops the goals in `.dev/README.md` into an ordered backlog. It uses
milestones and acceptance criteria instead of the original thirty-day deadline.
The private design notes remain useful context; this file includes the decisions
needed by contributors who do not have the gitignored `.dev/` directory.

## Recommended product: ImportDesk

Build an **open-source CSV intake, validation, approval, and delivery workspace**.
ImportDesk is a working name, not a checked project or trademark name.

The target user is a small operations team receiving recurring CSV files from
suppliers, partners, or other departments. Today they exchange attachments, find
errors manually, and struggle to identify which version was approved. The product
should answer: what arrived, whether it is valid, who approved it, and which exact
artifact was delivered.

Start with a supplier inventory example: `sku`, `quantity`, and `warehouse`.
An administrator defines simple rules, a member uploads inventory, and a reviewer
approves a valid version for delivery. Other CSV schemas use the same workflow.
The outcome is a real approved CSV artifact, not merely a successful background job.

Why this recommendation:

- The result is useful without a novel algorithm or a large integration catalog.
- Background validation, retries, notifications, and delivery give events a real job.
- Organizations, permissions, audit history, and a usable UI are necessary features.
- The domain creates meaningful tests for worker crashes, duplicates, cancellation,
  approval races, tenant isolation, and recovery.
- It fits the Go/Postgres/Next.js/AWS stack while leaving an achievable first release.

Alternatives remain viable: a webhook delivery/replay console, or a team approval
system for access/equipment requests. The former is a particularly good reliability
exercise, but existing products such as [Svix](https://www.svix.com/open-source-webhook-service/)
already provide open-source webhook delivery. ImportDesk is the recommended path
here because its complete workflow is easy to demonstrate to technical and
nontechnical users. This is a product recommendation, not a claim of validated demand.

## What a fully functional v1 includes

An owner installs the application, creates an organization, and invites teammates.
An administrator creates an intake definition with CSV rules. A member uploads a
file, sees validation results, downloads an error report, and submits a corrected
version. A reviewer approves or rejects that exact version. The worker delivers
the approved artifact to a configured S3 prefix. Authorized users can download it
and inspect its checksum, delivery status, approval, and attempt history.

| Included in v1 | Deliberate boundary |
|---|---|
| Password auth, verification, recovery, Google login when configured | No enterprise SSO or arbitrary identity-provider catalog |
| Organizations, invitations, owner/admin/member roles | No billing or nested organizations |
| Versioned intake definitions and submissions | CSV only; no XLSX, PDF, archives, or arbitrary executable transformations |
| Required fields, basic types, ranges, enums, duplicate-key checks | No workflow builder or general ETL engine |
| Validation reports, approval/rejection, corrections, delivery | One supported operator-configured S3 destination model |
| Retry, bounded cancellation, progress, notifications, audit trail | No claim of exactly-once external effects |
| Responsive browser UI, documented API, example data | Every advertised action works; no placeholder pages |
| Complete Compose demo and AWS deployment into the operator's account | LocalStack is for development/demo; provider-independent production comes later |
| Backups, restore procedure, alerts, release/upgrade instructions | No high-availability promise without a tested deployment profile |

Initial roles: a member submits and sees organization resources; an admin also
manages intake definitions and reviews submissions; an owner also manages members
and organization lifecycle. Require a different person to approve a submission in
v1. Provide two demo accounts so that this rule is practical to demonstrate.

Use one business application in `api`. Intake definitions, submissions,
approvals, and deliveries are resources within it, not separate microservices.
Use the existing `console` placeholder for the Next.js application and update
the old `console` references when implementing the frontend.

Keep the template reusable: put generic infrastructure in `pkg/`, product behavior
in `api`, and example fixtures in a clearly marked demo area. After the product
works, publish a clean template release and a product release with a documented
relationship. Do not build a plugin framework to separate them upfront.

## Execution rules and release boundaries

- Work through numbered steps in order. Each step can contain several small PRs;
  it is complete only when its acceptance criteria pass.
- Add regression tests before changing a reported invariant. Preserve existing
  user changes; staging, committing, and publishing remain separate requested actions.
- For every code step, follow [AGENTS.md](AGENTS.md) and the relevant task playbook.
  Regenerate contracts/mocks when their sources change and review generated diffs.
- Use actual version pins, recorded benchmark conditions, and measured costs.
  Estimates and proposed limits in this plan are not performance results.
- Documentation-only work needs link/content checks, not application generators.
- Resolve contradictions in the ADRs and agent playbooks with the implementation;
  do not leave accepted instructions promising behavior the code no longer uses.

| Milestone | Steps | Demonstrable result |
|---|---|---|
| M0: trustworthy backend foundation | 01–08 | Reviewed guarantees have regression coverage; existing endpoints are protected |
| M1: complete local product | 09–16 | A team can finish upload → correction → approval → delivery in a browser |
| M2: operated cloud release | 17–20 | Same workflow deployed, observable, recoverable, and installable |
| M3: open-source v1.0 | 21 | Fresh-clone install and release checklist pass |
| M4: portfolio extensions | 22–27 | Additional original-roadmap capabilities with their own acceptance gates |

### Step 01 — Establish the baseline and reproducible tooling

Addresses [R13](review.md#r13) and [R16](review.md#r16).

- [ ] Record the working-tree baseline and separate source changes from generated output.
- [ ] Centralize exact oapi-codegen, mockgen, linter, and other tool versions; use
  the same pins locally and in CI. Document the supported Go and Node versions.
- [ ] Track the public ADRs/runbooks; keep private planning notes private. Correct
  stale Taskfile, sqlc, strict-server, and unimplemented-feature claims.
- [ ] Fix the SMTP timeout contract: distinguish cancellation, a deadline reached,
  and unrelated network failure consistently. Do not merely add sleeps to the test.
- [ ] Document local key generation before migrations/startup; bootstrap must not
  overwrite an existing `.env` or create a production signing key silently.

Acceptance: a clean checkout installs the pinned tools, generates without drift,
and passes the SMTP timeout regression repeatedly. The review's prior test results
remain historical evidence until the revised code is tested again.

### Step 02 — Make credential revocation survive concurrent logins

Addresses [R01](review.md#r01).

- [ ] Add a session credential-generation field and capture the generation used
  when authenticating. Refresh must reject a session whose generation differs
  from the current user generation; it must never upgrade a stale session.
- [ ] Define how existing sessions are invalidated/backfilled during migration;
  do not infer a legacy session's original generation from today's user row.
- [ ] Make credential changes, generation bumps, and session revocation atomic.
  Cover reset, change-password, logout-all, and Google account reclamation.
- [ ] If locking is needed for additional invariants, use the same user lock and
  consistent lock order. Avoid holding a database lock throughout expensive hashing.
- [ ] Preserve the existing refresh-reuse policy and document its browser implications.

Acceptance: orchestrate a login that pauses after reading old credentials, complete
revocation, then resume it. Neither its access token nor its refresh token can
authenticate. Prove this for reset and Google reclamation with real PostgreSQL.

### Step 03 — Make Google identity linking explicit and safe

Addresses [R02](review.md#r02).

- [ ] Continue finding existing identities by provider/subject.
- [ ] Represent the verified issuer and provider claims required to establish email
  authority, including Google's hosted-domain claim when relevant.
- [ ] Require current account authentication or a fresh mailbox challenge for
  ambiguous email-based links. Never attach an identity from email equality alone.
- [ ] If retaining automatic links for authoritative Google identities, document
  the precise conditions and preserve the unverified-account reclamation protections.
- [ ] Handle simultaneous first logins without creating duplicate identities or
  leaving partially linked accounts. Preserve state/PKCE and sanitize callback errors.
- [ ] Update ADR 0007, tests, API behavior, and the later frontend handoff together.

Acceptance: test Gmail/Workspace authority, nonauthoritative third-party email,
existing subject with changed email, linking conflicts, unverified local account,
invalid state, and cancelled provider login. Perform a real Google smoke test
before advertising the integration as supported.

### Step 04 — Repair transaction boundaries

Addresses [R03](review.md#r03) and [R04](review.md#r04).

- [ ] Bind registration's outbox repository to the user-creation transaction.
- [ ] Consume a verification token and mark the user verified in one transaction.
- [ ] Audit remaining multi-write security operations for pool-bound calls inside
  transactional callbacks; use distinct transaction-bound mocks where useful.
- [ ] Add service integration tests with injected failures and rollback assertions.

Acceptance: registration leaves both user and event or neither; an uncommitted
user never has a publicly visible event; registration works with one pool connection;
failed verification leaves its token usable. Mock-only tests are insufficient here.

### Step 05 — Give recovery requests a durable token and delivery lifecycle

Addresses [R05](review.md#r05) and [R06](review.md#r06). Complete Step 02 first.

- [ ] Distinguish a new logical recovery request from another attempt to deliver
  that same request. Assign a durable request ID; queue messages carry only that ID.
- [ ] Serialize replacement per user/purpose. Enforce the active-request/token
  invariant in PostgreSQL, rather than relying on delete-then-insert at default isolation.
- [ ] A newer accepted request supersedes the older request; an old queued job
  must not replace the newer token when it eventually runs.
- [ ] Reuse the same link for retries of one logical request. Preferred design to
  evaluate in an ADR: store the verifier hash normally and retain short-lived,
  encrypted delivery material in a restricted table, outside ordinary outbox JSON.
  Use established authenticated encryption/key management, separate keys, versioned
  key IDs, expiry, and cleanup. Never log raw links or store plaintext tokens.
- [ ] Commit preparation before sending. Send outside long database transactions;
  record attempts and reconcile ambiguous outcomes. Duplicate mail must not revoke
  an already delivered link from that same request.
- [ ] Keep consume/update/revocation atomic. Successful reset invalidates all
  outstanding reset authorization appropriate to the chosen lifecycle.
- [ ] Render expiry from actual configuration instead of hardcoding thirty minutes.

Acceptance: concurrent issuance, stale queued events, retry after accepted mail,
failure after send but before recording success, expiry, and single-use consumption
all behave as documented. A raw-token leak check covers logs and broker payloads.
An explicitly newer request can invalidate an older email; a delivery retry cannot.

### Step 06 — Correct rate-limit accounting and recovery abuse policies

Addresses [R07](review.md#r07) and [R08](review.md#r08).

- [ ] Calculate the window while holding the counter lock; prevent old-window
  requests from resetting newer counters. Test the boundary with a controlled clock.
- [ ] Separate login/registration, recovery-email, and session-maintenance budgets.
  Shared-office traffic must not consume a tiny common logout/refresh budget.
- [ ] Add recipient/action cooldowns or coalescing for recovery requests, preserving
  indistinguishable public responses. Persist logical recovery state from Step 05;
  do not move high-frequency request counters into Postgres.
- [ ] Define behavior under cache eviction/full capacity. Treat local limits as
  per-replica protection; use reliable shared counters if exact enforcement is required.
- [ ] Remove or fix the multiple-limiter/shared-counter concurrency hazard. Either
  enforce one owner or share synchronization; add a race test for supported usage.
- [ ] Specify the trusted proxy chain, canonicalize client addresses, and align
  declared OpenAPI 429 responses with the operations actually limited.

Acceptance: deterministic boundary/concurrency tests pass; recovery requests across
several simulated IPs cannot continually flood one recipient; multiple legitimate
users behind one IP can maintain and end their sessions.

### Step 07 — Bound integration failures and validate production configuration

Addresses [R09](review.md#r09), [R13](review.md#r13), [R14](review.md#r14), and
[R15](review.md#r15).

- [ ] Move Google discovery into HTTP-specific wiring and give network initialization
  an explicit timeout. The worker must start without contacting Google.
- [ ] Give outbound mail and other network operations bounded lifetimes; verify
  resource cleanup on failure before a client is fully constructed.
- [ ] Require a second JSON decode to return EOF; reject malformed trailing data
  and preserve correct oversized-body responses.
- [ ] Add role/environment-aware configuration validation. Production must not
  silently discard work through memory mail/event adapters or use local link origins.
- [ ] Implement and document JWT key rotation with an active signing key and bounded verification-key
  overlap. Document emergency invalidation and avoid keeping private signing keys
  in a process that only needs unrelated capabilities when practical.
- [ ] Verify redaction of database credentials, OAuth secrets, cookies, recovery
  links, and signed storage URLs in errors and startup/access logs.

Acceptance: worker boot succeeds while Google is unreachable; outbound hangs end
within their configured budgets; invalid production combinations fail clearly;
parser regressions and key-rotation behavior are tested.

### Step 08 — Close existing authorization and event-recovery gaps

Addresses [R10](review.md#r10), [R11](review.md#r11), and [R12](review.md#r12).

- [ ] Restrict current profile/password/delete operations to the caller or an
  explicit authorized administrator. Remove unrestricted user-directory exposure.
- [ ] Add per-message deadlines, bounded concurrency, and visibility extension for
  all received messages that can wait or execute beyond their initial visibility.
- [ ] Define retry/backoff, terminal failure, quarantine, and DLQ handling. Unknown
  required event versions and corrupt envelopes must not silently disappear.
- [ ] Document compatible consumer-first event rollouts and test mixed versions.
- [ ] Separate completed-event deduplication from external-side-effect guarantees.
  Test partial publish success and failure after effect but before acknowledgement.
- [ ] Introduce explicit event schema version, correlation/trace context, and a
  stable logical event identity. Before multiple producers/consumers, namespace
  deduplication by source and consumer; do not assume every database's bigint IDs
  are globally unique. Business primary keys can remain bigint.
- [ ] Exercise shutdown with in-flight work and database-pool pressure.

Acceptance: another user cannot read or mutate protected user resources; LocalStack
tests actually run; poison/unsupported messages are recoverable; worker termination
does not silently lose accepted work. M0 is complete only after these tests pass.

### Step 09 — Specify the product model and its state transitions

Build only after M0. Replace the generic Project example with an intake definition
that exercises the same CRUD/list conventions while serving a real user need.

- [ ] Define organizations, memberships, intake definitions, immutable schema
  versions, submissions/file versions, validation runs, approvals, deliveries,
  attempts, notifications, and audit entries.
- [ ] Keep submission, validation, review, and delivery states distinct. Suggested
  states: validation `queued/running/passed/failed/cancelled`; review
  `pending/approved/rejected`; delivery `pending/running/succeeded/failed/cancelled`.
- [ ] Pin every run and approval to a specific file version, checksum, and schema
  version. Editing an intake definition never changes historical results.
- [ ] Use conditional transitions, attempt ownership, and fencing so stale workers
  cannot overwrite a retry or cancellation. Treat uncertain delivery as requiring
  reconciliation; cancellation cannot undo an artifact already delivered.
- [ ] Define an idempotency-key contract for retryable mutation requests, scoped
  to tenant/caller and request content, with bounded retention.
- [ ] Record the actual source/delivery storage policy and initial processing limits.

Acceptance: a transition table and tests define every valid/invalid action, including
concurrent approve/reject and cancellation/completion. Proposed paths and tables
become code only through migrations and the existing spec-first workflow.

### Step 10 — Add organizations, scoped roles, invitations, and audit

- [ ] Implement organization create/update/list, memberships, role changes, and
  invitations with expiry, single-use acceptance, resend, and revoke.
- [ ] Enforce authorization in services and tenant scope in queries. Derive scope
  from authenticated membership; never trust a supplied organization ID by itself.
- [ ] Use owner/admin/member permissions and disallow self-approval. Prevent
  removing/demoting the final owner or bypassing restrictions through bulk/list APIs.
- [ ] Insert authoritative audit records in the same transaction as the action.
  Events can create notifications/projections; they do not establish whether the
  original change was audited.
- [ ] Test tenant boundaries across implemented CRUD, lists, memberships, and
  invitations. Extend this same permission matrix as uploads/downloads, worker
  payloads, approvals, cancellation, retries, and notification subscriptions arrive
  in subsequent steps; each feature must pass it before its step is complete.

Acceptance: two organizations and several roles pass a deny-by-default permission
matrix. Inviting, accepting, changing role, and revoking access work end to end.

### Step 11 — Build browser authentication and the first usable console

- [ ] Create `console` with Next.js App Router, TypeScript strict mode,
  Tailwind, shadcn/ui, and an OpenAPI-generated client.
- [ ] Use one public origin initially: a local reverse proxy for Compose and ALB
  path routing in AWS, forwarding `/v1/*` and later `/ws` to Go and UI routes to
  Next.js. Keep the internal Go address configurable for server-side rendering.
- [ ] Define a browser cookie mode with host-only HttpOnly cookies, production
  Secure attributes, explicit SameSite/path settings, refresh and logout clearing.
  Browser auth must not expose raw token pairs to JavaScript; retain a documented
  bearer-token flow for nonbrowser API clients.
- [ ] Add CSRF protection/Origin validation to cookie-authenticated mutations.
  Test session rotation across concurrent requests/tabs without accidental reuse
  revocation. Do not put refresh tokens in localStorage.
- [ ] Implement register/login, verify/resend, forgot/reset/change password,
  Google callback handoff, logout/logout-all, profile, organization switcher,
  membership/invitations, and useful expired-session/error states.
- [ ] Use server components for initial reads and TanStack Query for interactive
  state where useful. UI route guards improve navigation; the API owns security.

Acceptance: Playwright completes password and invitation flows using Mailpit; fake
OIDC covers CI and real Google covers a manual smoke test. Direct API calls cannot
bypass the UI's permissions. Start the product UI now, before building all cloud features.

### Step 12 — Implement intake definitions and safe, immutable uploads

- [ ] Add intake-definition CRUD and paginated/filterable lists, with published
  schema versions and the inventory example. Give each form real validation/errors.
- [ ] Add a `pkg/` object-storage port and an S3 implementation; keep metadata and
  authorization in the application. Extend local tooling with S3 emulation.
- [ ] Support UTF-8 comma-separated CSV with a header row. Proposed initial limits:
  10 MiB, 100,000 data rows, 100 columns, 64 KiB per field, 60 seconds per validation,
  and 1,000 retained error samples. Enforce all limits and revise from measurements.
- [ ] Authorize upload creation; choose server-controlled object keys, short URL
  lifetimes, tenant quotas, and restricted storage CORS. Verify actual size/checksum
  and ownership during finalization; client declarations are not authoritative.
- [ ] Document that presigned URLs are bearer capabilities: authorization happens
  at issuance, and a copied URL can remain usable until expiry. Choose and test
  short expiries; never describe these URLs as bound to the signed-in browser.
- [ ] Pin a storage version or copy finalized bytes into a worker-only immutable
  location. A presigned upload to the same key can replace an object; possession
  of an old upload URL must not change an already reviewed submission.
- [ ] Make finalize idempotent and write its event in the metadata transaction.
  Reconcile abandoned uploads and storage/database failures with scheduled cleanup.
- [ ] Provide upload progress, completion/error feedback, and a submission detail page.

Acceptance: real browser upload/finalize works; replayed finalize creates one logical
submission; unauthorized callers cannot obtain URLs for another tenant's objects
or finalize its uploads; expired URLs fail; overwriting staging bytes cannot alter
the finalized version. [S3 upload behavior](https://docs.aws.amazon.com/AmazonS3/latest/userguide/PresignedUrlUploadObject.html)
is the reason immutability must be enforced explicitly.

### Step 13 — Deliver bounded CSV validation and useful reports

- [ ] Stream validation in the worker with bounded memory. Implement required
  columns/values, string/integer/decimal/date types, ranges, enum values, and a
  configurable duplicate key. Define decimal/date formats explicitly.
- [ ] Treat source data as untrusted: validate dialect, duplicate headers, quoting,
  embedded newlines, long fields, encoding, and row/column limits.
- [ ] Separate total error count from retained samples. Produce a summary and an
  authorized downloadable report with row, column, rule, and useful explanation.
- [ ] Escape previews and use a spreadsheet-formula-injection policy for generated
  reports. Preserve raw source bytes separately; label raw downloads and safe reports.
- [ ] Persist progress and attempt results. Poll authenticated status endpoints
  initially; browser refresh must reconstruct state from the database.
- [ ] Add correction upload as a new submission/file version linked to its predecessor.

Acceptance: a bad inventory file shows actionable errors; a corrected version passes;
old results remain unchanged; oversized/malformed input fails predictably; killing
and restarting the worker recovers the run without contradictory results.

### Step 14 — Implement approval and a real delivery destination

- [ ] Add a review queue and exact-version preview; approve/reject with comments,
  permissions, version checks, and audit entries. Failed validation cannot be approved.
- [ ] Approval and creation of the delivery request/outbox event are atomic.
- [ ] Deliver the validated approved CSV to an operator-configured S3 bucket/prefix,
  using task-role credentials and tenant-specific namespaces. In v1 administrators
  choose from configured destinations; users cannot supply arbitrary credentials/URLs.
- [ ] Give each logical delivery a stable ID, deterministic object key, and checksum.
  Reconcile a matching existing object after an ambiguous write before retrying.
- [ ] Record artifact identity, checksum, size, completion time, and each attempt.
  Expose a working delivery-history page and downloads authorized at URL issuance,
  with short expiry and a documented revocation window. If immediate revocation
  is required, use an authenticated download proxy instead of direct signed URLs.
- [ ] Implement retry and cooperative cancellation with the cutoff from Step 09.
  Never label an uncertain/already-completed external write as undone.

Acceptance: a separate reviewer approves a version and obtains the actual delivered
artifact. Duplicate events/retries do not create untracked outputs; concurrent
approval/rejection resolves once; wrong-tenant and stale-version operations fail.
Revoked members cannot obtain new download URLs; any previously issued URL follows
the explicitly tested expiry/revocation policy.

### Step 15 — Complete notifications, user recovery, and operator recovery

- [ ] Persist notifications for validation outcome, review requested, rejection,
  approval, and delivery failure/success; support unread/read state and email preferences.
- [ ] Build clear UI states for empty lists, failed uploads, expired invitations,
  cancelled runs, permission loss, and retryable versus terminal failures.
- [ ] Provide restricted operational views/actions for failed attempts, quarantined
  events, DLQ inspection/replay, and stalled work. Audit every replay/cancel action.
- [ ] Add cleanup/retention policies for uploads, reports, tokens, attempts, outbox,
  and processed-event records. Deduplication retention must cover the supported
  redelivery/replay window; expiration must not silently reauthorize old work.
- [ ] Ship example CSVs: valid, malformed, duplicate SKU, missing field, and corrected.

Acceptance: a user can recover from a rejected file and a transient delivery failure
without SQL access. An operator can investigate and replay safe failed work without
editing production rows or bypassing permissions.

### Step 16 — Prove the complete local workflow

- [ ] Provide a pinned Compose demo containing API, worker, console, reverse proxy,
  Postgres, LocalStack/S3 emulation, and Mailpit, with documented startup/migrations.
- [ ] Add Vitest/Testing Library coverage for meaningful UI behavior and Playwright
  tests for the full two-user journey, including permissions and failure recovery.
- [ ] Test with two workers, two API instances where supported, and persistent
  volumes. Run the real broker tests instead of allowing silent skips in this job.
- [ ] Prove state remains understandable after refresh/relogin, worker restart,
  broker outage, duplicate delivery, cancellation races, and invalid input.
- [ ] Have a fresh operator follow the installation and demo instructions without
  private files, preinstalled generators, or manual database fixes.

Acceptance: M1 is a complete local product. A backend API collection or a happy-path
video alone is insufficient. Record failures discovered here before cloud deployment.

### Step 17 — Deploy one supported AWS configuration

- [ ] Add Terraform modules for network, ECS, ALB/ACM/DNS, ECR, RDS, storage, SNS/SQS
  and DLQs, SES, secrets, and GitHub OIDC roles, with small composable interfaces.
- [ ] Bootstrap versioned remote state and locking. For a compatible pinned Terraform
  version use S3 `use_lockfile`; the old draft's DynamoDB locking proposal is deprecated.
  Commit provider lockfiles; keep state and sensitive plan artifacts out of Git.
- [ ] Start with one dev environment. Document public/private routing, permitted
  proxy hops, least-privilege task/execution/deploy roles, encryption, and egress.
- [ ] Deploy API and worker to ECS Fargate. Deploy Next.js as a separate frontend
  task behind the same ALB/origin for the first supported production topology.
- [ ] Add GitHub Actions using OIDC: build once, scan, push immutable image/digest,
  run migrations once, deploy, perform smoke tests, and roll back failed application
  rollouts. Block releases on all relevant checks, not just image build success.
- [ ] Configure real SES delivery and Google redirect origins; test actual cloud
  S3/SNS/SQS behavior and the complete browser journey.
- [ ] Add budgets, tagging, cost estimates based on the chosen region/resources,
  and teardown instructions that clearly distinguish disposable from retained data.

Acceptance: a fresh AWS account setup is documented; the complete product works over
TLS using deployed images. Failed startup rolls back without destroying data.
See [Terraform S3 locking](https://developer.hashicorp.com/terraform/language/backend/s3)
and [Next.js self-hosting](https://nextjs.org/docs/app/guides/self-hosting).

### Step 18 — Add operational signals before claiming production readiness

- [ ] Propagate request/correlation and trace context through HTTP, outbox, broker,
  worker, and delivery. Model asynchronous causality with appropriate trace links.
- [ ] Add OpenTelemetry traces and metrics: route latency/errors, database pool
  waits, outbox age, queue age, processing duration, retry/failure counts, and DLQ depth.
- [ ] Keep metric labels bounded; do not label metrics by user, file, or request ID.
- [ ] Choose Sentry or GlitchTip first; test redaction, release metadata, panic/job
  capture, and shutdown flushing. Add restricted pprof access, never public exposure.
- [ ] Publish dashboards and actionable alerts with matching runbooks. Test alert
  delivery using controlled failures; an installed dashboard is not a verified alert.
- [ ] Make readiness role-specific. A durable API may accept queued work during a
  broker outage if database persistence is healthy; surface the backlog separately.

Acceptance: follow one failed import from request to worker attempt and recovery;
the expected alert fires and a second person can follow its runbook.

### Step 19 — Add realtime updates on top of durable state

- [ ] Implement authenticated `coder/websocket` subscriptions with tenant/permission
  checks, Origin validation, bounded buffers, heartbeats, and slow-client handling.
- [ ] Use Postgres LISTEN/NOTIFY as a wake-up/fan-out signal carrying small IDs.
  Durable notifications/progress remain queryable in Postgres.
- [ ] Reconnect and resynchronize from durable state; missed notifications must not
  leave a browser permanently stale. Recheck access when membership changes.
- [ ] Test same-origin ALB routing, idle timeouts, proxy upgrades, and SIGTERM drain
  across two API replicas. Retain polling as a functional fallback.

Acceptance: progress and notifications reach the right user across replicas;
disconnect/reconnect and deployment recover the displayed state. No generic Next.js
Route Handler is assumed to provide a portable WebSocket proxy.

### Step 20 — Prove deployment, backup, security, and capacity behavior

- [ ] Create staging/prod profiles with explicit availability, backup, retention,
  deletion-protection, network, and cost differences. Keep demo fixtures out of prod.
- [ ] Restore database and object data into a clean environment; reconcile
  outbox/delivery state after restore. Record achieved recovery time and data-loss window.
- [ ] Test expand/contract upgrades and application rollback against the migrated
  schema. Down migrations are tested locally, not used blindly as production rollback.
- [ ] Add k6 scenarios for HTTP and whole-job completion; document file sizes, load,
  machine/task resources, replica counts, and latency/throughput results.
- [ ] Test task termination, slow storage/mail, broker outage, pool exhaustion,
  expired leases, and duplicate events. Confirm bounded resource use and recovery.
- [ ] Run dependency/image/secret scans, validate quotas and proxy trust, and test
  backup/access restrictions. Set SLOs from measured behavior and user needs.

Acceptance: the system meets its recorded limits and recovery objectives, or the
release notes narrow those claims explicitly. M2 requires a restore drill, not only
a configured backup policy.

### Step 21 — Publish the complete open-source v1.0

- [ ] Add a chosen open-source license, CONTRIBUTING, SECURITY reporting route,
  issue templates, maintainership expectations, changelog, and supported versions.
- [ ] Publish versioned images, installation/upgrade/rollback instructions, local
  demo setup, AWS deployment guide, configuration reference, and API documentation.
- [ ] Include the sample inventory dataset, a two-user walkthrough, screenshots,
  short demonstration video, architecture diagram, ADRs, known limits, and costs.
- [ ] Run fresh-clone and upgrade tests using only public repository contents.
- [ ] Verify every visible button and documented endpoint completes a supported
  action; remove unfinished menu items and misleading production claims.
- [ ] Get one external operator to install it and one small team to complete the
  workflow. Record usability failures and fix release-blocking ones.

Release acceptance: upload → errors → corrected version → independent approval →
S3 delivery/download → history all work, including recovery and cross-tenant denial.
The release supports production in the operator's AWS account and a complete local
demo. It does not yet promise production operation without AWS.

### Step 22 — Offer production self-hosting without AWS

First recommended post-v1 extension if open-source adoption is the priority.

- [ ] Implement a durable NATS JetStream adapter behind the existing contracts;
  test acknowledgements, durable consumers, redelivery, backpressure, retention,
  and the application's terminal-failure/replay policy. Core NATS alone is insufficient.
- [ ] Add a tested S3-compatible storage configuration and SMTP configuration for
  production self-hosting, with explicit compatibility and support boundaries.
- [ ] Ship a production-oriented Compose deployment with TLS ingress, persistent
  data, secrets, backups, upgrades, monitoring, and resource limits.
- [ ] Run the same end-to-end failure/tenant suite against AWS and this deployment.

Acceptance: a new operator can run the full product on their own server without an
AWS account or LocalStack. Do not call a transport switch a one-line operational change.

### Step 23 — Deploy the full observability stack and its Ansible host

- [ ] Provision the dedicated host with Terraform; use Ansible for installation,
  hardening, service configuration, retention, backup, and repeatable upgrades.
- [ ] Add Grafana, Loki, VictoriaMetrics, Tempo, and Alloy with provisioned
  dashboards/data sources. Use GlitchTip here only if it is the selected error backend.
- [ ] Verify the actual CloudWatch/log collection and OTLP/scrape paths against
  pinned versions; avoid duplicate ingestion and unbounded retention costs.
- [ ] Test restoring the observability host and alerts for failure of telemetry itself.

Acceptance: hosted logs, metrics, traces, and errors correlate; dashboards survive
rebuilds; the host has tested backups. Fargate itself has no Ansible-managed hosts.

### Step 24 — Add guarded canary deployments

- [ ] Add two target groups/revisions and weighted HTTP traffic shifts only after
  ordinary deployment/rollback and metric collection work.
- [ ] Require minimum observation time/sample size plus latency/error gates; handle
  workflow cancellation and partial deployment failure deterministically.
- [ ] Design worker rollout separately: queue consumers are not governed by ALB
  weights. Prove schema/event compatibility with mixed worker revisions.
- [ ] Keep migrations compatible with old/new revisions and document rollback limits.

Acceptance: an injected bad API revision rolls back automatically; interrupted
deployment recovers; old workers cannot discard new product events.

### Step 25 — Introduce gRPC for a real processing boundary

- [ ] If validation requires independent scaling/isolation, extract the bounded
  processing component while the app retains workflow and database ownership.
- [ ] Define contracts under the owning app's `proto/`; use buf lint/generate/
  breaking checks, deadlines, authenticated internal transport, and tracing.
- [ ] Send durable job requests through the queue; use gRPC for a bounded processing
  call or progress/control stream with explicit retry and cancellation semantics.
- [ ] Pass only necessary immutable artifact references and context; the processing
  service must not reach into another app's tables or import its domain packages.

Acceptance: a stopped/slow processor causes a recoverable attempt and clear telemetry;
the product still has one authoritative workflow state. Do not add reciprocal API ↔
worker RPC merely to demonstrate that gRPC is installed.

### Step 26 — Expand authorization and hosting when there is a concrete need

- [ ] Add nested collections or explicit resource sharing before introducing an
  OpenFGA adapter. Define how access changes become effective while authorization
  projections catch up; revocation must not rely on stale grants indefinitely.
- [ ] Run a shared permission suite against RBAC and OpenFGA for their supported
  policies; document which richer policies only the new model supports.
- [ ] Implement Cloudflare Workers/OpenNext as the first alternate frontend host,
  then Amplify if useful. Revalidate same-origin sessions, SSR, OAuth redirects,
  storage CORS, streaming, and realtime routing for each supported topology.
- [ ] If a BFF is introduced, explicitly handle Set-Cookie, token redaction, refresh
  coordination, CSRF, and a separate verified WebSocket route/ticket design.

Acceptance: each advertised host passes the browser smoke suite against deployed
backend services. A workflow input is exposed only after its target works.

### Step 27 — Extend the product based on actual usage

- [ ] Add scoped/revocable API keys and automation clients if teams need scheduled imports.
- [ ] Add signed outbound webhooks with delivery history/replay, bounded retries,
  destination validation, and SSRF protections as a separate complete feature.
- [ ] Consider additional file formats, column mapping, scheduled intake, SFTP,
  or tenant-owned storage one at a time, each with lifecycle/security/upgrade tests.
- [ ] Add Redis fan-out/shared rate limiting, Pyroscope, or another storage adapter
  when measurements or deployment requirements justify them.
- [ ] Extend agent skills for frontend, authorization, gRPC, Terraform, alerts,
  ADRs, and review from proven workflows. Test each against a disposable example.
- [ ] Publish technical case studies using actual failures, load measurements,
  recovery evidence, costs, and decisions made during the product's development.

Acceptance: each addition solves a documented user/operator problem and has a
complete UI/API, tests, documentation, and deployment story. Avoid an endless backlog
of partially implemented adapters.

## Coverage of the original design

| Original capability | Placement in this plan |
|---|---|
| Spec-first Go, pgx, goose, errors, tests, Makefile | Retained; Steps 01–08 harden existing behavior |
| Custom auth, Google, verification/reset, session invalidation | Steps 02–07 and browser integration in 11 |
| Organizations, invitations, RBAC, projects, audit | Steps 09–10; intake definitions replace generic projects |
| Outbox, SNS/SQS, jobs, mail, LocalStack | Steps 04–08 and real product execution in 12–16 |
| Next.js, TypeScript, Tailwind, shadcn, typed client, TanStack | Step 11 onward, with Vitest/Playwright in 16 |
| WebSockets and Postgres fan-out | Step 19, backed by persistent notifications |
| ECS, ALB, RDS, ECR, SES, IAM, GitHub OIDC, Terraform | Step 17; S3 added for the product |
| Dev/staging/prod, budgets, backups, restore, runbooks | Steps 17–20 |
| OTel, logs, metrics, tracing, Sentry/GlitchTip, pprof | Step 18; complete self-hosted stack in 23 |
| Grafana/Loki/VictoriaMetrics/Tempo/Alloy + Ansible | Step 23 |
| k6, failure testing, measured performance | Step 20 and release evidence in 21 |
| NATS adapter and provider-independent deployment | Step 22 |
| Canary traffic shifting and automatic rollback | Step 24 |
| gRPC, buf, internal auth/service discovery | Step 25 when a real processing boundary exists |
| OpenFGA, Redis fan-out, Pyroscope | Steps 26–27, each tied to a concrete requirement |
| Cloudflare/OpenNext and Amplify | Step 26; first release uses a same-origin ECS frontend |
| Reusable agent skills and public template | Existing skills retained; Steps 21 and 27 package proven workflows |

The intentional changes from the draft are an earlier usable frontend, an explicit
product domain, stronger correctness gates before deployment, and one initial
hosting topology. Deferred capabilities have a place in the backlog and do not
prevent shipping the complete v1 workflow.

## Next work session

Start with Steps 01 and 02. Capture the baseline, make tooling reproducible, fix
the SMTP flake, then add the stale-session regression and session-generation fix.
Continue with safe Google linking and atomic registration/verification. Do not
begin another transport, frontend host, or microservice while those guarantees
remain unproven.
