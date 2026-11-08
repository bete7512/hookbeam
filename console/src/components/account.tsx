"use client";

import { useMutation } from "@tanstack/react-query";
import { useState } from "react";

import { changePassword, sendVerification } from "@/api/auth";
import { asApiError } from "@/api/errors";
import { useMe, useProfile } from "@/api/queries";
import { Field, FormError, FormNotice } from "@/components/form";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";

// Account shows the profile goauth holds, the current session, and lets the person change their password.
// goauth's session list and per-session revocation belong to its session module, which this build does not
// register (tokens are stateless), so the only revocation here is signing out.
export function Account() {
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Account</h1>
      </header>
      <ProfileCard />
      <SessionInfoCard />
      <ChangePasswordCard />
    </div>
  );
}

function ProfileCard() {
  const prof = useProfile();
  const resend = useMutation({ mutationFn: (email: string) => sendVerification(email) });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Profile</CardTitle>
      </CardHeader>
      <CardContent>
        {prof.isPending && <LoadingState />}
        {prof.isError && <QueryState error={prof.error} onRetry={() => prof.refetch()} />}
        {prof.data && (
          <div className="flex flex-col gap-3 text-sm">
            <dl className="grid grid-cols-2 gap-1">
              <dt className="text-muted-foreground">name</dt>
              <dd data-testid="profile-name">{prof.data.name}</dd>
              <dt className="text-muted-foreground">email</dt>
              <dd>{prof.data.email}</dd>
              <dt className="text-muted-foreground">email verified</dt>
              <dd data-testid="email-verified">{prof.data.email_verified ? "yes" : "not yet"}</dd>
            </dl>
            {!prof.data.email_verified && (
              <div className="flex items-center gap-3">
                <Button variant="outline" size="sm" onClick={() => resend.mutate(prof.data.email)} disabled={resend.isPending}>
                  Send verification email
                </Button>
                <FormNotice message={resend.isSuccess ? "Sent. Check your inbox." : null} />
                <FormError message={resend.isError ? asApiError(resend.error).message : null} />
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function SessionInfoCard() {
  const me = useMe();
  return (
    <Card>
      <CardHeader>
        <CardTitle>This session</CardTitle>
        <CardDescription>Signing out ends it; refresh tokens rotate and a reused one is rejected.</CardDescription>
      </CardHeader>
      <CardContent>
        {me.isPending && <LoadingState />}
        {me.isError && <QueryState error={me.error} onRetry={() => me.refetch()} />}
        {me.data && (
          <dl className="grid grid-cols-2 gap-1 text-sm">
            <dt className="text-muted-foreground">signed in at</dt>
            <dd>{new Date(me.data.authenticatedAt).toLocaleString()}</dd>
            <dt className="text-muted-foreground">expires</dt>
            <dd>{me.data.expiresAt ? new Date(me.data.expiresAt).toLocaleString() : "never"}</dd>
            <dt className="text-muted-foreground">methods</dt>
            <dd>{me.data.methods.join(", ")}</dd>
          </dl>
        )}
      </CardContent>
    </Card>
  );
}

function ChangePasswordCard() {
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const change = useMutation({
    mutationFn: () => changePassword(oldPassword, newPassword),
    onSuccess: () => {
      setOldPassword("");
      setNewPassword("");
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Change password</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className="flex max-w-sm flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            change.mutate();
          }}
        >
          <Field id="old-password" label="Current password" type="password" autoComplete="current-password" required value={oldPassword} onChange={(e) => setOldPassword(e.target.value)} />
          <Field id="new-password" label="New password" type="password" autoComplete="new-password" required minLength={8} value={newPassword} onChange={(e) => setNewPassword(e.target.value)} hint="At least 8 characters." />
          <FormError message={change.isError ? asApiError(change.error).message : null} />
          <FormNotice message={change.isSuccess ? "Password changed." : null} />
          <div>
            <Button type="submit" disabled={change.isPending}>
              Change password
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
