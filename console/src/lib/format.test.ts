import { describe, expect, it } from "vitest";

import { duration } from "./format";

describe("duration", () => {
  it("reads whole days as days, whole hours as hours, the rest as minutes", () => {
    expect(duration(7 * 86400)).toBe("7 days");
    expect(duration(86400)).toBe("1 day");
    expect(duration(36 * 3600)).toBe("36 hours");
    expect(duration(90 * 60)).toBe("90 minutes");
  });
});
