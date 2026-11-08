"use client";

import { useQuery } from "@tanstack/react-query";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMemo } from "react";

import { api } from "@/api/client";
import { unwrap } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";

const resourceTypes = ["endpoint", "delivery", "message", "api_key", "member", "environment", "application"];
const pageSize = 50;

interface AuditFilters {
  actorId: string;
  resourceType: string;
  resourceId: string;
  after: string;
  before: string;
  offset: number;
}

export function parseAuditFilters(params: URLSearchParams): AuditFilters {
  const offset = Number(params.get("offset"));
  return {
    actorId: params.get("actor_id") ?? "",
    resourceType: params.get("resource_type") ?? "",
    resourceId: params.get("resource_id") ?? "",
    after: params.get("after") ?? "",
    before: params.get("before") ?? "",
    offset: Number.isInteger(offset) && offset >= 0 ? offset : 0,
  };
}

export function serializeAuditFilters(f: AuditFilters): string {
  const p = new URLSearchParams();
  if (f.actorId) p.set("actor_id", f.actorId);
  if (f.resourceType) p.set("resource_type", f.resourceType);
  if (f.resourceId) p.set("resource_id", f.resourceId);
  if (f.after) p.set("after", f.after);
  if (f.before) p.set("before", f.before);
  if (f.offset) p.set("offset", String(f.offset));
  const q = p.toString();
  return q ? `?${q}` : "";
}

// Audit lists who did what in the workspace, from both planes, newest first; the filters live in the URL.
export function Audit() {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const filters = useMemo(() => parseAuditFilters(new URLSearchParams(params.toString())), [params]);
  const set = (next: Partial<AuditFilters>) => router.replace(`${pathname}${serializeAuditFilters({ ...filters, offset: 0, ...next })}`);
  const list = useQuery({
    queryKey: ["audit", filters],
    queryFn: () =>
      unwrap(
        api.GET("/v1/audit", {
          params: {
            query: {
              limit: pageSize,
              offset: filters.offset,
              actor_id: filters.actorId || undefined,
              resource_type: filters.resourceType || undefined,
              resource_id: filters.resourceId ? Number(filters.resourceId) : undefined,
              after: filters.after ? new Date(filters.after).toISOString() : undefined,
              before: filters.before ? new Date(filters.before).toISOString() : undefined,
            },
          },
        }),
      ),
    placeholderData: (prev) => prev,
  });
  const total = list.data?.total ?? 0;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Audit</h1>
        <p className="text-muted-foreground">Key, member, endpoint, delivery and payload-read actions, tagged by the plane that recorded them. Details never hold secrets or payload bytes.</p>
      </header>
      <form
        className="grid gap-3 rounded-md border p-4 sm:grid-cols-2 lg:grid-cols-5"
        onSubmit={(e) => {
          e.preventDefault();
          const d = new FormData(e.currentTarget);
          set({ actorId: String(d.get("actor_id") ?? ""), resourceId: String(d.get("resource_id") ?? ""), after: String(d.get("after") ?? ""), before: String(d.get("before") ?? "") });
        }}
      >
        <div className="flex flex-col gap-1">
          <Label htmlFor="a-actor">Actor id</Label>
          <Input id="a-actor" name="actor_id" defaultValue={filters.actorId} key={`actor-${filters.actorId}`} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="a-type">Resource</Label>
          <select id="a-type" className="h-9 rounded-md border border-input bg-background px-2 text-sm" value={filters.resourceType} onChange={(e) => set({ resourceType: e.target.value })} data-testid="resource-type">
            <option value="">any</option>
            {resourceTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="a-id">Resource id</Label>
          <Input id="a-id" name="resource_id" type="number" min={1} defaultValue={filters.resourceId} key={`id-${filters.resourceId}`} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="a-after">After</Label>
          <Input id="a-after" name="after" type="datetime-local" defaultValue={filters.after} key={`after-${filters.after}`} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="a-before">Before</Label>
          <Input id="a-before" name="before" type="datetime-local" defaultValue={filters.before} key={`before-${filters.before}`} />
        </div>
        <div className="flex items-end gap-2">
          <Button type="submit" variant="outline">
            Apply
          </Button>
          <Button type="button" variant="ghost" onClick={() => router.replace(pathname)}>
            Clear
          </Button>
        </div>
      </form>
      {list.isPending && <LoadingState />}
      {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
      {list.data && list.data.items.length === 0 && <EmptyState title="No audit events match" />}
      {list.data && list.data.items.length > 0 && (
        <>
          <table className="w-full text-sm" data-testid="audit-table">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-2 pr-4 font-medium">When</th>
                <th className="py-2 pr-4 font-medium">Action</th>
                <th className="py-2 pr-4 font-medium">Resource</th>
                <th className="py-2 pr-4 font-medium">Actor</th>
                <th className="py-2 pr-4 font-medium">Details</th>
              </tr>
            </thead>
            <tbody>
              {list.data.items.map((e) => (
                <tr key={`${e.source}-${e.id}`} className="border-t align-top" data-testid="audit-row">
                  <td className="py-2 pr-4 whitespace-nowrap" title={when(e.occurredAt)}>
                    {ago(e.occurredAt)}
                  </td>
                  <td className="py-2 pr-4">
                    <code className="text-xs">{e.action}</code>
                    <Badge className="ml-2" tone="neutral">
                      {e.source}
                    </Badge>
                  </td>
                  <td className="py-2 pr-4">
                    {e.resourceType}
                    {e.resourceId ? ` ${e.resourceId}` : ""}
                  </td>
                  <td className="py-2 pr-4 font-mono text-xs">{e.actorId || "api key"}</td>
                  <td className="py-2 pr-4 text-xs text-muted-foreground">
                    {Object.entries(e.details ?? {})
                      .map(([k, v]) => `${k}: ${typeof v === "object" ? JSON.stringify(v) : String(v)}`)
                      .join(" · ")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">{total} events</span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={filters.offset === 0} onClick={() => router.replace(`${pathname}${serializeAuditFilters({ ...filters, offset: Math.max(0, filters.offset - pageSize) })}`)}>
                Previous
              </Button>
              <Button variant="outline" size="sm" disabled={filters.offset + pageSize >= total} onClick={() => router.replace(`${pathname}${serializeAuditFilters({ ...filters, offset: filters.offset + pageSize })}`)}>
                Next
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
