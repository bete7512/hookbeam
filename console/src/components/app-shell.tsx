"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Activity, Bell, BookOpen, Boxes, CircleUser, KeyRound, LayoutDashboard, LogOut, Menu, ScrollText, Settings, Users, X } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { logout, switchWorkspace } from "@/api/auth";
import { useMe, useUnreadNotifications, useWorkspaces } from "@/api/queries";
import { Brand } from "@/components/brand";
import { Button } from "@/components/ui/button";
import { LoadingState, QueryState } from "@/components/states";
import { EnvironmentBadge, EnvironmentProvider, useEnvironment } from "@/lib/environment";
import { cn } from "@/lib/utils";

// AppShell wraps every signed-in page: a sidebar with the workspace and the navigation, a top bar with the
// environment the person is acting in, notifications and the account. It requires a session.
export function AppShell({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const me = useMe();
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (me.data === null) {
      router.replace(`/login?next=${encodeURIComponent(pathname)}`);
    }
  }, [me.data, pathname, router]);

  // The drawer closes on navigation.
  useEffect(() => setOpen(false), [pathname]);

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

  const workspaceId = me.data.workspaceId ?? null;
  return (
    <EnvironmentProvider workspaceId={workspaceId ?? ""}>
      <div className="flex min-h-screen">
        {open && <button aria-label="Close menu" className="fixed inset-0 z-30 bg-foreground/30 lg:hidden" onClick={() => setOpen(false)} />}
        <aside
          className={cn(
            "fixed inset-y-0 left-0 z-40 flex w-64 shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground transition-transform lg:static lg:translate-x-0",
            open ? "translate-x-0" : "-translate-x-full",
          )}
          aria-label="Sidebar"
        >
          <div className="flex h-14 items-center justify-between px-4">
            <Brand href="/overview" />
            <button className="rounded-md p-1 hover:bg-sidebar-accent lg:hidden" aria-label="Close menu" onClick={() => setOpen(false)}>
              <X className="h-4 w-4" aria-hidden />
            </button>
          </div>
          <div className="px-3 pb-3">
            <WorkspaceSelector current={workspaceId} />
          </div>
          <nav aria-label="Primary" className="flex flex-1 flex-col gap-5 overflow-y-auto px-3 py-2">
            <NavGroup label="Deliver">
              <NavItem href="/overview" icon={LayoutDashboard} label="Overview" exact />
              <NavItem href="/applications" icon={Boxes} label="Applications" also={["/endpoints"]} />
              <NavItem href="/events" icon={Activity} label="Events" />
            </NavGroup>
            <NavGroup label="Workspace">
              <NavItem href="/team" icon={Users} label="Team" />
              <NavItem href="/developers" icon={KeyRound} label="Developers" />
              <NavItem href="/audit" icon={ScrollText} label="Audit" />
              <NavItem href="/settings" icon={Settings} label="Settings" />
            </NavGroup>
          </nav>
          <div className="border-t border-sidebar-border p-3">
            <NavItem href="/account" icon={CircleUser} label="Account" />
            <SignOutButton />
          </div>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b bg-background/85 px-4 backdrop-blur sm:px-6">
            <button className="rounded-md p-1.5 hover:bg-secondary lg:hidden" aria-label="Open menu" onClick={() => setOpen(true)}>
              <Menu className="h-5 w-5" aria-hidden />
            </button>
            <p className="truncate text-sm font-medium text-muted-foreground" aria-hidden>
              {sectionName(pathname)}
            </p>
            <div className="ml-auto flex items-center gap-2 sm:gap-3">
              <EnvironmentSwitcher />
              <a href="/docs" target="_blank" rel="noreferrer" className="hidden items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-secondary hover:text-foreground sm:inline-flex">
                <BookOpen className="h-4 w-4" aria-hidden />
                Docs
              </a>
              <NotificationsLink enabled={!!workspaceId} />
            </div>
          </header>
          <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 sm:px-6 lg:px-8">{children}</main>
        </div>
      </div>
    </EnvironmentProvider>
  );
}

const sections: [string, string][] = [
  ["/overview", "Overview"],
  ["/applications", "Applications"],
  ["/endpoints", "Endpoints"],
  ["/events", "Events"],
  ["/team", "Team"],
  ["/developers", "Developers"],
  ["/notifications", "Notifications"],
  ["/audit", "Audit"],
  ["/settings", "Settings"],
  ["/account", "Account"],
];

function sectionName(pathname: string) {
  return sections.find(([prefix]) => pathname === prefix || pathname.startsWith(`${prefix}/`))?.[1] ?? "";
}

function NavGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="px-2.5 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">{label}</p>
      <ul className="flex flex-col gap-0.5">{children}</ul>
    </div>
  );
}

function NavItem({ href, icon: Icon, label, exact = false, also = [], badge }: { href: string; icon: React.ComponentType<{ className?: string }>; label: string; exact?: boolean; also?: string[]; badge?: React.ReactNode }) {
  const pathname = usePathname();
  const matches = (prefix: string) => (exact ? pathname === prefix : pathname === prefix || pathname.startsWith(`${prefix}/`));
  const active = matches(href) || also.some(matches);
  return (
    <li>
      <Link
        href={href}
        aria-current={active ? "page" : undefined}
        className={cn(
          "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm font-medium transition-colors",
          active ? "bg-sidebar-accent text-sidebar-foreground" : "text-sidebar-foreground/75 hover:bg-sidebar-accent hover:text-sidebar-foreground",
        )}
      >
        <Icon className={cn("h-4 w-4 shrink-0", active ? "text-primary" : "")} aria-hidden />
        <span className="truncate">{label}</span>
        {badge}
      </Link>
    </li>
  );
}

// NotificationsLink carries the unread count, so a failing endpoint is visible from every page.
function NotificationsLink({ enabled }: { enabled: boolean }) {
  const unread = useUnreadNotifications(enabled);
  const count = unread.data ?? 0;
  return (
    <Link href="/notifications" className="relative inline-flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-muted-foreground hover:bg-secondary hover:text-foreground" data-testid="notifications-link" aria-label={`Notifications${count ? `, ${count} unread` : ""}`}>
      <Bell className="h-4 w-4" aria-hidden />
      <span className="hidden sm:inline">Notifications{count ? ` (${count})` : ""}</span>
      {count > 0 && <span className="absolute -right-0.5 -top-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold text-primary-foreground sm:hidden">{count}</span>}
    </Link>
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
      <span className="block rounded-md border border-dashed border-sidebar-border px-2.5 py-2 text-sm text-muted-foreground" data-testid="workspace-none">
        No workspace yet
      </span>
    );
  }
  return (
    <label className="block">
      <span className="sr-only">Workspace</span>
      <select
        className="h-9 w-full rounded-md border border-sidebar-border bg-card px-2.5 text-sm font-medium shadow-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        value={current ?? ""}
        onChange={(e) => switching.mutate(e.target.value)}
        disabled={switching.isPending}
        data-testid="workspace-selector"
      >
        {current === null && <option value="">Select a workspace…</option>}
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
          <select className="h-8 px-2" value={current?.id ?? ""} onChange={(e) => select(Number(e.target.value))} data-testid="environment-selector">
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
    <Button variant="ghost" className="mt-0.5 w-full justify-start gap-2.5 px-2.5 text-sidebar-foreground/75 hover:text-sidebar-foreground" onClick={() => signOut.mutate()} disabled={signOut.isPending}>
      <LogOut className="h-4 w-4" aria-hidden />
      Sign out
    </Button>
  );
}
