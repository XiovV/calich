import { describe, expect, it } from "vitest";
import { useShellStore } from "./shellStore";

describe("useShellStore", () => {
  it("defaults activeView to week", () => {
    expect(useShellStore.getState().activeView).toBe("week");
  });
});
