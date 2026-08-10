import { describe, expect, it } from "vitest";
import { useShellStore } from "./shellStore";

describe("useShellStore", () => {
  it("defaults activeView to week", () => {
    expect(useShellStore.getState().activeView).toBe("week");
  });
});

describe("reconcileCheckedCalendarIds", () => {
  it("checks an id it has never seen before", () => {
    useShellStore.setState({
      checkedCalendarIds: new Set(),
      knownCalendarIds: new Set(),
    });

    useShellStore.getState().reconcileCheckedCalendarIds(["cal-1"]);

    expect(useShellStore.getState().checkedCalendarIds).toEqual(new Set(["cal-1"]));
  });

  it("leaves a previously-seen, deliberately-unchecked id unchecked", () => {
    useShellStore.setState({
      checkedCalendarIds: new Set(),
      knownCalendarIds: new Set(["cal-1"]),
    });

    useShellStore.getState().reconcileCheckedCalendarIds(["cal-1"]);

    expect(useShellStore.getState().checkedCalendarIds).toEqual(new Set());
  });

  it("leaves an already-checked id checked and adds a newly-seen one alongside it", () => {
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-1"]),
      knownCalendarIds: new Set(["cal-1"]),
    });

    useShellStore.getState().reconcileCheckedCalendarIds(["cal-1", "cal-2"]);

    expect(useShellStore.getState().checkedCalendarIds).toEqual(new Set(["cal-1", "cal-2"]));
  });

  it("auto-checks a Calendar again after it was revoked and re-Shared, rather than treating it as already known", () => {
    useShellStore.setState({
      checkedCalendarIds: new Set(["cal-1"]),
      knownCalendarIds: new Set(["cal-1"]),
    });

    // The Share is revoked — a refetch no longer lists cal-1.
    useShellStore.getState().reconcileCheckedCalendarIds([]);
    expect(useShellStore.getState().checkedCalendarIds).toEqual(new Set(["cal-1"]));
    expect(useShellStore.getState().knownCalendarIds).toEqual(new Set());

    // The caller deliberately unchecks it while it's gone (e.g. via the
    // sidebar, which no longer renders it — simulated directly here).
    useShellStore.getState().removeCheckedCalendarId("cal-1");

    // It is re-Shared and reappears in a later refetch.
    useShellStore.getState().reconcileCheckedCalendarIds(["cal-1"]);

    expect(useShellStore.getState().checkedCalendarIds).toEqual(new Set(["cal-1"]));
  });
});

describe("addCheckedCalendarId", () => {
  it("checks the id and marks it known", () => {
    useShellStore.setState({
      checkedCalendarIds: new Set(),
      knownCalendarIds: new Set(),
    });

    useShellStore.getState().addCheckedCalendarId("cal-1");

    expect(useShellStore.getState().checkedCalendarIds).toEqual(new Set(["cal-1"]));
    expect(useShellStore.getState().knownCalendarIds).toEqual(new Set(["cal-1"]));
  });

  it("marking it known keeps a later deliberate uncheck from being undone by a reconcile", () => {
    useShellStore.setState({
      checkedCalendarIds: new Set(),
      knownCalendarIds: new Set(),
    });

    useShellStore.getState().addCheckedCalendarId("cal-1");
    useShellStore.getState().removeCheckedCalendarId("cal-1");
    useShellStore.getState().reconcileCheckedCalendarIds(["cal-1"]);

    expect(useShellStore.getState().checkedCalendarIds).toEqual(new Set());
  });
});
