"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import { cancelInvitation, invite, removeMember, setWorkspaceRole } from "@/api/auth";
import { keys, unwrap, useInvitations, useMe, useMembers } from "@/api/queries";
import { Field, FormError, FormNotice } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";
import { can, deny, goauthRole, type Role, roles } from "@/lib/roles";

// Team lists the workspace's members with the role the API applies, the pending invitations, and the controls the
// signed-in role may use. Owners and admins manage members; only owners touch owners. The API is the real check:
// a refusal from it is shown as it came.
export function Team() {
  const me = useMe();
  const orgId = me.data?.workspaceId ?? null;
  const role = me.data?.role ?? null;
  const members = useMembers(orgId);
  if (me.isPending || members.isPending) return <LoadingState />;
  if (members.isError) return <QueryState error={members.error} onRetry={() => members.refetch()} />;
  const manage = can(role, "manageMembers");
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Team</h1>
        <p className="text-muted-foreground">
          Who can see and change this workspace. Owners and admins manage the team; developers read payloads and replay; viewers read metadata only.
        </p>
      </header>
      <Card>
        <CardHeader>
          <CardTitle>Members</CardTitle>
          <CardDescription>{manage ? "Change a role from the list, or remove a member. The workspace keeps at least one owner." : deny(role, "manageMembers") + "; the list is read-only."}</CardDescription>
        </CardHeader>
        <CardContent>
          <table className="w-full text-sm" data-testid="members">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-2 pr-4 font-medium">Member</th>
                <th className="py-2 pr-4 font-medium">Role</th>
                <th className="py-2 pr-4 font-medium">Joined</th>
                <th className="py-2 pr-4 font-medium"></th>
              </tr>
            </thead>
            <tbody>
              {(members.data ?? []).map((m) => (
                <MemberRow key={m.userId} member={m} orgId={orgId ?? ""} actorRole={role} self={m.userId === me.data?.userId} />
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
      <Invitations orgId={orgId ?? ""} actorRole={role} />
    </div>
  );
}

type MemberView = NonNullable<ReturnType<typeof useMembers>["data"]>[number];

function MemberRow({ member, orgId, actorRole, self }: { member: MemberView; orgId: string; actorRole: string | null; self: boolean }) {
  const queryClient = useQueryClient();
  const [confirmRemove, setConfirmRemove] = useState(false);
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: keys.members });
    if (self) queryClient.invalidateQueries({ queryKey: keys.me });
  };
  // A role change may take two steps: goauth's role first (owner, admin, member), then Hookbeam's refinement of a
  // member into developer or viewer.
  const change = useMutation({
    mutationFn: async (next: Role) => {
      const target = goauthRole(next);
      if (target !== goauthRole(member.role)) await setWorkspaceRole(orgId, member.userId, target);
      if (next === "developer" || next === "viewer") {
        await unwrap(api.PUT("/v1/members/{userId}/role", { params: { path: { userId: member.userId } }, body: { role: next } }));
      }
    },
    onSuccess: refresh,
  });
  const remove = useMutation({ mutationFn: () => removeMember(orgId, member.userId), onSuccess: refresh });
  const manage = can(actorRole, "manageMembers");
  const touchesOwner = member.role === "owner";
  const mayChange = manage && (!touchesOwner || can(actorRole, "manageOwners"));
  const reason = !manage ? deny(actorRole, "manageMembers") : touchesOwner && !can(actorRole, "manageOwners") ? deny(actorRole, "manageOwners") : null;
  const options = roles.filter((r) => r !== "owner" || can(actorRole, "manageOwners"));
  return (
    <tr className="border-t align-top" data-testid="member-row">
      <td className="py-2 pr-4">
        <span className="font-medium" data-testid="member-name">
          {member.name || member.email || member.userId}
        </span>
        {member.email && member.name && <span className="block text-xs text-muted-foreground">{member.email}</span>}
        {self && <span className="block text-xs text-muted-foreground">you</span>}
      </td>
      <td className="py-2 pr-4">
        <label className="sr-only" htmlFor={`role-${member.userId}`}>
          Role of {member.name || member.email}
        </label>
        <select
          id={`role-${member.userId}`}
          className="h-8 rounded-md border border-input bg-background px-2 text-sm disabled:opacity-60"
          value={member.role}
          disabled={!mayChange || change.isPending}
          title={reason ?? undefined}
          onChange={(e) => change.mutate(e.target.value as Role)}
          data-testid="member-role"
        >
          {options.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
        {reason && (
          <span className="block text-xs text-muted-foreground" data-testid="role-reason">
            {reason}
          </span>
        )}
        <FormError message={change.isError ? asApiError(change.error).message : null} />
      </td>
      <td className="py-2 pr-4" title={when(member.joinedAt)}>
        {ago(member.joinedAt)}
      </td>
      <td className="py-2 pr-4 text-right">
        {mayChange && !self && !confirmRemove && (
          <Button variant="ghost" size="sm" onClick={() => setConfirmRemove(true)} data-testid="remove-member">
            Remove
          </Button>
        )}
        {confirmRemove && (
          <span className="inline-flex items-center gap-2 text-xs">
            Remove {member.email || member.userId}?
            <Button variant="destructive" size="sm" onClick={() => remove.mutate()} disabled={remove.isPending} data-testid="confirm-remove">
              Remove
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirmRemove(false)}>
              Keep
            </Button>
          </span>
        )}
        <FormError message={remove.isError ? asApiError(remove.error).message : null} />
      </td>
    </tr>
  );
}

function Invitations({ orgId, actorRole }: { orgId: string; actorRole: string | null }) {
  const queryClient = useQueryClient();
  const list = useInvitations(orgId || null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"admin" | "member">("member");
  const [sent, setSent] = useState<string | null>(null);
  const manage = can(actorRole, "manageMembers");
  const refresh = () => queryClient.invalidateQueries({ queryKey: keys.invitations(orgId) });
  const send = useMutation({
    mutationFn: () => invite(orgId, email, role),
    onSuccess: (inv) => {
      setSent(`Invitation sent to ${inv.email}; it expires ${when(inv.expires_at)}.`);
      setEmail("");
      refresh();
    },
  });
  const revoke = useMutation({ mutationFn: (id: string) => cancelInvitation(orgId, id), onSuccess: refresh });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Invitations</CardTitle>
        <CardDescription>
          An invitation is emailed with a link that expires. Invite as a developer (goauth member) or an admin; make someone a viewer from the member list once they joined.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {manage ? (
          <form
            className="flex flex-wrap items-end gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              setSent(null);
              send.mutate();
            }}
          >
            <Field id="invite-email" label="Email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} className="min-w-64" />
            <div className="flex flex-col gap-1">
              <label htmlFor="invite-role" className="text-sm font-medium">
                Role
              </label>
              <select id="invite-role" className="h-9 rounded-md border border-input bg-background px-2 text-sm" value={role} onChange={(e) => setRole(e.target.value as "admin" | "member")}>
                <option value="member">developer</option>
                {can(actorRole, "manageOwners") || actorRole === "admin" ? <option value="admin">admin</option> : null}
              </select>
            </div>
            <Button type="submit" disabled={send.isPending} data-testid="send-invite">
              Invite
            </Button>
            <FormError message={send.isError ? asApiError(send.error).message : null} />
            <FormNotice message={sent} />
          </form>
        ) : (
          <p className="text-sm text-muted-foreground" data-testid="invite-denied">
            {deny(actorRole, "manageMembers")}.
          </p>
        )}
        {list.isPending && <LoadingState />}
        {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
        {list.data && list.data.length === 0 && <p className="text-sm text-muted-foreground">No pending invitation.</p>}
        {list.data && list.data.length > 0 && (
          <table className="w-full text-sm" data-testid="invitations">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-2 pr-4 font-medium">Email</th>
                <th className="py-2 pr-4 font-medium">Role</th>
                <th className="py-2 pr-4 font-medium">Expires</th>
                <th className="py-2 pr-4 font-medium"></th>
              </tr>
            </thead>
            <tbody>
              {list.data.map((inv) => (
                <tr key={inv.id} className="border-t" data-testid="invitation-row">
                  <td className="py-2 pr-4">{inv.email}</td>
                  <td className="py-2 pr-4">
                    <Badge>{inv.role === "member" ? "developer" : inv.role}</Badge>
                  </td>
                  <td className="py-2 pr-4">{when(inv.expires_at)}</td>
                  <td className="py-2 pr-4 text-right">
                    {manage && (
                      <Button variant="ghost" size="sm" onClick={() => revoke.mutate(inv.id)} disabled={revoke.isPending} data-testid="revoke-invitation">
                        Revoke
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <FormError message={revoke.isError ? asApiError(revoke.error).message : null} />
      </CardContent>
    </Card>
  );
}
