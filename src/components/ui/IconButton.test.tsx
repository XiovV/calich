import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { IconButton } from "./IconButton";

// An IconButton shows nothing but an icon, so its aria-label is the only
// statement of what it does. These pin that the label reaches sighted users
// as a hover tooltip too, without the label itself changing.
describe("IconButton", () => {
  it("mirrors its aria-label into a hover tooltip", () => {
    render(<IconButton aria-label="Remove Damir">x</IconButton>);

    expect(screen.getByRole("button", { name: "Remove Damir" })).toHaveAttribute(
      "title",
      "Remove Damir",
    );
  });

  it("keeps an explicit title when the caller sets one", () => {
    render(
      <IconButton aria-label="Remove Damir" title="Remove from this workspace">
        x
      </IconButton>,
    );

    expect(screen.getByRole("button", { name: "Remove Damir" })).toHaveAttribute(
      "title",
      "Remove from this workspace",
    );
  });

  it("adds no tooltip when there is no aria-label", () => {
    render(<IconButton>x</IconButton>);

    expect(screen.getByRole("button")).not.toHaveAttribute("title");
  });
});
