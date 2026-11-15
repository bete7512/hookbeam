"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Hourglass, KeyRound, ListTree, Pause, Play, Send, Settings2 } from "lucide-react";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useApplication, useEndpoint, useEndpointDeliveries, useMe } from "@/api/queries";
import { FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Breadcrumb } from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog, DialogActions } from "@/components/ui/dialog";
import { Stat } from "@/components/ui/stat";
import { TabPanel, Tabs, useTabParam } from "@/components/ui/tabs";
import { LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";
import { can, deny } from "@/lib/roles";

import { ConfigurationTab } from "./configuration-tab";
import { DeliveriesTab } from "./deliveries-tab";
import { SecretsTab } from "./secrets-tab";
import { TestDeliveryPanel, useTestDelivery } from "./test-delivery";

type Endpoint = components["schemas"]["Endpoint"];

const tabs = ["deliveries", "configuration", "secrets"] as const;

// EndpointDetail is one destination: what it is and how it is doing, the actions an operator reaches for (test,
// pause, resume), then its deliveries, configuration and signing secrets behind tabs.
export function EndpointDetail({ id }: { id: number }) {
  const ep = useEndpoint(id);
  if (ep.isPending) return <LoadingState />;
  if (ep.isError) return <QueryState error={ep.error} onRetry={() => ep.refetch()} />;
  return <Loaded endpoint={ep.data} />;
}

function waitingText(n: number) {
  return `${n} ${n === 1 ? "delivery is" : "deliveries are"} waiting`;
}

function Loaded({ endpoint: e }: { endpoint: Endpoint }) {
  const me = useMe();
  const app = useApplication(e.applicationId);
  const [tab, setTab] = useTabParam(tabs, "deliveries");
  const test = useTestDelivery(e.id);
  const succeeded = useEndpointDeliveries(e.id, { state: "succeeded", limit: 1, offset: 0 }, 15_000);
  const failed = useEndpointDeliveries(e.id, { state: "failed", limit: 1, offset: 0 }, 15_000);
  const role = me.data?.role ?? null;
  const mayManage = can(role, "manageEndpoints");
  const mayTest = can(role, "replay");
  const reason = deny(role, "manageEndpoints");
  const archived = !!e.archivedAt;
  const waiting = e.waiting ?? 0;
  const failedTotal = failed.data?.total ?? 0;
  const health = e.health;

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-4">
        <Breadcrumb
          items={[
            { label: "Applications", href: "/applications" },
            { label: app.data?.name ?? `Application ${e.applicationId}`, href: `/applications/${e.applicationId}` },
            { label: `Endpoint ${e.id}` },
          ]}
        />
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0 flex-1">
            <div className="flex items-start gap-2">
              <span className="mt-1 rounded border px-1.5 py-0.5 font-mono text-[11px] font-medium text-muted-foreground">POST</span>
              <h1 className="min-w-0 break-all font-mono text-xl font-semibold leading-8 tracking-tight" data-testid="endpoint-url">
                {e.url}
              </h1>
              <CopyButton value={e.url} label="Copy the URL" className="mt-0.5" />
            </div>
            {e.description && <p className="mt-1 text-muted-foreground">{e.description}</p>}
            <p className="mt-2 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              {archived ? <Badge>archived</Badge> : e.pausedAt ? <Badge tone="warn" dot>paused</Badge> : <Badge tone="good" dot>active</Badge>}
              <span>revision {e.revision}</span>
              <span>·</span>
              <span>{e.eventTypes.length === 0 ? "every event type" : `${e.eventTypes.length} event type${e.eventTypes.length === 1 ? "" : "s"}`}</span>
              <span>·</span>
              <span title={when(e.createdAt)}>created {ago(e.createdAt)}</span>
            </p>
          </div>
          {!archived && (
            <div className="flex flex-col items-end gap-1">
              <div className="flex gap-2">
                <PauseResume endpoint={e} mayManage={mayManage} />
                <Button variant="beam" onClick={() => test.send.mutate()} disabled={test.send.isPending || !mayTest} data-testid="send-test">
                  <Send className="h-4 w-4" aria-hidden />
                  {test.send.isPending ? "Sending…" : "Send test"}
                </Button>
              </div>
              {!mayManage && me.data && <span className="text-xs text-muted-foreground">{reason}</span>}
            </div>
          )}
        </div>
      </header>

      {e.pausedAt && !archived && (
        <p role="status" className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm">
          Paused {ago(e.pausedAt)}. <span data-testid="waiting-notice">{waitingText(waiting)}</span> and will be sent when you resume; cycles past their 24-hour deadline stay expired.
        </p>
      )}

      <TestDeliveryPanel test={test} />

      <section aria-label="Summary" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat
          label="Health"
          icon={health.state === "unhealthy" ? CircleX : CircleCheck}
          value={<span data-testid="health-badge" data-state={health.state}>{health.state}</span>}
          tone={health.state === "healthy" ? "good" : health.state === "unhealthy" ? "bad" : "neutral"}
          hint={`${health.observedAt ? `observed ${ago(health.observedAt)}` : "no attempt yet"}${health.stale ? " · stale" : ""}${health.consecutiveFailures > 0 ? ` · ${health.consecutiveFailures} failing in a row` : ""}`}
        />
        <Stat label="Succeeded" icon={CircleCheck} value={succeeded.data ? succeeded.data.total : "–"} hint="deliveries, all time" />
        <Stat label="Failed" icon={CircleX} value={failed.data ? failedTotal : "–"} tone={failedTotal > 0 ? "bad" : "neutral"} hint="gave up or were refused" />
        <Stat label="Waiting" icon={Hourglass} value={<span data-testid="waiting">{waiting}</span>} tone={waiting > 0 ? "warn" : "neutral"} hint="queued or scheduled to retry" />
      </section>

      <Tabs
        label="Endpoint sections"
        value={tab}
        onChange={setTab}
        items={[
          { id: "deliveries", label: "Deliveries", icon: ListTree },
          { id: "configuration", label: "Configuration", icon: Settings2 },
          { id: "secrets", label: "Signing secrets", icon: KeyRound, count: e.secrets?.length },
        ]}
      />
      <TabPanel id="deliveries" value={tab}>
        <DeliveriesTab endpointId={e.id} />
      </TabPanel>
      <TabPanel id="configuration" value={tab}>
        <ConfigurationTab endpoint={e} mayManage={mayManage} reason={reason} />
      </TabPanel>
      <TabPanel id="secrets" value={tab}>
        <SecretsTab endpoint={e} mayManage={mayManage} reason={reason} />
      </TabPanel>
    </div>
  );
}

// PauseResume holds or releases the endpoint's backlog. Pausing asks first, because deliveries pile up behind it.
function PauseResume({ endpoint, mayManage }: { endpoint: Endpoint; mayManage: boolean }) {
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: keys.endpoint(endpoint.id) });
    queryClient.invalidateQueries({ queryKey: keys.endpoints(endpoint.applicationId) });
  };
  const pause = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/pause", { params: { path: { endpointId: endpoint.id } } })),
    onSuccess: () => {
      setConfirm(false);
      invalidate();
    },
  });
  const resume = useMutation({ mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/resume", { params: { path: { endpointId: endpoint.id } } })), onSuccess: invalidate });
  const waiting = endpoint.waiting ?? 0;
  if (endpoint.pausedAt) {
    return (
      <div className="flex flex-col items-end gap-1">
        <Button variant="outline" onClick={() => resume.mutate()} disabled={resume.isPending || !mayManage} data-testid="resume">
          <Play className="h-4 w-4" aria-hidden />
          {resume.isPending ? "Resuming…" : "Resume"}
        </Button>
        <FormError message={resume.isError ? asApiError(resume.error).message : null} />
      </div>
    );
  }
  return (
    <>
      <Button variant="outline" onClick={() => setConfirm(true)} disabled={!mayManage} data-testid="pause">
        <Pause className="h-4 w-4" aria-hidden />
        Pause
      </Button>
      <Dialog open={confirm} onClose={() => setConfirm(false)} title="Pause this endpoint?" description="Nothing new is attempted until you resume." data-testid="pause-dialog">
        <p className="text-sm text-muted-foreground">
          {waitingText(waiting)} and will hold, along with every event accepted meanwhile; attempts already claimed may still finish and record their outcome. A cycle still paused when its 24-hour deadline passes expires.
        </p>
        <FormError message={pause.isError ? asApiError(pause.error).message : null} />
        <DialogActions>
          <Button variant="ghost" onClick={() => setConfirm(false)}>
            Keep delivering
          </Button>
          <Button onClick={() => pause.mutate()} disabled={pause.isPending} data-testid="confirm-pause">
            {pause.isPending ? "Pausing…" : "Pause endpoint"}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
