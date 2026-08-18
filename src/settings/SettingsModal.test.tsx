import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { SettingsModal } from "./SettingsModal";
import { getSettingsSections } from "./settingsSections";

// The "Settings" heading is the dialog's accessible name (ADR-0049) — no
// Section's own <h2> doubles as the title. It has moved out of a header bar
// and into the rail, so these pin the contract that survived the move: the
// dialog is still named, and every Section is still reachable from the rail.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/settings" element={<SettingsModal />}>
          {getSettingsSections().map((section) => (
            <Route key={section.path} path={section.path} element={<h2>{section.label}</h2>} />
          ))}
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("SettingsModal", () => {
  it("names the dialog Settings", () => {
    renderAt("/settings/preferences");

    expect(screen.getByRole("dialog", { name: "Settings" })).toBeInTheDocument();
  });

  it("links every Section from the rail, and closes", () => {
    renderAt("/settings/preferences");

    for (const section of getSettingsSections()) {
      expect(screen.getByRole("link", { name: section.label })).toHaveAttribute(
        "href",
        `/settings/${section.path}`,
      );
    }

    expect(screen.getByRole("button", { name: "Close settings" })).toBeInTheDocument();
  });

  // The close button was briefly the LAST focusable in the dialog, behind all
  // seven rail links, when it moved into the content column. It is positioned
  // rather than laid out so that it stays first.
  it("puts the close button first in the tab order", () => {
    renderAt("/settings/preferences");

    const dialog = screen.getByRole("dialog", { name: "Settings" });
    const focusable = dialog.querySelectorAll("a[href], button");

    expect(focusable[0]).toBe(screen.getByRole("button", { name: "Close settings" }));
  });

  it("marks the Section matching the route as current", () => {
    renderAt("/settings/account");

    expect(screen.getByRole("link", { name: "Account" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "Preferences" })).not.toHaveAttribute(
      "aria-current",
    );
  });
});
