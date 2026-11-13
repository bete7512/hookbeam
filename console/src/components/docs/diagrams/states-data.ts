// The delivery state machine as the data plane implements it: delivery/repos/deliveries.go moves the rows,
// delivery/services/policy.go decides after each attempt.

import type { Port } from "@/components/docs/diagrams/flow";

export type StateTone = "neutral" | "info" | "warn" | "good" | "bad";

export interface DeliveryState {
  id: string;
  label: string;
  tone: StateTone;
  terminal: boolean;
  x: number;
  y: number;
  meaning: string;
  leaves: string; // what moves a delivery out of this state
}

export interface Transition {
  id: string;
  source: string;
  target: string;
  label: string;
  sourceSide?: Port;
  targetSide?: Port;
  offset?: number;
}

export const states: DeliveryState[] = [
  {
    id: "start",
    label: "accepted or replayed",
    tone: "neutral",
    terminal: false,
    x: 0,
    y: 270,
    meaning: "A new delivery cycle: AcceptMessage writes one per subscribed endpoint, ReplayDelivery writes one for a failed or cancelled cycle.",
    leaves: "Created directly as queued, due now, with a deadline 24 hours away (a replay's deadline is also capped by the payload's retention).",
  },
  {
    id: "queued",
    label: "queued",
    tone: "info",
    terminal: false,
    x: 330,
    y: 260,
    meaning: "Waiting for its first attempt. Claims skip it while its endpoint is paused or archived, or once a cancel was requested.",
    leaves: "A claim moves it to in_flight. A cancel or an archived endpoint cancels it. The sweep fails it with deadline_exceeded once its deadline passes.",
  },
  {
    id: "in_flight",
    label: "in_flight",
    tone: "info",
    terminal: false,
    x: 680,
    y: 260,
    meaning: "A worker holds it under a lease token and attempt_count already counts this attempt. Every write that ends the attempt is fenced by that token.",
    leaves: "The attempt's outcome through the retry policy; a release when nothing was sent; the sweep when the lease expired without an outcome.",
  },
  {
    id: "retry_scheduled",
    label: "retry_scheduled",
    tone: "warn",
    terminal: false,
    x: 680,
    y: 500,
    meaning: "A retryable failure with a durable next attempt time in due_at: the backoff with jitter, or a later Retry-After.",
    leaves: "A claim once due_at passes. A cancel or an archived endpoint cancels it; the sweep fails it once the deadline passes.",
  },
  {
    id: "succeeded",
    label: "succeeded",
    tone: "good",
    terminal: true,
    x: 1060,
    y: 60,
    meaning: "The receiver answered 2xx. A success is recorded even when a cancel was requested during the attempt.",
    leaves: "Terminal. A successful delivery is never replayed.",
  },
  {
    id: "failed",
    label: "failed",
    tone: "bad",
    terminal: true,
    x: 1060,
    y: 260,
    meaning: "The cycle ended without success; failure_reason says why: http_<status> or blocked_destination for a non-retryable result, attempts_exhausted after eight attempts, deadline_exceeded when the next attempt would pass the deadline.",
    leaves: "Terminal. A person or a key with deliveries:replay can start a new cycle while the payload is retained.",
  },
  {
    id: "cancelled",
    label: "cancelled",
    tone: "neutral",
    terminal: true,
    x: 330,
    y: 500,
    meaning: "Further attempts were stopped: cancelled by a person, or endpoint_archived when its endpoint or application was archived. What a receiver already got stays got.",
    leaves: "Terminal. It can be replayed like a failure.",
  },
];

// A release (the worker failed before sending anything) returns in_flight to queued or retry_scheduled without
// counting an attempt; it is described on the in_flight state rather than drawn, to keep the picture readable.
export const transitions: Transition[] = [
  { id: "start-queued", source: "start", target: "queued", label: "due now · 24 h deadline", sourceSide: "right", targetSide: "left" },
  { id: "queued-in_flight", source: "queued", target: "in_flight", label: "claimed with a lease", sourceSide: "right", targetSide: "left" },
  { id: "in_flight-succeeded", source: "in_flight", target: "succeeded", label: "2xx", sourceSide: "right-1", targetSide: "left" },
  { id: "in_flight-failed", source: "in_flight", target: "failed", label: "no retry left", sourceSide: "right", targetSide: "left" },
  { id: "in_flight-retry_scheduled", source: "in_flight", target: "retry_scheduled", label: "retryable, or lease lost", sourceSide: "bottom-1", targetSide: "top-1" },
  { id: "retry_scheduled-in_flight", source: "retry_scheduled", target: "in_flight", label: "due again", sourceSide: "top-2", targetSide: "bottom-2" },
  { id: "in_flight-cancelled", source: "in_flight", target: "cancelled", label: "cancel requested · archived", sourceSide: "left-2", targetSide: "right-1" },
  { id: "retry_scheduled-cancelled", source: "retry_scheduled", target: "cancelled", label: "cancel · archive", sourceSide: "left", targetSide: "right-2" },
  { id: "queued-cancelled", source: "queued", target: "cancelled", label: "cancel · archive", sourceSide: "bottom", targetSide: "top" },
  { id: "retry_scheduled-failed", source: "retry_scheduled", target: "failed", label: "past deadline (sweep)", sourceSide: "right", targetSide: "bottom" },
  { id: "queued-failed", source: "queued", target: "failed", label: "past deadline (sweep)", sourceSide: "top", targetSide: "top", offset: 50 },
];
