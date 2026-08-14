import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./toast", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const { toast } = await import("./toast");
const { ApiError } = await import("./apiClient");
const { makeOptimisticWrite } = await import("./optimisticWrite");

/** A promise plus the handles to settle it, so a test can assert on the
 * state of the world while `dispatch` is still in flight. */
function deferred() {
  let resolve!: () => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function parts() {
  return {
    apply: vi.fn(),
    revert: vi.fn(),
    fallbackMessage: "Failed to do the thing.",
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("makeOptimisticWrite", () => {
  it("paints before the server has answered", async () => {
    const write = makeOptimisticWrite();
    const inFlight = deferred();
    const { apply, revert, fallbackMessage } = parts();

    const result = write({
      apply,
      revert,
      dispatch: () => inFlight.promise,
      fallbackMessage,
    });

    expect(apply).toHaveBeenCalledTimes(1);
    expect(revert).not.toHaveBeenCalled();

    inFlight.resolve();
    await result;
  });

  it("keeps the change and reports success", async () => {
    const write = makeOptimisticWrite();
    const { apply, revert, fallbackMessage } = parts();

    const landed = await write({
      apply,
      revert,
      dispatch: vi.fn().mockResolvedValue(undefined),
      fallbackMessage,
    });

    expect(landed).toBe(true);
    expect(revert).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("runs onSuccess once the dispatch resolves", async () => {
    const write = makeOptimisticWrite();
    const order: string[] = [];
    const { apply, revert, fallbackMessage } = parts();

    await write({
      apply,
      revert,
      dispatch: async () => {
        order.push("dispatch");
      },
      onSuccess: async () => {
        order.push("onSuccess");
      },
      fallbackMessage,
    });

    expect(order).toEqual(["dispatch", "onSuccess"]);
  });

  it("reverts when onSuccess itself fails", async () => {
    const write = makeOptimisticWrite();
    const { apply, revert, fallbackMessage } = parts();

    const landed = await write({
      apply,
      revert,
      dispatch: vi.fn().mockResolvedValue(undefined),
      onSuccess: () => Promise.reject(new Error("nope")),
      fallbackMessage,
    });

    expect(landed).toBe(false);
    expect(revert).toHaveBeenCalledTimes(1);
    expect(toast.error).toHaveBeenCalledWith("Failed to do the thing.");
  });

  it("reverts and reports the failure", async () => {
    const write = makeOptimisticWrite();
    const { apply, revert, fallbackMessage } = parts();

    const landed = await write({
      apply,
      revert,
      dispatch: () => Promise.reject(new Error("network")),
      fallbackMessage,
    });

    expect(landed).toBe(false);
    expect(apply).toHaveBeenCalledTimes(1);
    expect(revert).toHaveBeenCalledTimes(1);
    expect(toast.error).toHaveBeenCalledWith("Failed to do the thing.");
  });

  describe("an Access change under the caller (#116)", () => {
    const forbidden = () => new ApiError(403, "forbidden", "Forbidden");
    const notFound = () => new ApiError(404, "not_found", "Not found");

    it("names the Calendar on a 403 instead of the generic message", async () => {
      const onAccessChange = vi
        .fn()
        .mockResolvedValue('Your access to "Team" has changed.');
      const write = makeOptimisticWrite(onAccessChange);
      const { apply, revert, fallbackMessage } = parts();

      await write({
        apply,
        revert,
        dispatch: () => Promise.reject(forbidden()),
        calendarId: "cal-2",
        fallbackMessage,
      });

      expect(onAccessChange).toHaveBeenCalledWith("cal-2");
      expect(toast.error).toHaveBeenCalledWith(
        'Your access to "Team" has changed.',
      );
    });

    it("treats a 404 the same way — the Event itself is gone", async () => {
      const onAccessChange = vi.fn().mockResolvedValue("Access changed.");
      const write = makeOptimisticWrite(onAccessChange);
      const { apply, revert, fallbackMessage } = parts();

      await write({
        apply,
        revert,
        dispatch: () => Promise.reject(notFound()),
        calendarId: "cal-2",
        fallbackMessage,
      });

      expect(toast.error).toHaveBeenCalledWith("Access changed.");
    });

    it("keeps the generic message for a failure that isn't one", async () => {
      const onAccessChange = vi.fn();
      const write = makeOptimisticWrite(onAccessChange);
      const { apply, revert, fallbackMessage } = parts();

      await write({
        apply,
        revert,
        dispatch: () =>
          Promise.reject(new ApiError(500, "internal", "Server error")),
        calendarId: "cal-2",
        fallbackMessage,
      });

      expect(onAccessChange).not.toHaveBeenCalled();
      expect(toast.error).toHaveBeenCalledWith("Failed to do the thing.");
    });

    it("keeps the generic message when the write names no Calendar", async () => {
      const onAccessChange = vi.fn();
      const write = makeOptimisticWrite(onAccessChange);
      const { apply, revert, fallbackMessage } = parts();

      // Creating the Calendar itself: there is no prior Access to have lost.
      await write({
        apply,
        revert,
        dispatch: () => Promise.reject(forbidden()),
        fallbackMessage,
      });

      expect(onAccessChange).not.toHaveBeenCalled();
      expect(toast.error).toHaveBeenCalledWith("Failed to do the thing.");
    });

    it("keeps the generic message for a store that binds no policy", async () => {
      // notificationsStore: a Notification has no Calendar to name.
      const write = makeOptimisticWrite();
      const { apply, revert, fallbackMessage } = parts();

      await write({
        apply,
        revert,
        dispatch: () => Promise.reject(forbidden()),
        calendarId: "cal-2",
        fallbackMessage,
      });

      expect(toast.error).toHaveBeenCalledWith("Failed to do the thing.");
    });
  });
});
