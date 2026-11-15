"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Plus, Webhook } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useEndpoints } from "@/api/queries";
import { Field, FieldErrors, FormError } from "@/components/form";
import { HeaderEditor, headersForCreate, noHeaderChanges, type HeaderChanges } from "@/components/endpoints/header-editor";
import { HealthBadge } from "@/components/endpoints/health-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogActions } from "@/components/ui/dialog";
import { SecretOnce } from "@/components/ui/secret-once";
import { TagInput } from "@/components/ui/tag-input";
import { EmptyState, LoadingState, QueryState } from "@/components/states";

type Endpoint = components["schemas"]["Endpoint"];
type Created = components["schemas"]["EndpointCreated"];

// EndpointsTab lists the application's destinations and registers new ones.
export function EndpointsTab({ applicationId, archived, mayManage, reason }: { applicationId: number; archived: boolean; mayManage: boolean; reason: string }) {
  const list = useEndpoints(applicationId);
  const [adding, setAdding] = useState(false);
  const blocked = archived ? "an archived application takes no new endpoints" : !mayManage ? reason : null;
  // The same action sits in the toolbar and in the empty state; only the toolbar one carries the test id.
  const add = (testId?: string) => (
    <Button onClick={() => setAdding(true)} disabled={!!blocked} data-testid={testId}>
      <Plus className="h-4 w-4" aria-hidden />
      Add endpoint
    </Button>
  );
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">Destination URLs. Each has its own signing secret and receives the event types it subscribes to.</p>
        <div className="flex flex-col items-end gap-1">
          {add("add-endpoint")}
          {blocked && <span className="text-xs text-muted-foreground">{blocked}</span>}
        </div>
      </div>
      {list.isPending && <LoadingState label="Loading endpoints…" />}
      {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
      {list.data && list.data.length === 0 && <EmptyState title="No endpoints yet" hint="Register the URL that should receive this application's events." action={blocked ? undefined : add()} />}
      {list.data && list.data.length > 0 && (
        <ul className="overflow-hidden rounded-lg border bg-card" data-testid="endpoints">
          {list.data.map((e) => (
            <EndpointRow key={e.id} endpoint={e} />
          ))}
        </ul>
      )}
      {adding && <AddEndpointDialog applicationId={applicationId} onClose={() => setAdding(false)} />}
    </>
  );
}

function EndpointRow({ endpoint: e }: { endpoint: Endpoint }) {
  const shown = e.eventTypes.slice(0, 3);
  return (
    <li className="group border-b transition-colors last:border-b-0 hover:bg-secondary/40">
      <Link href={`/endpoints/${e.id}`} className="flex items-center gap-4 px-4 py-3.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring" data-testid="endpoint-link">
        <span className="hidden h-9 w-9 shrink-0 items-center justify-center rounded-lg border bg-background text-muted-foreground sm:inline-flex">
          <Webhook className="h-4 w-4" aria-hidden />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="truncate font-mono text-sm">{e.url}</span>
          <span className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
            {e.description && <span className="mr-1 truncate">{e.description}</span>}
            {e.eventTypes.length === 0 ? (
              <span>every event type</span>
            ) : (
              <>
                {shown.map((t) => (
                  <code key={t} className="rounded bg-secondary px-1.5 py-0.5 text-[11px] text-secondary-foreground">
                    {t}
                  </code>
                ))}
                {e.eventTypes.length > shown.length && <span>+{e.eventTypes.length - shown.length} more</span>}
              </>
            )}
            <span>· revision {e.revision}</span>
          </span>
        </span>
        <span className="hidden md:block">
          <HealthBadge health={e.health} />
        </span>
        {e.archivedAt ? <Badge>archived</Badge> : e.pausedAt ? <Badge tone="warn" dot>paused</Badge> : <Badge tone="good" dot>active</Badge>}
        <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" aria-hidden />
      </Link>
    </li>
  );
}

// AddEndpointDialog registers an endpoint, then stays open on the signing secret, which is shown this once.
function AddEndpointDialog({ applicationId, onClose }: { applicationId: number; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [url, setUrl] = useState("");
  const [description, setDescription] = useState("");
  const [eventTypes, setEventTypes] = useState<string[]>([]);
  const [headers, setHeaders] = useState<HeaderChanges>(noHeaderChanges);
  const [created, setCreated] = useState<Created | null>(null);
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/v1/applications/{applicationId}/endpoints", {
          params: { path: { applicationId } },
          body: { url: url.trim(), description: description.trim() || undefined, eventTypes, headers: headersForCreate(headers) },
        }),
      ),
    onSuccess: (res) => {
      setCreated(res);
      queryClient.invalidateQueries({ queryKey: keys.endpoints(applicationId) });
      queryClient.invalidateQueries({ queryKey: keys.limits });
    },
  });
  const error = create.isError ? asApiError(create.error) : null;
  const fieldErrors = error?.fields ?? {};
  if (created) {
    return (
      <Dialog open onClose={onClose} title="Endpoint registered" description="Give the signing secret to the receiver, which verifies every delivery with it.">
        <SecretOnce label={`Signing secret, version ${created.secretVersion}`} value={created.signingSecret} hint="You can rotate it later from the endpoint." />
        <DialogActions>
          <Button variant="ghost" onClick={onClose}>
            Done
          </Button>
          <Button asChild>
            <Link href={`/endpoints/${created.endpoint.id}`} data-testid="open-endpoint">
              Open endpoint
            </Link>
          </Button>
        </DialogActions>
      </Dialog>
    );
  }
  return (
    <Dialog open onClose={onClose} title="Add endpoint" description="Hookbeam posts each event here, signed, and retries on failure.">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Field id="endpoint-url" label="URL" type="url" required value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://api.example.com/webhooks/hookbeam" error={fieldErrors.url} hint="An https URL; plain http and private addresses only where the data plane allows them." autoFocus />
        <Field id="endpoint-description" label="Description" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Ledger service" error={fieldErrors.description} />
        <TagInput id="endpoint-types" label="Event types" values={eventTypes} onChange={setEventTypes} placeholder="order.shipped" hint="Press Enter after each type. Leave empty to receive every type." />
        <HeaderEditor names={[]} changes={headers} onChange={setHeaders} />
        <FieldErrors fields={fieldErrors} skip={["url", "description"]} />
        <FormError message={error && Object.keys(fieldErrors).length === 0 ? error.message : null} />
        <DialogActions>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={create.isPending}>
            {create.isPending ? "Registering…" : "Register endpoint"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
