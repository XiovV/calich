import { describe, expect, it } from "vitest";
import {
  priorityLevelFromValue,
  priorityRank,
  priorityValueForLevel,
} from "./taskPriority";

describe("priorityLevelFromValue", () => {
  it("maps 0 to none", () => {
    expect(priorityLevelFromValue(0)).toBe("none");
  });

  it("maps 1-4 to high", () => {
    expect(priorityLevelFromValue(1)).toBe("high");
    expect(priorityLevelFromValue(4)).toBe("high");
  });

  it("maps 5 to medium", () => {
    expect(priorityLevelFromValue(5)).toBe("medium");
  });

  it("maps 6-9 to low", () => {
    expect(priorityLevelFromValue(6)).toBe("low");
    expect(priorityLevelFromValue(9)).toBe("low");
  });
});

describe("priorityValueForLevel", () => {
  it("round-trips through priorityLevelFromValue for every level", () => {
    for (const level of ["none", "low", "medium", "high"] as const) {
      expect(priorityLevelFromValue(priorityValueForLevel(level))).toBe(level);
    }
  });
});

describe("priorityRank", () => {
  it("ranks None lowest and High highest, the opposite of the raw value's own ordering", () => {
    expect(priorityRank(0)).toBeLessThan(priorityRank(9)); // None < Low
    expect(priorityRank(9)).toBeLessThan(priorityRank(5)); // Low < Medium
    expect(priorityRank(5)).toBeLessThan(priorityRank(1)); // Medium < High
  });
});
