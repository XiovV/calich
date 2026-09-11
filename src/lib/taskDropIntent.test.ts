import { describe, expect, it } from "vitest";
import { DEFAULT_TIME_BLOCK_DURATION_MINUTES, taskDropIntent, type TaskDropWrite } from "./taskDropIntent";

describe("taskDropIntent", () => {
  it("sets a Time block at the drop time with a 1h default duration for a panel row dropped on the hourly grid (#313)", () => {
    const dropTime = new Date(2026, 8, 16, 14, 30);

    const result = taskDropIntent({ kind: "panelRow" }, { surface: "hourlyGrid", time: dropTime });

    expect(result).toEqual({
      action: "setTimeBlock",
      start: dropTime,
      durationMinutes: DEFAULT_TIME_BLOCK_DURATION_MINUTES,
    });
  });

  it("moves a Time block dragged elsewhere on the grid, keeping its own duration and leaving the Deadline untouched", () => {
    const dropTime = new Date(2026, 8, 17, 9, 0);

    const result = taskDropIntent({ kind: "timeBlock", durationMinutes: 90 }, { surface: "hourlyGrid", time: dropTime });

    expect(result).toEqual({ action: "setTimeBlock", start: dropTime, durationMinutes: 90 });
    expect(result).not.toHaveProperty("due");
  });

  it("sets the Deadline and clears the Time block when a Time block is dragged into the all-day lane", () => {
    const dropDate = new Date(2026, 8, 18);

    const result = taskDropIntent({ kind: "timeBlock", durationMinutes: 60 }, { surface: "allDayLane", date: dropDate });

    expect(result).toEqual({ action: "setDeadlineAndClearTimeBlock", due: dropDate });
  });

  it("clears the Time block and nothing else when a Time block is dragged back to the panel, leaving the Deadline untouched", () => {
    const result = taskDropIntent({ kind: "timeBlock", durationMinutes: 60 }, { surface: "panel" });

    expect(result).toEqual({ action: "clearTimeBlock" });
    expect(result).not.toHaveProperty("due");
  });

  it("sets a Time block with a 1h default duration when an all-day chip is dragged onto the hourly grid, leaving the Deadline untouched", () => {
    const dropTime = new Date(2026, 8, 19, 11, 0);

    const result = taskDropIntent({ kind: "allDayChip" }, { surface: "hourlyGrid", time: dropTime });

    expect(result).toEqual({
      action: "setTimeBlock",
      start: dropTime,
      durationMinutes: DEFAULT_TIME_BLOCK_DURATION_MINUTES,
    });
    expect(result).not.toHaveProperty("due");
  });

  it("always defaults a fresh block to 60 minutes, regardless of the drop time", () => {
    const morning = taskDropIntent({ kind: "panelRow" }, { surface: "hourlyGrid", time: new Date(2026, 8, 16, 6, 0) });
    const evening = taskDropIntent({ kind: "allDayChip" }, { surface: "hourlyGrid", time: new Date(2026, 8, 16, 22, 45) });

    expect(morning).toMatchObject({ durationMinutes: 60 });
    expect(evening).toMatchObject({ durationMinutes: 60 });
  });

  // A minimal model of a Task's two independent axes, updated by whichever
  // write taskDropIntent returns — just enough to drive the round trip below
  // without pulling in the real Task shape.
  interface FakeTaskState {
    start: Date | null;
    durationMinutes: number | null;
    due: Date | null;
  }

  function applyWrite(state: FakeTaskState, write: TaskDropWrite): FakeTaskState {
    switch (write.action) {
      case "setTimeBlock":
        return { ...state, start: write.start, durationMinutes: write.durationMinutes };
      case "setDeadlineAndClearTimeBlock":
        return { start: null, durationMinutes: null, due: write.due };
      case "clearTimeBlock":
        return { ...state, start: null, durationMinutes: null };
    }
  }

  it("round trip panel -> grid -> all-day lane -> grid -> panel leaves the Deadline exactly as it started", () => {
    // The Deadline is only ever written by the all-day-lane leg, so a round
    // trip that drops back onto the same day it was already due on comes out
    // exactly where it started — proof that the other three legs (the grid
    // creation, the trip back onto the grid, and the trip back to the panel)
    // never so much as touch `due`, matching each row's own assertion above,
    // chained end to end.
    const startingDue = new Date(2026, 8, 18);
    let task: FakeTaskState = { start: null, durationMinutes: null, due: startingDue };

    // panel -> grid
    task = applyWrite(task, taskDropIntent({ kind: "panelRow" }, { surface: "hourlyGrid", time: new Date(2026, 8, 16, 10, 0) }));
    expect(task.due).toEqual(startingDue);

    // grid -> all-day lane, dropped on the day it was already due
    task = applyWrite(task, taskDropIntent({ kind: "timeBlock", durationMinutes: task.durationMinutes! }, { surface: "allDayLane", date: startingDue }));
    expect(task).toEqual({ start: null, durationMinutes: null, due: startingDue });

    // all-day lane -> grid
    task = applyWrite(task, taskDropIntent({ kind: "allDayChip" }, { surface: "hourlyGrid", time: new Date(2026, 8, 17, 15, 0) }));
    expect(task.due).toEqual(startingDue);

    // grid -> panel
    task = applyWrite(task, taskDropIntent({ kind: "timeBlock", durationMinutes: task.durationMinutes! }, { surface: "panel" }));

    expect(task).toEqual({ start: null, durationMinutes: null, due: startingDue });
  });
});
