import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ScopePicker } from "./ScopePicker";

describe("ScopePicker", () => {
  it("offers all three scopes by default, defaulting to This event", async () => {
    const onConfirm = vi.fn();
    render(<ScopePicker action="Edit" onConfirm={onConfirm} onClose={vi.fn()} />);

    expect(screen.getByText("Edit recurring event")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "This event" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "This and following events" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "All events" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Edit" }));

    expect(onConfirm).toHaveBeenCalledWith("this");
  });

  it("confirms whichever scope was picked", async () => {
    const onConfirm = vi.fn();
    render(<ScopePicker action="Edit" onConfirm={onConfirm} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole("radio", { name: "All events" }));
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));

    expect(onConfirm).toHaveBeenCalledWith("all");
  });

  it("narrows to the scopes it was given, defaulting to the first", async () => {
    const onConfirm = vi.fn();
    render(
      <ScopePicker
        action="Download"
        scopes={["this", "all"]}
        onConfirm={onConfirm}
        onClose={vi.fn()}
      />,
    );

    // "This and following" makes no sense for a single-file export (#78).
    expect(
      screen.queryByRole("radio", { name: "This and following events" }),
    ).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Download" }));

    expect(onConfirm).toHaveBeenCalledWith("this");
  });

  it("closes without confirming when cancelled", async () => {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(<ScopePicker action="Edit" onConfirm={onConfirm} onClose={onClose} />);

    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onClose).toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
