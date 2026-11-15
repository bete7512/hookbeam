"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap } from "@/api/queries";
import { Field, FieldErrors, FormError, FormNotice } from "@/components/form";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogActions } from "@/components/ui/dialog";
import { TagInput } from "@/components/ui/tag-input";
import { when } from "@/lib/format";

import { HeaderEditor, headersPatch, noHeaderChanges, type HeaderChanges } from "./header-editor";

type Endpoint = components["schemas"]["Endpoint"];

// ConfigurationTab edits where and what the endpoint receives. Saving creates a new revision and is conditional
// on the one that was loaded, so two editors cannot overwrite each other.
export function ConfigurationTab({ endpoint, mayManage, reason }: { endpoint: Endpoint; mayManage: boolean; reason: string }) {
  const queryClient = useQueryClient();
  const [url, setUrl] = useState(endpoint.url);
  const [description, setDescription] = useState(endpoint.description);
  const [eventTypes, setEventTypes] = useState(endpoint.eventTypes);
  const [headers, setHeaders] = useState<HeaderChanges>(noHeaderChanges);
  // A new revision (ours or someone else's) replaces what the form shows.
  useEffect(() => {
    setUrl(endpoint.url);
    setDescription(endpoint.description);
    setEventTypes(endpoint.eventTypes);
    setHeaders(noHeaderChanges);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [endpoint.revision]);
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PATCH("/v1/endpoints/{endpointId}", {
          params: { path: { endpointId: endpoint.id } },
          body: { url: url.trim(), description: description.trim(), eventTypes, headers: headersPatch(headers), expectedRevision: endpoint.revision },
        }),
      ),
    onSuccess: (saved) => {
      // The response is the endpoint at its new revision: put it in the cache now, so the form resets once, at the
      // moment of the save, and never under someone who has already started the next edit.
      queryClient.setQueryData(keys.endpoint(endpoint.id), (prev: Endpoint | undefined) => ({ ...prev, ...saved }));
      queryClient.invalidateQueries({ queryKey: keys.endpoint(endpoint.id) });
      queryClient.invalidateQueries({ queryKey: keys.endpoints(endpoint.applicationId) });
    },
  });
  const archived = !!endpoint.archivedAt;
  const blocked = archived ? "an archived endpoint cannot change" : !mayManage ? reason : null;
  const error = save.isError ? asApiError(save.error) : null;
  const fields = error?.fields ?? {};
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Configuration</CardTitle>
          <CardDescription>Deliveries already planned keep the revision they were planned under; every attempt records the revision and URL it actually used.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex max-w-2xl flex-col gap-5"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate();
            }}
          >
            <Field id="edit-url" label="URL" type="url" required value={url} onChange={(e) => setUrl(e.target.value)} disabled={!!blocked} error={fields.url} />
            <Field id="edit-description" label="Description" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} disabled={!!blocked} error={fields.description} placeholder="What receives these events" />
            <TagInput id="edit-types" label="Event types" values={eventTypes} onChange={setEventTypes} disabled={!!blocked} placeholder="every event type" hint="Press Enter after each type. Leave empty to receive every type." />
            <HeaderEditor names={endpoint.headerNames} changes={headers} onChange={setHeaders} disabled={!!blocked} />
            <FieldErrors fields={fields} skip={["url", "description"]} />
            <FormError message={error && Object.keys(fields).length === 0 ? error.message : null} />
            <FormNotice message={save.isSuccess ? `Saved as revision ${save.data.revision}.` : null} />
            <div className="flex flex-wrap items-center gap-3">
              <Button type="submit" disabled={save.isPending || !!blocked} data-testid="save-endpoint">
                {save.isPending ? "Saving…" : "Save changes"}
              </Button>
              <span className="text-xs text-muted-foreground">{blocked ?? `Saves as revision ${endpoint.revision + 1}, if nobody changed revision ${endpoint.revision} meanwhile.`}</span>
            </div>
          </form>
        </CardContent>
      </Card>
      <Archive endpoint={endpoint} mayManage={mayManage} reason={reason} />
    </>
  );
}

function Archive({ endpoint, mayManage, reason }: { endpoint: Endpoint; mayManage: boolean; reason: string }) {
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const archive = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/endpoints/{endpointId}", { params: { path: { endpointId: endpoint.id } } })),
    onSuccess: () => {
      setConfirm(false);
      queryClient.invalidateQueries({ queryKey: keys.endpoint(endpoint.id) });
      queryClient.invalidateQueries({ queryKey: keys.endpoints(endpoint.applicationId) });
      queryClient.invalidateQueries({ queryKey: keys.endpointDeliveries(endpoint.id) });
      queryClient.invalidateQueries({ queryKey: keys.limits });
    },
  });
  const waiting = endpoint.waiting ?? 0;
  return (
    <Card className="border-destructive/30">
      <CardHeader>
        <CardTitle>Danger zone</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center justify-between gap-3 text-sm">
        <div className="max-w-xl">
          <p className="font-medium">Archive this endpoint</p>
          <p className="text-muted-foreground">
            {endpoint.archivedAt ? `Archived ${when(endpoint.archivedAt)}. It receives nothing; its deliveries and attempts stay readable.` : "It stops matching new events and its waiting deliveries are cancelled. History stays readable. This cannot be undone."}
          </p>
          {archive.data && (
            <p role="status" className="mt-1" data-testid="archive-endpoint-result">
              Archived; {archive.data.cancelledDeliveries} waiting {archive.data.cancelledDeliveries === 1 ? "delivery was" : "deliveries were"} cancelled.
            </p>
          )}
          {!mayManage && <p className="mt-1 text-xs text-muted-foreground">{reason}</p>}
        </div>
        {!endpoint.archivedAt && (
          <Button variant="outline" className="border-destructive/40 text-destructive hover:bg-destructive/10" disabled={!mayManage} onClick={() => setConfirm(true)} data-testid="archive-endpoint">
            Archive endpoint
          </Button>
        )}
      </CardContent>
      <Dialog open={confirm} onClose={() => setConfirm(false)} title="Archive this endpoint?" description={endpoint.url}>
        <p className="text-sm text-muted-foreground">
          {waiting === 0 ? "No delivery is waiting." : `${waiting} waiting ${waiting === 1 ? "delivery is" : "deliveries are"} cancelled.`} An attempt already in flight finishes and records its outcome.
        </p>
        <FormError message={archive.isError ? asApiError(archive.error).message : null} />
        <DialogActions>
          <Button variant="ghost" onClick={() => setConfirm(false)}>
            Keep it
          </Button>
          <Button variant="destructive" onClick={() => archive.mutate()} disabled={archive.isPending} data-testid="confirm-archive-endpoint">
            {archive.isPending ? "Archiving…" : "Archive endpoint"}
          </Button>
        </DialogActions>
      </Dialog>
    </Card>
  );
}
