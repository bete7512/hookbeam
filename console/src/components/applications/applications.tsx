"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import { keys, unwrap, useApplications } from "@/api/queries";
import { Field, FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { useEnvironment } from "@/lib/environment";
import { when } from "@/lib/format";

export function Applications() {
  const { current } = useEnvironment();
  const envId = current?.id ?? null;
  const apps = useApplications(envId);
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Applications</h1>
        <p className="text-muted-foreground">One per recipient in {current?.name ?? "the environment"}; each owns its endpoints and keys.</p>
      </header>
      {envId !== null && <CreateApplication environmentId={envId} />}
      {apps.isPending && envId !== null && <LoadingState />}
      {apps.isError && <QueryState error={apps.error} onRetry={() => apps.refetch()} />}
      {apps.data && apps.data.length === 0 && <EmptyState title="No applications yet" hint="Create the first one above; it gets the endpoints and keys for one recipient." />}
      {apps.data && apps.data.length > 0 && (
        <table className="w-full text-sm">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2 pr-4 font-medium">Name</th>
              <th className="py-2 pr-4 font-medium">Created</th>
              <th className="py-2 pr-4 font-medium">State</th>
            </tr>
          </thead>
          <tbody>
            {apps.data.map((a) => (
              <tr key={a.id} className="border-t">
                <td className="py-2 pr-4">
                  <Link href={`/applications/${a.id}`} className="underline" data-testid="application-link">
                    {a.name}
                  </Link>
                </td>
                <td className="py-2 pr-4">{when(a.createdAt)}</td>
                <td className="py-2 pr-4">{a.archivedAt ? <Badge>archived</Badge> : <Badge tone="good">active</Badge>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function CreateApplication({ environmentId }: { environmentId: number }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/environments/{environmentId}/applications", { params: { path: { environmentId } }, body: { name } })),
    onSuccess: () => {
      setName("");
      queryClient.invalidateQueries({ queryKey: keys.applications(environmentId) });
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>New application</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <Field id="application-name" label="Name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} className="min-w-64" />
          <Button type="submit" disabled={create.isPending}>
            Create application
          </Button>
          <FormError message={create.isError ? asApiError(create.error).message : null} />
        </form>
      </CardContent>
    </Card>
  );
}
