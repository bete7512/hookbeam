"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";

import { logout, switchWorkspace } from "@/api/auth";
import { useMe, useWorkspaces } from "@/api/queries";
import { Button } from "@/components/ui/button";
import { LoadingState, QueryState } from "@/components/states";
import { EnvironmentBadge, EnvironmentProvider, useEnvironment } from "@/lib/environment";

// AppShell wraps every signed-in page: it requires a session, shows the workspace and environment the person is
// acting in, and offers the account page and sign-out.
export function AppShell({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const me = useMe();

  useEffect(() => {
    if (me.data === null) {
      router.replace(`/login?next=${encodeURIComponent(pathname)}`);
    }
  }, [me.data, pathname, router]);

  if (me.isPending) {
    return (
      <main className="p-8">
        <LoadingState label="Checking your session…" />
      </main>
    );
  }
  if (me.isError) {
    return (
      <main className="p-8">
        <QueryState error={me.error} onRetry={() => me.refetch()} />
      </main>
    );
  }
  if (!me.data) return null;

  return (
    <EnvironmentProvider workspaceId={me.data.workspaceId ?? ""}>
      <div className="min-h-screen">
        <header className="border-b">
          <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-4 px-6 py-3">
            <Link href="/" className="font-semibold">
              Hookbeam
            </Link>
            <WorkspaceSelector current={me.data.workspaceId ?? null} />
            <EnvironmentSwitcher />
            <nav aria-label="Primary" className="ml-auto flex items-center gap-2 text-sm">
              <Link href="/" className="rounded px-2 py-1 hover:bg-secondary">
                Overview
              </Link>
              <Link href="/applications" className="rounded px-2 py-1 hover:bg-secondary">
                Applications
              </Link>
              <Link href="/events" className="rounded px-2 py-1 hover:bg-secondary">
                Events
              </Link>
              <Link href="/account" className="rounded px-2 py-1 hover:bg-secondary">
                Account
              </Link>
              <SignOutButton />
            </nav>
          </div>
        </header>
        <main className="mx-auto max-w-5xl px-6 py-8">{children}</main>
      </div>
    </EnvironmentProvider>
  );
}

function WorkspaceSelector({ current }: { current: string | null }) {
  const queryClient = useQueryClient();
  const workspaces = useWorkspaces();
  const switching = useMutation({
    mutationFn: (id: string) => switchWorkspace(id),
    onSuccess: () => queryClient.invalidateQueries(),
  });
  const only = workspaces.data?.length === 1 ? workspaces.data[0].id : null;
  // A person with exactly one workspace and none selected is switched into it without asking.
  useEffect(() => {
    if (current === null && only !== null && !switching.isPending && !switching.isError) {
      switching.mutate(only);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current, only]);
  if (workspaces.isPending) return <LoadingState label="Loading workspaces…" />;
  if (workspaces.isError) return <span className="text-sm text-destructive">Workspaces unavailable</span>;
  const list = workspaces.data ?? [];
  if (list.length === 0) {
    return (
      <span className="text-sm text-muted-foreground" data-testid="workspace-none">
        No workspace yet
      </span>
    );
  }
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Workspace</span>
      <select
        className="h-8 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        value={current ?? ""}
        onChange={(e) => switching.mutate(e.target.value)}
        disabled={switching.isPending}
        data-testid="workspace-selector"
      >
        {current === null && <option value="">Select…</option>}
        {list.map((w) => (
          <option key={w.id} value={w.id}>
            {w.name}
          </option>
        ))}
      </select>
    </label>
  );
}

function EnvironmentSwitcher() {
  const { environments, current, select, loading } = useEnvironment();
  if (loading) return <LoadingState label="Loading environments…" />;
  return (
    <div className="flex items-center gap-2">
      <EnvironmentBadge environment={current} />
      {environments.length > 1 && (
        <label className="flex items-center gap-2 text-sm">
          <span className="sr-only">Environment</span>
          <select
            className="h-8 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            value={current?.id ?? ""}
            onChange={(e) => select(Number(e.target.value))}
            data-testid="environment-selector"
          >
            {environments.map((env) => (
              <option key={env.id} value={env.id}>
                {env.name} ({env.kind})
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  );
}

function SignOutButton() {
  const queryClient = useQueryClient();
  const signOut = useMutation({
    mutationFn: () => logout(),
    onSettled: () => {
      queryClient.clear();
      // A full navigation, so nothing of the signed-in session survives in the client router.
      window.location.assign("/login");
    },
  });
  return (
    <Button variant="outline" size="sm" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
      Sign out
    </Button>
  );
}
