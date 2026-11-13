// The smaller diagrams: the concept map, the trace through the queue, and the outbox path.

import type { Link } from "@/components/docs/diagrams/flow";

export interface Card {
  id: string;
  title: string;
  tag?: string;
  body?: string;
  mono?: boolean;
  tone?: "plain" | "accent" | "store" | "queue";
  x: number;
  y: number;
}

export interface Diagram {
  cards: Card[];
  links: (Link & { dashed?: boolean })[];
}

export const concepts: Diagram = {
  cards: [
    { id: "workspace", title: "Workspace", tag: "goauth organization", body: "Your team. Members, keys and everything below belong to one.", tone: "accent", x: 0, y: 0 },
    { id: "environment", title: "Environment", tag: "development or production", body: "Keeps production work apart from tests.", x: 270, y: 0 },
    { id: "application", title: "Application", tag: "a consumer", body: "One customer or system you send to; the recipient boundary.", x: 540, y: 0 },
    { id: "endpoint", title: "Endpoint", tag: "URL + revision", body: "Where webhooks land. It can be paused, edited and archived.", x: 810, y: 0 },
    { id: "subscription", title: "Subscription", tag: "event type", body: "The types an endpoint takes; none means every type.", x: 1080, y: 0 },
    { id: "secret", title: "Signing secret", tag: "versioned", body: "Encrypted at rest; a rotation overlaps two versions.", x: 1080, y: 160 },
    { id: "message", title: "Message", tag: "webhook-id", body: "One submitted event, kept as the exact bytes you sent.", tone: "store", x: 540, y: 320 },
    { id: "delivery", title: "Delivery", tag: "one per endpoint", body: "A cycle of attempts to one endpoint, with its own state and deadline.", tone: "store", x: 810, y: 320 },
    { id: "attempt", title: "Attempt", tag: "one HTTP request", body: "Status or error class, latency, and the revision and secret used.", tone: "store", x: 1080, y: 320 },
    { id: "apiKey", title: "API key", tag: "scopes", body: "A sender credential, workspace-wide or bound to one application.", x: 0, y: 190 },
    { id: "member", title: "Member", tag: "owner · admin · developer · viewer", body: "A person, and the role that decides what they may do.", x: 0, y: 360 },
  ],
  links: [
    { id: "c1", source: "workspace", target: "environment", label: "has" },
    { id: "c2", source: "environment", target: "application", label: "has" },
    { id: "c3", source: "application", target: "endpoint", label: "has" },
    { id: "c4", source: "endpoint", target: "subscription", label: "filters by" },
    { id: "c5", source: "endpoint", target: "secret", label: "signs with", sourceSide: "bottom", targetSide: "left" },
    { id: "c6", source: "application", target: "message", label: "receives", sourceSide: "bottom", targetSide: "top" },
    { id: "c7", source: "message", target: "delivery", label: "fans out to" },
    { id: "c8", source: "delivery", target: "attempt", label: "made of" },
    { id: "c9", source: "delivery", target: "endpoint", label: "targets", sourceSide: "top", targetSide: "bottom", dashed: true },
    { id: "c10", source: "workspace", target: "apiKey", label: "issues", sourceSide: "bottom", targetSide: "top" },
    { id: "c11", source: "apiKey", target: "application", label: "may be bound to", sourceSide: "right", targetSide: "bottom", dashed: true },
    { id: "c12", source: "workspace", target: "member", label: "has", sourceSide: "left", targetSide: "left" },
  ],
};

export const traceLinks: Diagram = {
  cards: [
    { id: "http", title: "POST /v1/applications/{applicationId}/messages", tag: "api", body: "HTTP server span", mono: true, tone: "accent", x: 0, y: 0 },
    { id: "client", title: "hookbeam.delivery.v1.DeliveryService/AcceptMessage", tag: "api", body: "gRPC client span", mono: true, x: 0, y: 150 },
    { id: "server", title: "hookbeam.delivery.v1.DeliveryService/AcceptMessage", tag: "delivery", body: "gRPC server span; its context is stored with the message and the outbox row", mono: true, x: 0, y: 300 },
    { id: "publish", title: "outbox.publish", tag: "delivery worker", body: "links every transaction whose rows the batch carries", mono: true, x: 360, y: 140 },
    { id: "wakeup", title: "wakeup delivery.due", tag: "delivery worker", body: "links the transaction that queued the wake-up", mono: true, x: 360, y: 300 },
    { id: "attempt", title: "delivery.attempt", tag: "delivery worker", body: "links the message's acceptance", mono: true, tone: "accent", x: 360, y: 470 },
    { id: "publish2", title: "outbox.publish", tag: "delivery worker", body: "publishes the delivery state event", mono: true, x: 720, y: 390 },
    { id: "event", title: "event delivery.succeeded", tag: "control worker", body: "links the attempt that wrote the event", mono: true, x: 720, y: 550 },
  ],
  links: [
    { id: "t1", source: "http", target: "client", label: "parent", sourceSide: "bottom", targetSide: "top" },
    { id: "t2", source: "client", target: "server", label: "parent", sourceSide: "bottom", targetSide: "top" },
    { id: "t3", source: "server", target: "publish", label: "link", sourceSide: "right-1", targetSide: "left", dashed: true },
    { id: "t4", source: "server", target: "wakeup", label: "link", dashed: true },
    { id: "t5", source: "server", target: "attempt", label: "link", sourceSide: "right-2", targetSide: "left", dashed: true },
    { id: "t6", source: "attempt", target: "publish2", label: "link", sourceSide: "right-1", targetSide: "left", dashed: true },
    { id: "t7", source: "attempt", target: "event", label: "link", sourceSide: "right-2", targetSide: "left", dashed: true },
  ],
};

export const outboxPath: Diagram = {
  cards: [
    { id: "tx", title: "One transaction", tag: "service", body: "The change and its event row commit together, or neither does.", tone: "accent", x: 0, y: 0 },
    { id: "row", title: "outbox", tag: "published_at NULL", body: "The event waits here, durable, until it is published.", mono: true, tone: "store", x: 310, y: 0 },
    { id: "relay", title: "Relay", tag: "every 500 ms", body: "Claims rows FOR UPDATE SKIP LOCKED, publishes, marks them published.", tone: "accent", x: 620, y: 0 },
    { id: "topic", title: "SNS topic", tag: "filter on type", body: "Each subscription takes only its worker's types.", tone: "queue", x: 930, y: 0 },
    { id: "queue", title: "SQS queue", tag: "3 receives, then DLQ", body: "At-least-once: a message can arrive twice.", tone: "queue", x: 930, y: 200 },
    { id: "consumer", title: "ProcessOnce", tag: "consumer", body: "Runs the handler; the event id is marked only if it succeeds.", tone: "accent", x: 620, y: 200 },
    { id: "processed", title: "processed_events", tag: "event_id", body: "A redelivered event finds its id and is skipped.", mono: true, tone: "store", x: 310, y: 200 },
  ],
  links: [
    { id: "o1", source: "tx", target: "row", label: "same commit" },
    { id: "o2", source: "row", target: "relay", label: "claim" },
    { id: "o3", source: "relay", target: "topic", label: "publish" },
    { id: "o4", source: "topic", target: "queue", label: "subscribe", sourceSide: "bottom", targetSide: "top" },
    { id: "o5", source: "queue", target: "consumer", label: "receive", sourceSide: "left", targetSide: "right" },
    { id: "o6", source: "consumer", target: "processed", label: "mark + handle", sourceSide: "left", targetSide: "right" },
  ],
};
