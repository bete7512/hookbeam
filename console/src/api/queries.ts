"use client";

import { useQuery } from "@tanstack/react-query";

import { api } from "./client";
import { myWorkspaces, profile, providers } from "./auth";
import { ApiError, asApiError, errorFromResponse } from "./errors";

export const keys = {
  overview: (environmentId: number) => ["overview", environmentId] as const,
  applications: (environmentId: number) => ["applications", environmentId] as const,
  application: (id: number) => ["application", id] as const,
  endpoints: (applicationId: number) => ["endpoints", applicationId] as const,
  endpoint: (id: number) => ["endpoint", id] as const,
  apiKeys: ["api-keys"] as const,
  message: (id: number) => ["message", id] as const,
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

// unwrap awaits an openapi-fetch call and returns its data, or throws the ApiError the response carried.
export async function unwrap<T>(call: Promise<{ data?: T; response: Response }>): Promise<T> {
  const result = await call;
  if (result.data === undefined) throw await errorFromResponse(result.response);
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
