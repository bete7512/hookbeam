"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { RotateCw } from "lucide-react";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap } from "@/api/queries";
import { FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { SecretOnce } from "@/components/ui/secret-once";
import { until, when } from "@/lib/format";

type Endpoint = components["schemas"]["Endpoint"];

const overlaps = [
  { value: "0s", label: "Stop now (the old secret leaked)" },
  { value: "1h", label: "1 hour" },
  { value: "24h", label: "24 hours" },
  { value: "72h", label: "3 days" },
  { value: "168h", label: "7 days" },
];

// SecretsTab lists the signing secret versions by fingerprint and rotates: a new version is shown once, and the
// previous ones keep signing for the chosen overlap so the receiver can switch without missing a delivery.
export function SecretsTab({ endpoint, mayManage, reason }: { endpoint: Endpoint; mayManage: boolean; reason: string }) {
  const queryClient = useQueryClient();
  const [retireIn, setRetireIn] = useState("24h");
  const [rotated, setRotated] = useState<{ secret: string; version: number; retireAt: string } | null>(null);
  const rotate = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/rotate-secret", { params: { path: { endpointId: endpoint.id } }, body: { retireIn } })),
    onSuccess: (res) => {
      setRotated({ secret: res.signingSecret, version: res.secretVersion, retireAt: res.retireAt });
      queryClient.invalidateQueries({ queryKey: keys.endpoint(endpoint.id) });
    },
  });
  const blocked = endpoint.archivedAt ? "an archived endpoint keeps its secrets as they are" : !mayManage ? reason : null;
  return (
    <>
      {rotated && <SecretOnce label={`New signing secret, version ${rotated.version}`} value={rotated.secret} hint={`Previous versions stop signing ${when(rotated.retireAt)}.`} />}
      <Card>
        <CardHeader>
          <CardTitle>Versions</CardTitle>
          <CardDescription>Every delivery carries one signature per version that is still signing. Only fingerprints are kept in view.</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table data-testid="secret-versions">
              <thead>
                <tr className="border-b">
                  <th className="py-2 pr-4">Version</th>
                  <th className="py-2 pr-4">Fingerprint</th>
                  <th className="py-2 pr-4">Created</th>
                  <th className="py-2 pr-4">Status</th>
                </tr>
              </thead>
              <tbody>
                {(endpoint.secrets ?? []).map((s) => {
                  const retired = !!s.retireAt && new Date(s.retireAt).getTime() <= Date.now();
                  return (
                    <tr key={s.version} className="border-b last:border-b-0">
                      <td className="py-2.5 pr-4 tabular-nums">v{s.version}</td>
                      <td className="py-2.5 pr-4 font-mono text-xs">{s.fingerprint}</td>
                      <td className="py-2.5 pr-4 text-muted-foreground">{when(s.createdAt)}</td>
                      <td className="py-2.5 pr-4" title={s.retireAt ? when(s.retireAt) : undefined}>
                        {!s.retireAt ? (
                          <Badge tone="good" dot>
                            current
                          </Badge>
                        ) : retired ? (
                          <Badge>retired</Badge>
                        ) : (
                          <Badge tone="warn" dot>
                            signing, retires {until(s.retireAt)}
                          </Badge>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Rotate</CardTitle>
          <CardDescription>Creates a new version. Choose how long the previous ones keep signing while you deploy the new secret to the receiver.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-wrap items-end gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              rotate.mutate();
            }}
          >
            <div className="flex min-w-64 flex-col gap-1">
              <Label htmlFor="retire-in">Previous secrets keep signing for</Label>
              <select id="retire-in" className="h-9 px-2" value={retireIn} onChange={(e) => setRetireIn(e.target.value)} disabled={!!blocked}>
                {overlaps.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>
            <Button type="submit" variant="outline" disabled={rotate.isPending || !!blocked} data-testid="rotate">
              <RotateCw className="h-4 w-4" aria-hidden />
              {rotate.isPending ? "Rotating…" : "Rotate secret"}
            </Button>
          </form>
          {blocked && <p className="mt-2 text-xs text-muted-foreground">{blocked}</p>}
          <FormError message={rotate.isError ? asApiError(rotate.error).message : null} />
        </CardContent>
      </Card>
    </>
  );
}
