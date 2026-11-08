// goauth's routes are mounted at /v1/auth and documented in the merged /openapi.json, not in the control plane's
// bundle the client is generated from, so the few calls the console needs are typed here by hand.

export interface LoginInput {
  email: string;
  password: string;
}

export interface LoginResult {
  access_token: string;
  refresh_token: string;
}

interface Envelope<T> {
  data: T;
}

export async function login(input: LoginInput, fetcher: typeof fetch = fetch): Promise<LoginResult> {
  const res = await fetcher("/v1/auth/login", {
    method: "POST",
    headers: { "content-type": "application/json" },
    credentials: "include",
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(res.status === 401 ? "Wrong email or password." : `Login failed (${res.status}).`);
  }
  const body = (await res.json()) as Envelope<LoginResult>;
  return body.data;
}

export async function logout(fetcher: typeof fetch = fetch): Promise<void> {
  await fetcher("/v1/auth/logout", { method: "POST", credentials: "include" });
}
