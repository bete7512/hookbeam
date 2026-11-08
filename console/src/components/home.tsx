"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { api } from "@/api/client";
import { login, logout } from "@/api/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Home is the placeholder page of the scaffold: it proves the one-origin setup by reading the API's readiness
// and the caller's identity through the session cookie, and lets a developer sign in to get that cookie.
export function Home() {
  return (
    <main className="mx-auto flex max-w-3xl flex-col gap-6 p-8">
      <header>
        <h1 className="text-2xl font-semibold">Hookbeam</h1>
        <p className="text-muted-foreground">Webhook delivery you can see.</p>
      </header>
      <ReadinessCard />
      <SessionCard />
    </main>
  );
}

function ReadinessCard() {
  const ready = useQuery({
    queryKey: ["readyz"],
    queryFn: async () => {
      const { data, response } = await api.GET("/readyz");
      if (!data) throw new Error(`readiness answered ${response.status}`);
      return data;
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>API</CardTitle>
        <CardDescription>What the control plane reports through the shared origin.</CardDescription>
      </CardHeader>
      <CardContent>
        {ready.isPending && <p role="status">Checking…</p>}
        {ready.isError && (
          <p role="alert" className="text-destructive">
            The API is not reachable right now.
          </p>
        )}
        {ready.data && (
          <dl className="grid grid-cols-2 gap-1 text-sm">
            <dt className="text-muted-foreground">status</dt>
            <dd>{ready.data.status}</dd>
            {Object.entries(ready.data.checks ?? {}).map(([name, state]) => (
              <Fragment2 key={name} name={name} state={String(state)} />
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  );
}

function Fragment2({ name, state }: { name: string; state: string }) {
  return (
    <>
      <dt className="text-muted-foreground">{name}</dt>
      <dd>{state}</dd>
    </>
  );
}

function SessionCard() {
  const queryClient = useQueryClient();
  const me = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const { data, response } = await api.GET("/v1/me");
      if (response.status === 401) return null;
      if (!data) throw new Error(`identity answered ${response.status}`);
      return data;
    },
  });
  const signOut = useMutation({
    mutationFn: () => logout(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["me"] }),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Session</CardTitle>
        <CardDescription>The cookie goauth set at login, read back through the same origin.</CardDescription>
      </CardHeader>
      <CardContent>
        {me.isPending && <p role="status">Loading…</p>}
        {me.isError && (
          <p role="alert" className="text-destructive">
            Could not read the session.
          </p>
        )}
        {me.data === null && <LoginForm onSignedIn={() => queryClient.invalidateQueries({ queryKey: ["me"] })} />}
        {me.data && (
          <div className="flex flex-col gap-3 text-sm">
            <dl className="grid grid-cols-2 gap-1">
              <dt className="text-muted-foreground">user</dt>
              <dd data-testid="user-id">{me.data.userId ?? "api key"}</dd>
              <dt className="text-muted-foreground">workspace</dt>
              <dd>{me.data.workspaceId ?? "none selected"}</dd>
              <dt className="text-muted-foreground">methods</dt>
              <dd>{me.data.methods.join(", ")}</dd>
            </dl>
            <div>
              <Button variant="outline" size="sm" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
                Sign out
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function LoginForm({ onSignedIn }: { onSignedIn: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const signIn = useMutation({
    mutationFn: () => login({ email, password }),
    onSuccess: onSignedIn,
  });
  return (
    <form
      className="flex max-w-sm flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        signIn.mutate();
      }}
    >
      <div className="flex flex-col gap-1">
        <Label htmlFor="email">Email</Label>
        <Input id="email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="password">Password</Label>
        <Input id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
      </div>
      {signIn.isError && (
        <p role="alert" className="text-sm text-destructive">
          {signIn.error.message}
        </p>
      )}
      <div>
        <Button type="submit" disabled={signIn.isPending}>
          Sign in
        </Button>
      </div>
    </form>
  );
}
