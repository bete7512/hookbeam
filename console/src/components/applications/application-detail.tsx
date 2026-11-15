"use client";

import { Activity, HeartPulse, KeyRound, ListTree, Settings2, Webhook } from "lucide-react";

import { useApiKeys, useApplication, useApplicationMessages, useEndpoints, useMe } from "@/api/queries";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Breadcrumb } from "@/components/ui/breadcrumb";
import { Stat } from "@/components/ui/stat";
import { TabPanel, Tabs, useTabParam } from "@/components/ui/tabs";
import { LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";
import { can, deny } from "@/lib/roles";

import { EndpointsTab } from "./endpoints-tab";
import { EventsTab, eventsPageSize } from "./events-tab";
import { KeysTab } from "./keys-tab";
import { SettingsTab } from "./settings-tab";

const tabs = ["endpoints", "keys", "events", "settings"] as const;

// ApplicationDetail is one application: a summary strip, then its endpoints, bound keys, events and settings
// behind tabs. The selected tab lives in the URL.
export function ApplicationDetail({ id }: { id: number }) {
  const app = useApplication(id);
  const me = useMe();
  const endpoints = useEndpoints(id);
  const apiKeys = useApiKeys();
  const events = useApplicationMessages(id, { limit: eventsPageSize, offset: 0 });
  const [tab, setTab] = useTabParam(tabs, "endpoints");
  if (app.isPending) return <LoadingState />;
  if (app.isError) return <QueryState error={app.error} onRetry={() => app.refetch()} />;
  const a = app.data;
  const role = me.data?.role ?? null;
  const mayManage = can(role, "manageEndpoints");
  const mayManageKeys = can(role, "manageKeys");
  const archived = !!a.archivedAt;

  const list = endpoints.data ?? [];
  const live = list.filter((e) => !e.archivedAt);
  const unhealthy = live.filter((e) => e.health.state === "unhealthy").length;
  const healthy = live.filter((e) => e.health.state === "healthy").length;
  const paused = live.filter((e) => e.pausedAt).length;
  const bound = (apiKeys.data ?? []).filter((k) => k.applicationId === id);
  const activeKeys = bound.filter((k) => !k.revokedAt).length;

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-4">
        <Breadcrumb items={[{ label: "Applications", href: "/applications" }, { label: a.name }]} />
        <div className="flex items-center gap-4">
          <Avatar name={a.name} size={48} className={`rounded-xl text-base ${archived ? "opacity-50 grayscale" : ""}`} />
          <div className="min-w-0">
            <h1 className="truncate text-2xl font-semibold tracking-tight" data-testid="application-name">
              {a.name}
            </h1>
            <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              {archived ? <Badge>archived</Badge> : <Badge tone="good" dot>active</Badge>}
              <Badge tone={a.environmentKind === "production" ? "bad" : "neutral"}>{a.environmentKind}</Badge>
              <span title={when(a.createdAt)}>
                Application {a.id} · created {ago(a.createdAt)}
              </span>
            </p>
          </div>
        </div>
      </header>

      <section aria-label="Summary" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat label="Endpoints" icon={Webhook} value={endpoints.data ? live.length : "–"} hint={endpoints.data ? (paused > 0 ? `${paused} paused` : live.length === 0 ? "none registered" : "all delivering") : undefined} />
        <Stat
          label="Unhealthy"
          icon={HeartPulse}
          value={endpoints.data ? unhealthy : "–"}
          tone={unhealthy > 0 ? "bad" : "neutral"}
          hint={endpoints.data ? `${healthy} healthy · ${live.length - healthy - unhealthy} not observed` : undefined}
        />
        <Stat label="Active keys" icon={KeyRound} value={apiKeys.data ? activeKeys : "–"} hint={apiKeys.data ? (bound.length - activeKeys > 0 ? `${bound.length - activeKeys} revoked` : "bound to this application") : undefined} />
        <Stat label="Events" icon={Activity} value={events.data ? events.data.total : "–"} hint="accepted so far" data-testid="stat-events" />
      </section>

      <Tabs
        label="Application sections"
        value={tab}
        onChange={setTab}
        items={[
          { id: "endpoints", label: "Endpoints", icon: Webhook, count: endpoints.data ? live.length : undefined },
          { id: "keys", label: "API keys", icon: KeyRound, count: apiKeys.data ? activeKeys : undefined },
          { id: "events", label: "Events", icon: ListTree },
          { id: "settings", label: "Settings", icon: Settings2 },
        ]}
      />
      <TabPanel id="endpoints" value={tab}>
        <EndpointsTab applicationId={id} archived={archived} mayManage={mayManage} reason={deny(role, "manageEndpoints")} />
      </TabPanel>
      <TabPanel id="keys" value={tab}>
        <KeysTab applicationId={id} archived={archived} mayManage={mayManageKeys} reason={deny(role, "manageKeys")} />
      </TabPanel>
      <TabPanel id="events" value={tab}>
        <EventsTab applicationId={id} />
      </TabPanel>
      <TabPanel id="settings" value={tab}>
        <SettingsTab application={a} mayManage={mayManage} reason={deny(role, "manageEndpoints")} />
      </TabPanel>
    </div>
  );
}
