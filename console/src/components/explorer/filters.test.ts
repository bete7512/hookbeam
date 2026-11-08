import { describe, expect, it } from "vitest";

import { defaultFilters, parseFilters, serializeFilters, toQuery } from "./filters";

describe("explorer filters", () => {
  it("round-trips through the URL and drops defaults", () => {
    const f = { ...defaultFilters, type: "order.paid", outcome: "failed" as const, offset: 40, sortBy: "id" as const };
    const url = serializeFilters(f);
    expect(url).toBe("?type=order.paid&outcome=failed&sort_by=id&offset=40");
    expect(parseFilters(new URLSearchParams(url))).toEqual(f);
    expect(serializeFilters(defaultFilters)).toBe("");
  });
  it("ignores values the API would refuse", () => {
    const f = parseFilters(new URLSearchParams("?outcome=lost&limit=500&offset=-3&sort_dir=sideways"));
    expect(f.outcome).toBe("");
    expect(f.limit).toBe(20);
    expect(f.offset).toBe(0);
    expect(f.sortDir).toBe("desc");
  });
  it("maps onto the list API's parameters", () => {
    const q = toQuery({ ...defaultFilters, externalId: "order-817", acceptedAfter: "2026-11-08T00:00" });
    expect(q.external_id).toBe("order-817");
    expect(q.accepted_after).toMatch(/^2026-11-0[78]T/);
    expect(q.type).toBeUndefined();
    expect(q.sort_dir).toBe("desc");
  });
});
