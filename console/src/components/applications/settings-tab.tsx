"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap } from "@/api/queries";
import { FormError } from "@/components/form";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { Switch } from "@/components/ui/switch";
import { when } from "@/lib/format";

import { ArchiveApplicationDialog, DeleteApplicationDialog, archiveSummary } from "./lifecycle";

type Application = components["schemas"]["Application"];

// SettingsTab holds what describes the application, its one setting, and the two ways to retire it: archive
// (history stays) and, once archived, delete for good.
export function SettingsTab({ application, mayManage, reason }: { application: Application; mayManage: boolean; reason: string }) {
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>About</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-[10rem_1fr] items-center gap-x-4 gap-y-3 text-sm">
            <dt className="text-muted-foreground">Name</dt>
            <dd>{application.name}</dd>
            <dt className="text-muted-foreground">Application id</dt>
            <dd className="flex items-center gap-2">
              <code className="rounded bg-secondary px-1.5 py-0.5 text-xs">{application.id}</code>
              <CopyButton value={String(application.id)} label="Copy the application id" className="h-7" />
            </dd>
            <dt className="text-muted-foreground">Environment</dt>
            <dd className="capitalize">{application.environmentKind}</dd>
            <dt className="text-muted-foreground">Created</dt>
            <dd>{when(application.createdAt)}</dd>
            {application.archivedAt && (
              <>
                <dt className="text-muted-foreground">Archived</dt>
                <dd>{when(application.archivedAt)}</dd>
              </>
            )}
          </dl>
        </CardContent>
      </Card>
      <Previews application={application} mayManage={mayManage} reason={reason} />
      <DangerZone application={application} mayManage={mayManage} reason={reason} />
    </>
  );
}

function Previews({ application, mayManage, reason }: { application: Application; mayManage: boolean; reason: string }) {
  const queryClient = useQueryClient();
  const toggle = useMutation({
    mutationFn: (next: boolean) => unwrap(api.PATCH("/v1/applications/{applicationId}", { params: { path: { applicationId: application.id } }, body: { responsePreviewsEnabled: next } })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.application(application.id) }),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Response previews</CardTitle>
        <CardDescription>Keep the first bytes of what a receiver answers and show them, with the response headers, to people allowed to read payloads.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <div className="flex items-center gap-3 text-sm">
          <Switch checked={application.responsePreviewsEnabled} onChange={(next) => toggle.mutate(next)} disabled={!mayManage || toggle.isPending || !!application.archivedAt} label="Show response previews" data-testid="previews-switch" />
          <span>{application.responsePreviewsEnabled ? "Previews are shown" : "Previews are hidden"}</span>
        </div>
        {!mayManage && <p className="text-xs text-muted-foreground">{reason}</p>}
        <FormError message={toggle.isError ? asApiError(toggle.error).message : null} />
      </CardContent>
    </Card>
  );
}

function DangerZone({ application, mayManage, reason }: { application: Application; mayManage: boolean; reason: string }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [dialog, setDialog] = useState<"archive" | "delete" | null>(null);
  const [archived, setArchived] = useState<string | null>(null);
  const isArchived = !!application.archivedAt;
  return (
    <Card className="border-destructive/30">
      <CardHeader>
        <CardTitle>Danger zone</CardTitle>
        <CardDescription>{mayManage ? "Both actions are final. Archive keeps the history; delete removes it." : `${reason}.`}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col text-sm">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
          <div className="max-w-xl">
            <p className="font-medium">Archive this application</p>
            <p className="text-muted-foreground">
              {isArchived ? `Archived ${when(application.archivedAt)}. It accepts no events; its history stays readable.` : "Stops it for good: new events are refused, every endpoint is archived and waiting deliveries are cancelled. History stays readable."}
            </p>
            {archived && (
              <p role="status" className="mt-1" data-testid="archive-result">
                {archived}
              </p>
            )}
          </div>
          {!isArchived && (
            <Button variant="outline" className="border-destructive/40 text-destructive hover:bg-destructive/10" disabled={!mayManage} onClick={() => setDialog("archive")} data-testid="archive-application">
              Archive application
            </Button>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 pt-4">
          <div className="max-w-xl">
            <p className="font-medium">Delete permanently</p>
            <p className="text-muted-foreground">
              {isArchived ? "Removes the application with its endpoints, events, deliveries and attempts, and revokes its keys. Nothing brings it back." : "Available once the application is archived, so nothing is still being delivered when the data goes."}
            </p>
          </div>
          <Button variant="destructive" disabled={!mayManage || !isArchived} onClick={() => setDialog("delete")} data-testid="delete-application">
            Delete permanently
          </Button>
        </div>
      </CardContent>
      {dialog === "archive" && (
        <ArchiveApplicationDialog
          application={application}
          onClose={() => setDialog(null)}
          onArchived={(res) => {
            setArchived(archiveSummary(res));
            setDialog(null);
          }}
        />
      )}
      {dialog === "delete" && (
        <DeleteApplicationDialog
          application={application}
          onClose={() => setDialog(null)}
          onDeleted={() => {
            queryClient.removeQueries({ queryKey: keys.endpoints(application.id) });
            router.replace("/applications");
          }}
        />
      )}
    </Card>
  );
}
