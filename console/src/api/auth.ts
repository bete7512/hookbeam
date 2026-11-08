// goauth's routes are mounted at /v1/auth and documented in the merged /openapi.json, not in the control plane's
// bundle the client is generated from, so the calls the console needs are typed here by hand.
import { ApiError, errorFromResponse, networkError } from "./errors";

export interface LoginInput {
  email: string;
  password: string;
}

export interface SignupInput {
  name: string;
  email: string;
  password: string;
}

export interface TokenPair {
  access_token?: string;
  refresh_token?: string;
}

export interface Profile {
  id: string;
  name: string;
  email: string;
  email_verified: boolean;
  created_at?: string;
}

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  owner_id: string;
  active: boolean;
}

export interface Provider {
  name: string;
  enabled: boolean;
}

interface Envelope<T> {
  data: T;
}

// goauthFetch calls a goauth route on the same origin, unwraps its {data} envelope and turns failures into ApiError.
async function goauthFetch<T>(path: string, init: RequestInit = {}, fetcher: typeof fetch = globalThis.fetch): Promise<T> {
  let res: Response;
  try {
    res = await fetcher(`/v1/auth${path}`, { credentials: "include", ...init });
  } catch {
    throw networkError();
  }
  if (!res.ok) {
    throw await errorFromResponse(res);
  }
  if (res.status === 204) return undefined as T;
  const body = (await res.json()) as Envelope<T>;
  return body.data;
}

function json(body: unknown): RequestInit {
  return { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) };
}

export function login(input: LoginInput, fetcher?: typeof fetch) {
  return goauthFetch<TokenPair>("/login", json(input), fetcher).catch((err: ApiError) => {
    throw err.unauthorized ? new ApiError(401, err.code, "Wrong email or password.") : err;
  });
}

export function signup(input: SignupInput, fetcher?: typeof fetch) {
  return goauthFetch<{ user: Profile }>("/signup", json(input), fetcher);
}

export function logout(fetcher?: typeof fetch) {
  return goauthFetch<unknown>("/logout", { method: "POST" }, fetcher);
}

export function profile(fetcher?: typeof fetch) {
  return goauthFetch<Profile>("/me", {}, fetcher);
}

export function changePassword(oldPassword: string, newPassword: string, fetcher?: typeof fetch) {
  return goauthFetch<unknown>("/change-password", { ...json({ old_password: oldPassword, new_password: newPassword }), method: "PUT" }, fetcher);
}

export function forgotPassword(email: string, fetcher?: typeof fetch) {
  return goauthFetch<unknown>("/forgot-password", json({ email }), fetcher);
}

export function resetPassword(token: string, newPassword: string, fetcher?: typeof fetch) {
  return goauthFetch<unknown>("/reset-password", json({ token, new_password: newPassword }), fetcher);
}

// sendVerification asks goauth to email a verification link; signup itself only sends the welcome email.
export function sendVerification(email: string, fetcher?: typeof fetch) {
  return goauthFetch<unknown>("/send-verification-email", json({ email }), fetcher);
}

export async function providers(fetcher?: typeof fetch): Promise<Provider[]> {
  const res = await goauthFetch<{ providers: Provider[] }>("/oauth/providers", {}, fetcher);
  return (res.providers ?? []).filter((p) => p.enabled);
}

export function myWorkspaces(fetcher?: typeof fetch) {
  return goauthFetch<Workspace[]>("/org/my", {}, fetcher);
}

export function switchWorkspace(orgId: string, fetcher?: typeof fetch) {
  return goauthFetch<TokenPair>("/org/switch", json({ org_id: orgId }), fetcher);
}

// oauthStartUrl is where the browser goes to sign in with a provider; goauth redirects back to /auth/callback.
export function oauthStartUrl(provider: string) {
  return `/v1/auth/oauth/${encodeURIComponent(provider)}`;
}
