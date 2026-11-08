"use client";

import { useQuery } from "@tanstack/react-query";

import { api } from "./client";
import { myWorkspaces, profile, providers } from "./auth";
import { ApiError, asApiError, errorFromResponse } from "./errors";

export const keys = {
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
  return useQuery({ queryKey: keys.workspaces, queryFn: () => myWorkspaces(), enabled, retry: false });
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
