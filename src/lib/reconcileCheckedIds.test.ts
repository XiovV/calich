import { describe, expect, it } from "vitest";
import { reconcileCheckedIds } from "./reconcileCheckedIds";

describe("reconcileCheckedIds", () => {
  it("checks an id it has never seen before", () => {
    const result = reconcileCheckedIds(new Set(), new Set(), ["a"]);
    expect(result.checkedIds).toEqual(new Set(["a"]));
    expect(result.knownIds).toEqual(new Set(["a"]));
  });

  it("leaves a previously-seen, deliberately-unchecked id unchecked", () => {
    const result = reconcileCheckedIds(new Set(["a"]), new Set(), ["a"]);
    expect(result.checkedIds).toEqual(new Set());
  });

  it("leaves an already-checked id checked and adds a newly-seen one alongside it", () => {
    const result = reconcileCheckedIds(new Set(["a"]), new Set(["a"]), ["a", "b"]);
    expect(result.checkedIds).toEqual(new Set(["a", "b"]));
  });

  it("replaces knownIds with exactly the fresh snapshot, dropping ids no longer present", () => {
    const result = reconcileCheckedIds(new Set(["a", "b"]), new Set(["a", "b"]), ["a"]);
    expect(result.knownIds).toEqual(new Set(["a"]));
  });

  it("works over numeric ids the same as string ids", () => {
    const result = reconcileCheckedIds(new Set([1]), new Set(), [1, 2]);
    expect(result.checkedIds).toEqual(new Set([2]));
    expect(result.knownIds).toEqual(new Set([1, 2]));
  });
});
