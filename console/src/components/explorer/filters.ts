// The explorer's filters live in the URL, so a reload or a copied link reproduces the same list.
export type Outcome = "queued" | "in_flight" | "retry_scheduled" | "succeeded" | "failed" | "cancelled";
export type SortBy = "id" | "acceptedAt";
export type SortDir = "asc" | "desc";

export interface Filters {
  type: string;
  externalId: string;
  acceptedAfter: string;
  acceptedBefore: string;
  outcome: Outcome | "";
  sortBy: SortBy;
  sortDir: SortDir;
  limit: number;
  offset: number;
}

export const defaultFilters: Filters = { type: "", externalId: "", acceptedAfter: "", acceptedBefore: "", outcome: "", sortBy: "acceptedAt", sortDir: "desc", limit: 20, offset: 0 };

const outcomes: Outcome[] = ["queued", "in_flight", "retry_scheduled", "succeeded", "failed", "cancelled"];

export function parseFilters(params: URLSearchParams): Filters {
  const outcome = params.get("outcome") ?? "";
  const sortBy = params.get("sort_by");
  const sortDir = params.get("sort_dir");
  const limit = Number(params.get("limit"));
  const offset = Number(params.get("offset"));
  return {
    type: params.get("type") ?? "",
    externalId: params.get("external_id") ?? "",
    acceptedAfter: params.get("accepted_after") ?? "",
    acceptedBefore: params.get("accepted_before") ?? "",
    outcome: outcomes.includes(outcome as Outcome) ? (outcome as Outcome) : "",
    sortBy: sortBy === "id" ? "id" : "acceptedAt",
    sortDir: sortDir === "asc" ? "asc" : "desc",
    limit: Number.isInteger(limit) && limit >= 1 && limit <= 100 ? limit : 20,
    offset: Number.isInteger(offset) && offset >= 0 ? offset : 0,
  };
}

// serializeFilters writes only what differs from the defaults, so the URL stays short and stable.
export function serializeFilters(f: Filters): string {
  const params = new URLSearchParams();
  if (f.type) params.set("type", f.type);
  if (f.externalId) params.set("external_id", f.externalId);
  if (f.acceptedAfter) params.set("accepted_after", f.acceptedAfter);
  if (f.acceptedBefore) params.set("accepted_before", f.acceptedBefore);
  if (f.outcome) params.set("outcome", f.outcome);
  if (f.sortBy !== defaultFilters.sortBy) params.set("sort_by", f.sortBy);
  if (f.sortDir !== defaultFilters.sortDir) params.set("sort_dir", f.sortDir);
  if (f.limit !== defaultFilters.limit) params.set("limit", String(f.limit));
  if (f.offset) params.set("offset", String(f.offset));
  const q = params.toString();
  return q ? `?${q}` : "";
}

// toQuery maps the filters onto the list API's query parameters.
export function toQuery(f: Filters) {
  return {
    limit: f.limit,
    offset: f.offset,
    type: f.type || undefined,
    external_id: f.externalId || undefined,
    accepted_after: f.acceptedAfter ? new Date(f.acceptedAfter).toISOString() : undefined,
    accepted_before: f.acceptedBefore ? new Date(f.acceptedBefore).toISOString() : undefined,
    outcome: f.outcome || undefined,
    sort_by: f.sortBy,
    sort_dir: f.sortDir,
  };
}
