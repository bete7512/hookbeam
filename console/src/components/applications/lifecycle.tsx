"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap } from "@/api/queries";
import { Field, FormError } from "@/components/form";
import { Button } from "@/components/ui/button";
import { Dialog, DialogActions } from "@/components/ui/dialog";

type Application = components["schemas"]["Application"];
export type ArchiveResult = components["schemas"]["ApplicationArchived"];
export type PurgeResult = components["schemas"]["ApplicationPurged"];

// plural writes "1 endpoint", "2 endpoints", "1 delivery", "3 deliveries".
export function plural(n: number, one: string, many = `${one}s`) {
  return `${n} ${n === 1 ? one : many}`;
}

// ArchiveApplicationDialog confirms the archive and says what stops. Mount it only while it is open.
export function ArchiveApplicationDialog({ application, onClose, onArchived }: { application: Application; onClose: () => void; onArchived: (result: ArchiveResult) => void }) {
  const queryClient = useQueryClient();
  const archive = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/applications/{applicationId}", { params: { path: { applicationId: application.id } } })),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: keys.application(application.id) });
      queryClient.invalidateQueries({ queryKey: keys.endpoints(application.id) });
      queryClient.invalidateQueries({ queryKey: ["applications"] });
      queryClient.invalidateQueries({ queryKey: keys.limits });
      onArchived(res);
    },
  });
  return (
    <Dialog open onClose={onClose} title={`Archive “${application.name}”?`} description="Archiving retires the application. Its history stays readable, and it cannot be brought back." data-testid="archive-dialog">
      <ul className="flex list-disc flex-col gap-1.5 pl-5 text-sm text-muted-foreground">
        <li>New events are refused, so keys bound to it stop submitting.</li>
        <li>Every endpoint is archived and deliveries still waiting are cancelled.</li>
        <li>Attempts already in flight finish and record their outcome.</li>
      </ul>
      <FormError message={archive.isError ? asApiError(archive.error).message : null} />
      <DialogActions>
        <Button variant="ghost" onClick={onClose}>
          Keep it
        </Button>
        <Button variant="destructive" onClick={() => archive.mutate()} disabled={archive.isPending} data-testid="confirm-archive">
          {archive.isPending ? "Archiving…" : "Archive application"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

// DeleteApplicationDialog removes an archived application for good; the name has to be typed, because nothing
// brings the data back. Mount it only while it is open.
export function DeleteApplicationDialog({ application, onClose, onDeleted }: { application: Application; onClose: () => void; onDeleted: (result: PurgeResult) => void }) {
  const queryClient = useQueryClient();
  const [typed, setTyped] = useState("");
  const purge = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/applications/{applicationId}/purge", { params: { path: { applicationId: application.id } } })),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ["applications"] });
      queryClient.invalidateQueries({ queryKey: keys.apiKeys });
      queryClient.invalidateQueries({ queryKey: keys.limits });
      onDeleted(res);
    },
  });
  const matches = typed.trim() === application.name;
  return (
    <Dialog open onClose={onClose} title={`Delete “${application.name}” for good?`} description="This removes the application and everything recorded under it. It cannot be undone." data-testid="delete-dialog">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (matches) purge.mutate();
        }}
      >
        <ul className="flex list-disc flex-col gap-1.5 pl-5 text-sm text-muted-foreground">
          <li>Its endpoints, signing secrets and subscriptions are deleted.</li>
          <li>Its events, deliveries and attempts are deleted, payloads included.</li>
          <li>Keys bound to it are revoked. Audit entries stay.</li>
        </ul>
        <Field id="confirm-application-name" label={`Type “${application.name}” to confirm`} value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" autoFocus data-testid="confirm-name" />
        <FormError message={purge.isError ? asApiError(purge.error).message : null} />
        <DialogActions>
          <Button type="button" variant="ghost" onClick={onClose}>
            Keep it
          </Button>
          <Button type="submit" variant="destructive" disabled={!matches || purge.isPending} data-testid="confirm-delete">
            {purge.isPending ? "Deleting…" : "Delete permanently"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

// archiveSummary is the sentence shown after an archive.
export function archiveSummary(result: ArchiveResult) {
  return `Archived ${plural(result.archivedEndpoints, "endpoint")} and cancelled ${plural(result.cancelledDeliveries, "waiting delivery", "waiting deliveries")}.`;
}

// purgeSummary is the sentence shown after a permanent delete.
export function purgeSummary(name: string, result: PurgeResult) {
  return `Deleted “${name}”: ${plural(result.deletedEndpoints, "endpoint")}, ${plural(result.deletedMessages, "event")}, ${plural(result.deletedDeliveries, "delivery", "deliveries")}; ${plural(result.revokedKeys, "key")} revoked.`;
}
