"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Archive, ChevronRight, Plus, Search, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useApplications, useLimits, useMe } from "@/api/queries";
import { Field, FormError, FormNotice } from "@/components/form";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogActions } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { useEnvironment } from "@/lib/environment";
import { ago, when } from "@/lib/format";
import { can, deny } from "@/lib/roles";
import { cn } from "@/lib/utils";

import { ArchiveApplicationDialog, DeleteApplicationDialog, archiveSummary, plural, purgeSummary } from "./lifecycle";

type Application = components["schemas"]["Application"];
type View = "active" | "archived";

// Applications lists the environment's applications, active ones first; archived ones sit behind their own filter
// with the option to delete them for good.
export function Applications() {
  const { current } = useEnvironment();
  const envId = current?.id ?? null;
  const apps = useApplications(envId);
  const limits = useLimits();
  const me = useMe();
  const role = me.data?.role ?? null;
  const mayManage = can(role, "manageEndpoints");
  const [search, setSearch] = useState("");
  const [view, setView] = useState<View>("active");
  const [creating, setCreating] = useState(false);
  const [archiving, setArchiving] = useState<Application | null>(null);
  const [deleting, setDeleting] = useState<Application | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const endpointCounts = useMemo(() => new Map((limits.data?.usage.applications ?? []).map((a) => [a.applicationId, a.endpoints])), [limits.data]);
  const all = apps.data ?? [];
  const counts = { active: all.filter((a) => !a.archivedAt).length, archived: all.filter((a) => a.archivedAt).length };
  const needle = search.trim().toLowerCase();
  const shown = all.filter((a) => (view === "archived" ? !!a.archivedAt : !a.archivedAt)).filter((a) => needle === "" || a.name.toLowerCase().includes(needle));

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{current ? current.name : "Environment"}</p>
          <h1 className="text-2xl font-semibold tracking-tight">Applications</h1>
          <p className="text-muted-foreground">One per recipient; each owns its endpoints, keys and events.</p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <Button variant="beam" onClick={() => setCreating(true)} disabled={!mayManage || envId === null} data-testid="new-application">
            <Plus className="h-4 w-4" aria-hidden />
            New application
          </Button>
          {!mayManage && me.data && <span className="text-xs text-muted-foreground">{deny(role, "manageEndpoints")}</span>}
        </div>
      </header>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input aria-label="Search applications" placeholder="Search applications" value={search} onChange={(e) => setSearch(e.target.value)} className="pl-9" />
        </div>
        <div role="group" aria-label="Show" className="inline-flex rounded-lg border bg-card p-0.5 text-sm">
          {(["active", "archived"] as const).map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={view === v}
              onClick={() => setView(v)}
              data-testid={`filter-${v}`}
              className={cn(
                "inline-flex items-center gap-2 rounded-md px-3 py-1.5 capitalize transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                view === v ? "bg-secondary font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {v}
              <span className="tabular-nums text-xs text-muted-foreground">{counts[v]}</span>
            </button>
          ))}
        </div>
      </div>

      <FormNotice message={notice} />

      {envId === null && <LoadingState label="Waiting for the environment…" />}
      {apps.isPending && envId !== null && <LoadingState label="Loading applications…" />}
      {apps.isError && <QueryState error={apps.error} onRetry={() => apps.refetch()} />}
      {apps.data && all.length === 0 && (
        <EmptyState
          title="No applications yet"
          hint="An application is one recipient of your events: it gets its own endpoints and keys."
          action={
            mayManage ? (
              <Button variant="beam" onClick={() => setCreating(true)}>
                <Plus className="h-4 w-4" aria-hidden />
                Create the first application
              </Button>
            ) : undefined
          }
        />
      )}
      {apps.data && all.length > 0 && shown.length === 0 && (
        <EmptyState title={needle ? "No application matches" : view === "archived" ? "Nothing archived" : "No active application"} hint={needle ? "Try another name, or look under the other filter." : view === "archived" ? "Archived applications are listed here until they are deleted." : "Every application here is archived."} />
      )}
      {shown.length > 0 && (
        <ul className="overflow-hidden rounded-lg border bg-card" data-testid="applications">
          {shown.map((a) => (
            <li key={a.id} className="group flex items-center gap-2 border-b pr-3 transition-colors last:border-b-0 hover:bg-secondary/40" data-testid="application-row">
              <Link href={`/applications/${a.id}`} className="flex min-w-0 flex-1 items-center gap-4 px-4 py-3.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring" data-testid="application-link">
                <Avatar name={a.name} size={36} className={cn("rounded-lg", a.archivedAt && "opacity-50 grayscale")} />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate font-medium">{a.name}</span>
                  <span className="text-xs text-muted-foreground" title={when(a.createdAt)}>
                    Application {a.id} · created {ago(a.createdAt)}
                  </span>
                </span>
                {!a.archivedAt && endpointCounts.has(a.id) && <span className="hidden text-sm text-muted-foreground sm:inline">{plural(endpointCounts.get(a.id) ?? 0, "endpoint")}</span>}
                {a.archivedAt ? <Badge>archived {ago(a.archivedAt)}</Badge> : <Badge tone="good" dot>active</Badge>}
                <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" aria-hidden />
              </Link>
              {mayManage && !a.archivedAt && (
                <Button variant="ghost" size="sm" onClick={() => setArchiving(a)} aria-label={`Archive ${a.name}`} title="Archive" data-testid="row-archive">
                  <Archive className="h-4 w-4" aria-hidden />
                </Button>
              )}
              {mayManage && a.archivedAt && (
                <Button variant="ghost" size="sm" onClick={() => setDeleting(a)} aria-label={`Delete ${a.name} permanently`} title="Delete permanently" className="text-destructive hover:text-destructive" data-testid="row-delete">
                  <Trash2 className="h-4 w-4" aria-hidden />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {creating && envId !== null && <CreateApplicationDialog environmentId={envId} onClose={() => setCreating(false)} />}
      {archiving && (
        <ArchiveApplicationDialog
          application={archiving}
          onClose={() => setArchiving(null)}
          onArchived={(res) => {
            setNotice(`“${archiving.name}”: ${archiveSummary(res)}`);
            setArchiving(null);
          }}
        />
      )}
      {deleting && (
        <DeleteApplicationDialog
          application={deleting}
          onClose={() => setDeleting(null)}
          onDeleted={(res) => {
            setNotice(purgeSummary(deleting.name, res));
            setDeleting(null);
          }}
        />
      )}
    </div>
  );
}

// CreateApplicationDialog names a new application and opens it, since the next step is always its first endpoint.
function CreateApplicationDialog({ environmentId, onClose }: { environmentId: number; onClose: () => void }) {
  const queryClient = useQueryClient();
  const router = useRouter();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/environments/{environmentId}/applications", { params: { path: { environmentId } }, body: { name: name.trim() } })),
    onSuccess: (app) => {
      queryClient.invalidateQueries({ queryKey: keys.applications(environmentId) });
      queryClient.invalidateQueries({ queryKey: keys.limits });
      router.push(`/applications/${app.id}`);
    },
  });
  const error = create.isError ? asApiError(create.error) : null;
  return (
    <Dialog open onClose={onClose} title="New application" description="Name it after the recipient or the system that will receive the events.">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Field id="application-name" label="Name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} placeholder="Checkout" error={error?.fields?.name} autoFocus autoComplete="off" />
        <FormError message={error && !error.fields?.name ? error.message : null} />
        <DialogActions>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={create.isPending || name.trim() === ""}>
            {create.isPending ? "Creating…" : "Create application"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
