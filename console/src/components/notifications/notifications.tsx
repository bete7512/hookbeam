"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useNotificationPreferences, useNotifications } from "@/api/queries";
import { FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";

type Notification = components["schemas"]["Notification"];

// Notifications lists the workspace's health notices, newest first, with the unread filter in the URL and the
// email preference next to it.
export function Notifications() {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const unreadOnly = params.get("unread") === "1";
  const list = useNotifications(unreadOnly);
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.notifications });
  const markRead = useMutation({
    mutationFn: (id: number) => unwrap(api.POST("/v1/notifications/{notificationId}/read", { params: { path: { notificationId: id } } })),
    onSuccess: refresh,
  });
  const markAll = useMutation({
    mutationFn: async () => {
      const unread = (list.data?.items ?? []).filter((n) => !n.readAt);
      for (const n of unread) await unwrap(api.POST("/v1/notifications/{notificationId}/read", { params: { path: { notificationId: n.id } } }));
    },
    onSuccess: refresh,
  });
  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Notifications</h1>
          <p className="text-muted-foreground">An endpoint that fails three times in a row opens an episode; the next success closes it. Owners and admins are notified.</p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={unreadOnly} onChange={(e) => router.replace(e.target.checked ? `${pathname}?unread=1` : pathname)} data-testid="unread-only" />
            unread only
          </label>
          <Button variant="outline" size="sm" onClick={() => markAll.mutate()} disabled={markAll.isPending || !list.data || list.data.unread === 0} data-testid="mark-all-read">
            Mark all read
          </Button>
        </div>
      </header>
      <Preferences />
      {list.isPending && <LoadingState />}
      {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
      {list.data && list.data.items.length === 0 && <EmptyState title={unreadOnly ? "Nothing unread" : "No notifications yet"} hint="Health changes of your endpoints appear here." />}
      {list.data && list.data.items.length > 0 && (
        <ol className="flex flex-col gap-2" data-testid="notifications">
          {list.data.items.map((n) => (
            <Notice key={n.id} notice={n} onRead={() => markRead.mutate(n.id)} pending={markRead.isPending} />
          ))}
        </ol>
      )}
      <p className="text-sm text-muted-foreground" data-testid="unread-count">
        {list.data ? `${list.data.unread} unread` : ""}
      </p>
      <FormError message={markRead.isError ? asApiError(markRead.error).message : markAll.isError ? asApiError(markAll.error).message : null} />
    </div>
  );
}

function Notice({ notice, onRead, pending }: { notice: Notification; onRead: () => void; pending: boolean }) {
  const unread = !notice.readAt;
  return (
    <li className={`rounded-md border p-3 text-sm ${unread ? "border-foreground/30" : "text-muted-foreground"}`} data-testid="notice" data-unread={unread}>
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={notice.kind === "failure" ? "bad" : "good"}>{notice.kind}</Badge>
        <span className="font-medium">{notice.title}</span>
        <span className="ml-auto text-xs" title={when(notice.createdAt)}>
          {ago(notice.createdAt)}
        </span>
      </div>
      <p className="mt-1">{notice.body}</p>
      <p className="mt-1 text-xs">
        <Link href={`/endpoints/${notice.endpointId}`} className="underline">
          {notice.endpointUrl}
        </Link>{" "}
        · application {notice.applicationId} · episode {notice.episodeId}
      </p>
      {unread && (
        <Button variant="ghost" size="sm" className="mt-2" onClick={onRead} disabled={pending} data-testid="mark-read">
          Mark read
        </Button>
      )}
    </li>
  );
}

function Preferences() {
  const prefs = useNotificationPreferences();
  const queryClient = useQueryClient();
  const save = useMutation({
    mutationFn: (emailAlerts: boolean) => unwrap(api.PUT("/v1/me/notification-preferences", { body: { emailAlerts } })),
    onSuccess: (data) => queryClient.setQueryData(keys.notificationPreferences, data),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Email</CardTitle>
        <CardDescription>In-app notices are always kept. Emails go to a verified address only.</CardDescription>
      </CardHeader>
      <CardContent>
        {prefs.isPending && <LoadingState />}
        {prefs.isError && <QueryState error={prefs.error} onRetry={() => prefs.refetch()} />}
        {prefs.data && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={prefs.data.emailAlerts} disabled={save.isPending} onChange={(e) => save.mutate(e.target.checked)} data-testid="email-alerts" />
            Email me when an endpoint becomes unhealthy or recovers
          </label>
        )}
        <FormError message={save.isError ? asApiError(save.error).message : null} />
      </CardContent>
    </Card>
  );
}
