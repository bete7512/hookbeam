"use client";

import { useQuery } from "@tanstack/react-query";

import { api } from "./client";
import { myWorkspaces, profile, providers, workspaceInvitations } from "./auth";
import { ApiError, asApiError, errorFromBody, errorFromResponse } from "./errors";

export const keys = {
  overview: (environmentId: number) => ["overview", environmentId] as const,
  applications: (environmentId: number) => ["applications", environmentId] as const,
  application: (id: number) => ["application", id] as const,
  endpoints: (applicationId: number) => ["endpoints", applicationId] as const,
  endpoint: (id: number) => ["endpoint", id] as const,
  apiKeys: ["api-keys"] as const,
  message: (id: number) => ["message", id] as const,
  members: ["members"] as const,
  invitations: (orgId: string) => ["invitations", orgId] as const,
  notifications: ["notifications"] as const,
  notificationPreferences: ["notification-preferences"] as const,
  limits: ["limits"] as const,
  attempts: (deliveryId: number) => ["attempts", deliveryId] as const,
  me: ["me"] as const,
  profile: ["profile"] as const,
  workspaces: ["workspaces"] as const,
  environments: ["environments"] as const,
  providers: ["providers"] as const,
  readyz: ["readyz"] as const,
};

// useMe reads the caller as the control plane sees it; null means no session.
export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: async () => {
      const { data, response } = await api.GET("/v1/me");
      if (response.status === 401) return null;
      if (!data) throw await errorFromResponse(response);
      return data;
    },
    retry: false,
  });
}

export function useProfile(enabled = true) {
  return useQuery({ queryKey: keys.profile, queryFn: () => profile(), enabled, retry: false });
}

export function useWorkspaces(enabled = true) {
  return useQuery({
    queryKey: keys.workspaces,
    queryFn: () => myWorkspaces(),
    enabled,
    retry: false,
    // goauth creates the signup's workspace a moment after the account; poll until it shows up.
    refetchInterval: (query) => ((query.state.data?.length ?? 0) === 0 ? 2_000 : false),
  });
}

export function useEnvironments(enabled = true) {
  return useQuery({
    queryKey: keys.environments,
    queryFn: async () => {
      const { data, response } = await api.GET("/v1/environments");
      if (!data) throw await errorFromResponse(response);
      return data.items;
    },
    enabled,
    retry: false,
  });
}

export function useProviders() {
  return useQuery({ queryKey: keys.providers, queryFn: () => providers(), retry: false, staleTime: 5 * 60_000 });
}

export function useReadiness() {
  return useQuery({
    queryKey: keys.readyz,
    queryFn: async () => {
      const { data, response } = await api.GET("/readyz");
      if (!data) throw await errorFromResponse(response);
      return data;
    },
    retry: false,
  });
}

// errorOf reads a query's error as an ApiError, for the shared state components.
export function errorOf(error: unknown): ApiError {
  return asApiError(error);
}

// unwrap awaits an openapi-fetch call and returns its data, or throws the ApiError the response carried. openapi-fetch
// has already read the error body into `error`, so it is mapped from there; the response is only read when it did not.
export async function unwrap<T>(call: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const result = await call;
  if (result.data === undefined) {
    if (result.response.ok) return undefined as T; // a 204
    if (result.error !== undefined) throw errorFromBody(result.response.status, result.response.statusText, result.error);
    throw await errorFromResponse(result.response);
  }
  return result.data;
}

export function useOverview(environmentId: number | null) {
  return useQuery({
    queryKey: keys.overview(environmentId ?? 0),
    queryFn: () => unwrap(api.GET("/v1/environments/{environmentId}/overview", { params: { path: { environmentId: environmentId ?? 0 } } })),
    enabled: environmentId !== null,
    refetchInterval: 15_000,
  });
}

export function useApplications(environmentId: number | null) {
  return useQuery({
    queryKey: keys.applications(environmentId ?? 0),
    queryFn: async () => (await unwrap(api.GET("/v1/environments/{environmentId}/applications", { params: { path: { environmentId: environmentId ?? 0 } } }))).items,
    enabled: environmentId !== null,
  });
}

export function useApplication(id: number) {
  return useQuery({ queryKey: keys.application(id), queryFn: () => unwrap(api.GET("/v1/applications/{applicationId}", { params: { path: { applicationId: id } } })) });
}

export function useEndpoints(applicationId: number) {
  return useQuery({
    queryKey: keys.endpoints(applicationId),
    queryFn: async () => (await unwrap(api.GET("/v1/applications/{applicationId}/endpoints", { params: { path: { applicationId } } }))).items,
  });
}

export function useEndpoint(id: number) {
  return useQuery({ queryKey: keys.endpoint(id), queryFn: () => unwrap(api.GET("/v1/endpoints/{endpointId}", { params: { path: { endpointId: id } } })) });
}

export function useApiKeys() {
  return useQuery({ queryKey: keys.apiKeys, queryFn: async () => (await unwrap(api.GET("/v1/api-keys", { params: { query: { limit: 100 } } }))).items });
}

export function useMessage(id: number | null, poll = false) {
  return useQuery({
    queryKey: keys.message(id ?? 0),
    queryFn: () => unwrap(api.GET("/v1/messages/{messageId}", { params: { path: { messageId: id ?? 0 } } })),
    enabled: id !== null,
    refetchInterval: poll ? 2_000 : false,
  });
}

export function useAttempts(deliveryId: number | null, poll = false) {
  return useQuery({
    queryKey: keys.attempts(deliveryId ?? 0),
    queryFn: async () => (await unwrap(api.GET("/v1/deliveries/{deliveryId}/attempts", { params: { path: { deliveryId: deliveryId ?? 0 } } }))).items,
    enabled: deliveryId !== null,
    refetchInterval: poll ? 2_000 : false,
  });
}

// useMembers lists the workspace's members with the role the API applies and the person behind each (name and
// email come from goauth through the control plane).
export function useMembers(orgId: string | null) {
  return useQuery({ queryKey: keys.members, queryFn: async () => (await unwrap(api.GET("/v1/members"))).items, enabled: orgId !== null });
}

export function useInvitations(orgId: string | null) {
  return useQuery({ queryKey: keys.invitations(orgId ?? ""), queryFn: () => workspaceInvitations(orgId ?? ""), enabled: orgId !== null });
}

export function useNotifications(unreadOnly: boolean, limit = 50) {
  return useQuery({
    queryKey: [...keys.notifications, unreadOnly, limit],
    queryFn: () => unwrap(api.GET("/v1/notifications", { params: { query: { limit, unread: unreadOnly || undefined } } })),
    refetchInterval: 15_000,
  });
}

// useUnreadNotifications is the badge count in the shell; it asks for one row and reads the workspace-wide count.
export function useUnreadNotifications(enabled = true) {
  return useQuery({
    queryKey: [...keys.notifications, "unread-count"],
    queryFn: async () => (await unwrap(api.GET("/v1/notifications", { params: { query: { limit: 1, unread: true } } }))).unread,
    refetchInterval: 30_000,
    enabled,
  });
}

export function useNotificationPreferences() {
  return useQuery({ queryKey: keys.notificationPreferences, queryFn: () => unwrap(api.GET("/v1/me/notification-preferences")) });
}

export function useLimits() {
  return useQuery({ queryKey: keys.limits, queryFn: () => unwrap(api.GET("/v1/workspaces/current/limits")), refetchInterval: 30_000 });
}
