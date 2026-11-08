// ApiError is what every call into the control plane or goauth throws when the response is not 2xx. The
// control plane answers {error, message, code, fields}; goauth answers {data: {code, message, status_code}}.
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: Record<string, string>;

  constructor(status: number, code: string, message: string, fields: Record<string, string> = {}) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.fields = fields;
  }

  get unauthorized() {
    return this.status === 401;
  }
  get forbidden() {
    return this.status === 403;
  }
  get notFound() {
    return this.status === 404;
  }
  get unavailable() {
    return this.status === 503 || this.status === 0;
  }
}

interface ControlPlaneError {
  error?: string;
  message?: string;
  fields?: Record<string, string>;
}

interface GoauthError {
  data?: { code?: string; message?: string };
}

// errorFromResponse reads either envelope from a failed response.
export async function errorFromResponse(res: Response): Promise<ApiError> {
  let body: unknown = null;
  try {
    body = await res.json();
  } catch {
    body = null;
  }
  return errorFromBody(res.status, res.statusText, body);
}

// errorFromBody builds the ApiError from an already parsed body, for clients that consumed the response themselves.
export function errorFromBody(status: number, statusText: string, body: unknown): ApiError {
  const cp = (body ?? {}) as ControlPlaneError;
  if (typeof cp.error === "string") {
    return new ApiError(status, cp.error, cp.message ?? statusText, cp.fields ?? {});
  }
  const ga = (body ?? {}) as GoauthError;
  if (ga.data && typeof ga.data === "object" && (ga.data.code || ga.data.message)) {
    return new ApiError(status, ga.data.code ?? "ERROR", ga.data.message ?? statusText);
  }
  return new ApiError(status, status === 503 ? "SERVICE_UNAVAILABLE" : "ERROR", statusText || "Request failed");
}

// networkError is what a fetch that never reached the server becomes.
export function networkError(): ApiError {
  return new ApiError(0, "NETWORK", "The server did not answer.");
}

// asApiError normalises anything thrown in a query into an ApiError.
export function asApiError(err: unknown): ApiError {
  if (err instanceof ApiError) return err;
  if (err instanceof TypeError) return networkError();
  return new ApiError(500, "ERROR", err instanceof Error ? err.message : "Something went wrong.");
}
