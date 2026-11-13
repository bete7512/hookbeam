// The system as one graph: the architecture page explores it, the lifecycle page steps a message through it.
// Positions are hand-placed; groups are the two planes. Every `code` path is checked against the repository by
// src/components/docs/docs.test.ts, so a moved file fails the console's tests instead of leaving a dead link.

import type { Port } from "@/components/docs/diagrams/flow";

export type ComponentKind = "external" | "edge" | "process" | "database" | "queue";

export interface SystemComponent {
  id: string;
  label: string;
  detail: string; // one line under the label
  kind: ComponentKind;
  group?: "control" | "data";
  x: number;
  y: number;
  role: string; // what it is responsible for
  owns?: string[]; // state it owns
  code?: string[]; // repository paths
}

export interface Connection {
  id: string;
  source: string;
  target: string;
  label: string;
  sourceSide?: Port;
  targetSide?: Port;
  offset?: number;
}

export interface Group {
  id: "control" | "data";
  label: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export const groups: Group[] = [
  { id: "control", label: "Control plane · api/", x: 440, y: 0, width: 470, height: 320 },
  { id: "data", label: "Data plane · delivery/", x: 440, y: 380, width: 470, height: 320 },
];

export const components: SystemComponent[] = [
  {
    id: "sender",
    label: "Your backend",
    detail: "submits events with an API key",
    kind: "external",
    x: 0,
    y: 30,
    role: "The sending application. It submits each business event once with an Idempotency-Key and retries the same request, with the same key, whenever the outcome is unknown.",
  },
  {
    id: "browser",
    label: "Browser",
    detail: "the console, signed in",
    kind: "external",
    x: 0,
    y: 250,
    role: "People operating Hookbeam: they register endpoints, mint keys, read every attempt and replay failures. The session is goauth's cookie, first-party because the console and the API share one origin.",
  },
  {
    id: "edge",
    label: "One origin",
    detail: "proxy in front of console and API",
    kind: "edge",
    x: 215,
    y: 140,
    role: "Routes /v1/*, /openapi.json, /healthz and /readyz to the control plane and everything else to the console, so cookies are first-party and the console never needs to know the API's host.",
    code: ["deploy/proxy/Caddyfile"],
  },
  {
    id: "console",
    label: "Console",
    detail: "Next.js · these docs",
    kind: "process",
    x: 215,
    y: 340,
    role: "The web console and this documentation. Every screen reads the public API from the browser with the session cookie; the API client is generated from the control plane's OpenAPI document.",
    code: ["console/src/app/", "console/src/api/client.ts"],
  },
  {
    id: "api",
    label: "Control plane API",
    detail: "Go · HTTP · goauth",
    kind: "process",
    group: "control",
    x: 15,
    y: 50,
    role: "The public REST API. It authenticates sessions (goauth) and API keys, enforces the spec's security and scopes, applies the role matrix, and forwards every delivery operation to the data plane over gRPC.",
    owns: ["identity and workspaces (goauth)", "API keys and scopes", "roles", "audit of control actions"],
    code: ["api/cmd/api/main.go", "api/handlers/", "api/services/delivery.go"],
  },
  {
    id: "apiWorker",
    label: "Control plane worker",
    detail: "relay · event handlers",
    kind: "process",
    group: "control",
    x: 15,
    y: 220,
    role: "Publishes the control plane's outbox, consumes delivery state and health events from its queue and skips any event id it already handled, keeps a health projection, creates notifications and sends alert emails.",
    owns: ["endpoint_health projection", "notifications"],
    code: ["api/cmd/worker/main.go", "api/jobs/"],
  },
  {
    id: "controlDb",
    label: "Postgres · hookbeam",
    detail: "control plane database",
    kind: "database",
    group: "control",
    x: 275,
    y: 50,
    role: "The control plane's own database: API keys, member roles, audit events, notifications, the endpoint health projection, its outbox and processed events, and goauth's identity tables.",
    code: ["api/migrations/"],
  },
  {
    id: "delivery",
    label: "Delivery service",
    detail: "Go · gRPC · mTLS",
    kind: "process",
    group: "data",
    x: 15,
    y: 50,
    role: "The data plane's API, reachable only from the control plane over mutual TLS. It owns acceptance (idempotency, fan-out, quotas), endpoints and their secrets, and every read of messages, deliveries and attempts.",
    owns: ["environments, applications, endpoints", "messages, deliveries, attempts", "signing secrets (encrypted)"],
    code: ["delivery/cmd/server/main.go", "delivery/rpc/server.go", "delivery/services/messages.go"],
  },
  {
    id: "deliveryWorker",
    label: "Delivery worker",
    detail: "claim · sign · send · record",
    kind: "process",
    group: "data",
    x: 275,
    y: 220,
    role: "Claims due deliveries under a lease, signs and posts them through the SSRF guard, records each attempt and the next state in one transaction, recovers expired leases, applies retention and relays the data plane's outbox.",
    code: ["delivery/cmd/worker/main.go", "delivery/services/dispatch.go", "delivery/sender/http.go"],
  },
  {
    id: "deliveryDb",
    label: "Postgres · hookbeam_delivery",
    detail: "data plane database",
    kind: "database",
    group: "data",
    x: 275,
    y: 50,
    role: "The schedule itself: due_at, leases and deadlines live on the deliveries rows, so the queue never has to be trusted with work. Also messages with their exact payload bytes, attempts, health episodes and the outbox.",
    code: ["delivery/migrations/"],
  },
  {
    id: "sns",
    label: "SNS topic",
    detail: "hookbeam-events",
    kind: "queue",
    x: 970,
    y: 380,
    role: "One topic both planes publish to from their outboxes. Each subscription filters on the event's type attribute, so each worker receives only what it handles.",
    code: ["deploy/localstack/init.sh", "pkg/events/sns/"],
  },
  {
    id: "controlQueue",
    label: "SQS · hookbeam-worker",
    detail: "everything but delivery.due",
    kind: "queue",
    x: 970,
    y: 160,
    role: "The control plane worker's queue: delivery state events, health changes and notification emails. Three failed receives move a message to its dead-letter queue.",
    code: ["pkg/events/sqs/"],
  },
  {
    id: "dataQueue",
    label: "SQS · hookbeam-delivery-worker",
    detail: "delivery.due only",
    kind: "queue",
    x: 970,
    y: 550,
    role: "The delivery worker's wake-ups. A wake-up carries no work, only a signal to claim now; losing one delays a delivery until the next claim tick, it never loses it.",
  },
  {
    id: "mail",
    label: "Email",
    detail: "SMTP or SES",
    kind: "external",
    x: 970,
    y: 0,
    role: "Account emails (verification, reset, invitations) sent by goauth from the API, and alert emails sent by the control plane worker to verified owners and admins who keep email alerts on.",
    code: ["pkg/mailer/"],
  },
  {
    id: "receiver",
    label: "Receiver endpoint",
    detail: "your customer's HTTPS URL",
    kind: "external",
    x: 970,
    y: 730,
    role: "Where webhooks land. It verifies the signature over the exact body, deduplicates on webhook-id, and answers 2xx once the event is safely stored.",
    code: ["examples/receiver/handler.go"],
  },
];

export const connections: Connection[] = [
  { id: "sender-edge", source: "sender", target: "edge", label: "POST …/messages · API key", sourceSide: "right", targetSide: "left" },
  { id: "browser-edge", source: "browser", target: "edge", label: "pages and /v1 · cookie", sourceSide: "right", targetSide: "left" },
  { id: "edge-console", source: "edge", target: "console", label: "pages", sourceSide: "bottom", targetSide: "top" },
  { id: "edge-api", source: "edge", target: "api", label: "/v1/*", sourceSide: "right", targetSide: "left" },
  { id: "api-controlDb", source: "api", target: "controlDb", label: "SQL", sourceSide: "right", targetSide: "left" },
  { id: "api-delivery", source: "api", target: "delivery", label: "gRPC · mTLS", sourceSide: "left", targetSide: "top", offset: 40 },
  { id: "api-mail", source: "api", target: "mail", label: "account emails", sourceSide: "top", targetSide: "left" },
  { id: "delivery-deliveryDb", source: "delivery", target: "deliveryDb", label: "one transaction", sourceSide: "right", targetSide: "left" },
  { id: "deliveryWorker-deliveryDb", source: "deliveryWorker", target: "deliveryDb", label: "claim · record", sourceSide: "top", targetSide: "bottom" },
  { id: "deliveryWorker-sns", source: "deliveryWorker", target: "sns", label: "relay outbox", sourceSide: "right-1", targetSide: "left-2" },
  { id: "sns-dataQueue", source: "sns", target: "dataQueue", label: "type = delivery.due", sourceSide: "bottom", targetSide: "top" },
  { id: "sns-controlQueue", source: "sns", target: "controlQueue", label: "every other type", sourceSide: "top", targetSide: "bottom" },
  { id: "dataQueue-deliveryWorker", source: "dataQueue", target: "deliveryWorker", label: "wake up", sourceSide: "left", targetSide: "right-2" },
  { id: "deliveryWorker-receiver", source: "deliveryWorker", target: "receiver", label: "signed POST · SSRF guard", sourceSide: "bottom", targetSide: "left" },
  { id: "apiWorker-sns", source: "apiWorker", target: "sns", label: "relay outbox", sourceSide: "right-2", targetSide: "left-1" },
  { id: "controlQueue-apiWorker", source: "controlQueue", target: "apiWorker", label: "state events", sourceSide: "left", targetSide: "top" },
  { id: "apiWorker-controlDb", source: "apiWorker", target: "controlDb", label: "projections · notifications", sourceSide: "right-1", targetSide: "bottom" },
  { id: "apiWorker-delivery", source: "apiWorker", target: "delivery", label: "gRPC · GetHealthEpisode", sourceSide: "bottom", targetSide: "top" },
  { id: "apiWorker-mail", source: "apiWorker", target: "mail", label: "alert emails", sourceSide: "left", targetSide: "left-2", offset: 36 },
];

export interface LifecycleStep {
  title: string;
  body: string;
  nodes: string[];
  edges: string[];
  code: string[];
}

// One message from submission to the console, in the order the system does it.
export const lifecycle: LifecycleStep[] = [
  {
    title: "Submit",
    body: "Your backend posts the event with a key holding messages:write and an Idempotency-Key. The control plane looks the key up by its SHA-256 hash, checks its scope and its application binding, and counts it against the per-key rate limit.",
    nodes: ["sender", "edge", "api"],
    edges: ["sender-edge", "edge-api"],
    code: ["api/handlers/messages.go", "api/handlers/authn.go"],
  },
  {
    title: "Cross to the data plane",
    body: "The control plane forwards the exact request bytes to AcceptMessage over gRPC with mutual TLS. The workspace travels as call metadata, never as a request field, and the data plane's interceptor refuses a call without it.",
    nodes: ["api", "delivery"],
    edges: ["api-delivery"],
    code: ["api/deliveryclient/client.go", "pkg/grpcx/metadata.go"],
  },
  {
    title: "Commit the delivery plan",
    body: "The data plane validates the body, resolves the idempotency key, finds the subscribed endpoints and checks the quotas. Then one transaction writes the message (its payload as exact bytes), one delivery per endpoint due now with a 24-hour deadline, and a delivery.due row in the outbox. Only then does the sender get 202.",
    nodes: ["delivery", "deliveryDb"],
    edges: ["delivery-deliveryDb"],
    code: ["delivery/services/messages.go", "delivery/repos/messages.go"],
  },
  {
    title: "Relay the wake-up",
    body: "The delivery worker's relay claims unpublished outbox rows with FOR UPDATE SKIP LOCKED, publishes them to the topic and marks them published in the same transaction. The subscription filter sends delivery.due to the delivery worker's own queue.",
    nodes: ["deliveryWorker", "deliveryDb", "sns", "dataQueue"],
    edges: ["deliveryWorker-sns", "sns-dataQueue"],
    code: ["delivery/services/events.go", "deploy/localstack/init.sh"],
  },
  {
    title: "Wake and claim",
    body: "The wake-up only says: claim now. The worker takes one advisory lock, picks due deliveries fairly across endpoints within each endpoint's concurrency cap, and moves them to in_flight with a lease token valid for 60 seconds. A timer claims every 5 seconds anyway, so a lost wake-up only delays.",
    nodes: ["dataQueue", "deliveryWorker", "deliveryDb"],
    edges: ["dataQueue-deliveryWorker", "deliveryWorker-deliveryDb"],
    code: ["delivery/services/dispatch.go", "delivery/repos/deliveries.go"],
  },
  {
    title: "Sign and send",
    body: "Each claimed delivery runs in its own goroutine. The worker decrypts the endpoint's active secrets, signs message id, timestamp and body with each one, and posts through a client that refuses private and metadata addresses and follows no redirect, within a 10-second budget.",
    nodes: ["deliveryWorker", "receiver"],
    edges: ["deliveryWorker-receiver"],
    code: ["delivery/services/dispatch.go", "pkg/webhooks/webhooks.go", "pkg/safehttp/safehttp.go"],
  },
  {
    title: "Record the outcome",
    body: "One transaction writes the attempt row and the delivery's next state, fenced by the lease token, so a worker that lost its lease writes nothing. The same transaction updates endpoint health and writes a delivery state event to the outbox.",
    nodes: ["deliveryWorker", "deliveryDb"],
    edges: ["deliveryWorker-deliveryDb"],
    code: ["delivery/services/dispatch.go", "delivery/services/health.go"],
  },
  {
    title: "Tell the control plane",
    body: "The state event travels through the topic to the control plane's queue. Its worker handles each event, then records the event id in processed_events in a transaction that commits only if the handler succeeded, so a redelivered event is skipped. The handler updates the control plane's own health projection, where an older event never overwrites a newer one.",
    nodes: ["deliveryWorker", "sns", "controlQueue", "apiWorker", "controlDb"],
    edges: ["deliveryWorker-sns", "sns-controlQueue", "controlQueue-apiWorker", "apiWorker-controlDb"],
    code: ["api/jobs/jobs.go", "api/services/endpoint_health.go"],
  },
  {
    title: "Alert on sustained failure",
    body: "The third consecutive failed attempt opens a health episode in the data plane and announces it. The control plane worker rereads the episode, stores one notice per owner and admin, and queues an email for each who keeps email alerts on; the email goes out only to an address goauth reports as verified. The next success resolves the episode the same way.",
    nodes: ["apiWorker", "delivery", "controlDb", "mail"],
    edges: ["apiWorker-delivery", "apiWorker-controlDb", "apiWorker-mail"],
    code: ["api/services/notifications.go", "delivery/services/health.go"],
  },
  {
    title: "Read it back",
    body: "The console, or your backend with messages:read, asks for the message, its deliveries and every attempt: status, latency, endpoint revision, secret version and, where permitted, a bounded response preview. Every read is scoped by the workspace in the call metadata.",
    nodes: ["browser", "edge", "api", "delivery", "deliveryDb"],
    edges: ["browser-edge", "edge-api", "api-delivery", "delivery-deliveryDb"],
    code: ["api/handlers/messages.go", "delivery/rpc/server.go"],
  },
];
