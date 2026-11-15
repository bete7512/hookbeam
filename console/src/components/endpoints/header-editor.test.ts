import { describe, expect, it } from "vitest";

import { headersForCreate, headersPatch, noHeaderChanges } from "./header-editor";

describe("custom header changes", () => {
  it("sends nothing when nothing changed", () => {
    expect(headersForCreate(noHeaderChanges)).toBeUndefined();
    expect(headersPatch(noHeaderChanges)).toBeUndefined();
    expect(headersPatch({ added: [{ name: "  ", value: "" }], removed: [] })).toBeUndefined();
  });

  it("creates with lower-cased names and trimmed values", () => {
    expect(headersForCreate({ added: [{ name: " X-Api-Key ", value: " s3cret " }], removed: [] })).toEqual({ "x-api-key": "s3cret" });
  });

  it("patches as a merge: a value sets, null removes, and a header added again wins over its removal", () => {
    expect(headersPatch({ added: [{ name: "x-new", value: "1" }], removed: ["x-old"] })).toEqual({ "x-new": "1", "x-old": null });
    expect(headersPatch({ added: [{ name: "authorization", value: "Bearer b" }], removed: ["authorization"] })).toEqual({ authorization: "Bearer b" });
  });
});
