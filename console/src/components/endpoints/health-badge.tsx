import type { components } from "@/api/schema";
import { Badge } from "@/components/ui/badge";
import { ago } from "@/lib/format";

type Health = components["schemas"]["EndpointHealth"];

// HealthBadge says the state in words, adds the observation age, and flags a stale view.
export function HealthBadge({ health }: { health: Health }) {
  const tone = health.state === "healthy" ? "good" : health.state === "unhealthy" ? "bad" : "neutral";
  return (
    <span className="inline-flex flex-col gap-0.5" data-testid="health-badge" data-state={health.state}>
      <Badge tone={tone}>{health.state}</Badge>
      <span className="text-xs text-muted-foreground">
        {health.observedAt ? `observed ${ago(health.observedAt)}` : "no attempt yet"}
        {health.stale ? " · stale" : ""}
        {health.consecutiveFailures > 0 ? ` · ${health.consecutiveFailures} failing` : ""}
      </span>
    </span>
  );
}
