import { describe, expect, it } from "vitest";
import { DEFAULT_TIME_BLOCK_DURATION_MINUTES, taskDropIntent } from "./taskDropIntent";

describe("taskDropIntent", () => {
  it("sets a Time block at the drop time with a 1h default duration for a panel row dropped on the hourly grid", () => {
    const dropTime = new Date(2026, 8, 16, 14, 30);

    const result = taskDropIntent({ surface: "hourlyGrid", time: dropTime });

    expect(result).toEqual({
      start: dropTime,
      durationMinutes: DEFAULT_TIME_BLOCK_DURATION_MINUTES,
    });
  });

  it("never writes a Deadline: the result carries only start/durationMinutes", () => {
    const result = taskDropIntent({ surface: "hourlyGrid", time: new Date(2026, 8, 16, 9, 0) });

    expect(result).not.toHaveProperty("due");
    expect(Object.keys(result).sort()).toEqual(["durationMinutes", "start"]);
  });

  it("always defaults to a 60-minute duration, regardless of the drop time", () => {
    const morning = taskDropIntent({ surface: "hourlyGrid", time: new Date(2026, 8, 16, 6, 0) });
    const evening = taskDropIntent({ surface: "hourlyGrid", time: new Date(2026, 8, 16, 22, 45) });

    expect(morning.durationMinutes).toBe(60);
    expect(evening.durationMinutes).toBe(60);
  });
});
