import { describe, expect, it } from "vitest";

import { duration, until } from "./format";

describe("duration", () => {
  it("reads whole days as days, whole hours as hours, the rest as minutes", () => {
    expect(duration(7 * 86400)).toBe("7 days");
    expect(duration(86400)).toBe("1 day");
    expect(duration(36 * 3600)).toBe("36 hours");
    expect(duration(90 * 60)).toBe("90 minutes");
  });
});

describe("until", () => {
  it("counts down to a future moment and says now once it has passed", () => {
    const now = Date.parse("2026-11-08T00:00:00Z");
    expect(until("2026-11-08T00:00:12Z", now)).toBe("in 12s");
    expect(until("2026-11-08T00:03:00Z", now)).toBe("in 3m");
    expect(until("2026-11-08T05:00:00Z", now)).toBe("in 5h");
    expect(until("2026-11-07T23:59:00Z", now)).toBe("now");
    expect(until(null, now)).toBe("never");
  });
});
