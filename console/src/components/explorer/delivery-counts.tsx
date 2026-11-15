import type { components } from "@/api/schema";
import { Badge } from "@/components/ui/badge";

type Counts = components["schemas"]["DeliveryCounts"];

// DeliveryCountBadges says where a message's deliveries stand, one badge per group that has any.
export function DeliveryCountBadges({ counts }: { counts: Counts }) {
  const parts: { label: string; n: number; tone: "good" | "bad" | "warn" | "neutral" }[] = [
    { label: "succeeded", n: counts.succeeded, tone: "good" },
    { label: "failed", n: counts.failed, tone: "bad" },
    { label: "retrying", n: counts.retryScheduled + counts.queued + counts.inFlight, tone: "warn" },
    { label: "cancelled", n: counts.cancelled, tone: "neutral" },
  ];
  return (
    <span className="flex flex-wrap gap-1">
      {parts
        .filter((p) => p.n > 0)
        .map((p) => (
          <Badge key={p.label} tone={p.tone} dot pulse={p.tone === "warn"}>
            {p.n} {p.label}
          </Badge>
        ))}
    </span>
  );
}
