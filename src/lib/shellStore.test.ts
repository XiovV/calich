import { describe, expect, it } from "vitest";
import { useShellStore } from "./shellStore";

describe("useShellStore", () => {
  it("defaults activeView to week", () => {
    expect(useShellStore.getState().activeView).toBe("week");
  });

  // #304, ADR-0082: no Preference seeds this, unlike Default view — every
  // reload starts on "All calendars" since the store itself re-initializes.
  it("defaults activeCalendarSetId to null (All calendars)", () => {
    expect(useShellStore.getState().activeCalendarSetId).toBeNull();
  });

  // #317, ADR-0083: the Tasks panel is closed by default.
  it("defaults tasksPanelOpen to false", () => {
    expect(useShellStore.getState().tasksPanelOpen).toBe(false);
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

// #317, ADR-0083: the Lists filter's checked/known pair carries the
// identical rule as Calendar toggle's, via the same extracted
// reconcileCheckedIds helper — these tests mirror the Calendar ones above.
describe("reconcileCheckedTaskListIds", () => {
  it("checks an id it has never seen before", () => {
    useShellStore.setState({
      checkedTaskListIds: new Set(),
      knownTaskListIds: new Set(),
    });

    useShellStore.getState().reconcileCheckedTaskListIds([1]);

    expect(useShellStore.getState().checkedTaskListIds).toEqual(new Set([1]));
  });

  it("leaves a previously-seen, deliberately-unchecked id unchecked", () => {
    useShellStore.setState({
      checkedTaskListIds: new Set(),
      knownTaskListIds: new Set([1]),
    });

    useShellStore.getState().reconcileCheckedTaskListIds([1]);

    expect(useShellStore.getState().checkedTaskListIds).toEqual(new Set());
  });

  it("leaves an already-checked id checked and adds a newly-seen one alongside it", () => {
    useShellStore.setState({
      checkedTaskListIds: new Set([1]),
      knownTaskListIds: new Set([1]),
    });

    useShellStore.getState().reconcileCheckedTaskListIds([1, 2]);

    expect(useShellStore.getState().checkedTaskListIds).toEqual(new Set([1, 2]));
  });
});

describe("toggleTaskListChecked", () => {
  it("toggles an id in and out of checkedTaskListIds", () => {
    useShellStore.setState({ checkedTaskListIds: new Set() });

    useShellStore.getState().toggleTaskListChecked(1);
    expect(useShellStore.getState().checkedTaskListIds).toEqual(new Set([1]));

    useShellStore.getState().toggleTaskListChecked(1);
    expect(useShellStore.getState().checkedTaskListIds).toEqual(new Set());
  });
});

describe("addCheckedTaskListId", () => {
  it("checks the id and marks it known, so a later reconcile doesn't undo a deliberate uncheck", () => {
    useShellStore.setState({
      checkedTaskListIds: new Set(),
      knownTaskListIds: new Set(),
    });

    useShellStore.getState().addCheckedTaskListId(1);
    expect(useShellStore.getState().checkedTaskListIds).toEqual(new Set([1]));
    expect(useShellStore.getState().knownTaskListIds).toEqual(new Set([1]));

    useShellStore.getState().toggleTaskListChecked(1);
    useShellStore.getState().reconcileCheckedTaskListIds([1]);

    expect(useShellStore.getState().checkedTaskListIds).toEqual(new Set());
  });
});

describe("setTasksPanelOpen", () => {
  it("sets tasksPanelOpen", () => {
    useShellStore.getState().setTasksPanelOpen(true);
    expect(useShellStore.getState().tasksPanelOpen).toBe(true);

    useShellStore.getState().setTasksPanelOpen(false);
    expect(useShellStore.getState().tasksPanelOpen).toBe(false);
  });
});
