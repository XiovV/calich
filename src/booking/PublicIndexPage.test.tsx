import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import type { PublicIndex } from "../lib/publicIndexApi";

vi.mock("../lib/publicIndexApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/publicIndexApi")>("../lib/publicIndexApi");
  return {
    ...actual,
    publicIndexApi: { get: vi.fn() },
  };
});

const { publicIndexApi } = await import("../lib/publicIndexApi");
const { PublicIndexPage } = await import("./PublicIndexPage");

const index: PublicIndex = {
  hostName: "Damir Hadzagic",
  links: [
    { slug: "intro-call", title: "Intro call", durationMinutes: 30 },
    { slug: "deep-dive", title: "Deep dive", durationMinutes: 60 },
  ],
};

function renderPage(path = "/damir") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/:handle" element={<PublicIndexPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("PublicIndexPage", () => {
  it("renders the owner's name, initials, and every Public link with its title and duration", async () => {
    vi.mocked(publicIndexApi.get).mockResolvedValue(index);

    renderPage();

    expect(await screen.findByText("Damir Hadzagic")).toBeInTheDocument();
    expect(screen.getByText("DH")).toBeInTheDocument();
    expect(screen.getByText("Intro call")).toBeInTheDocument();
    expect(screen.getByText("30 min")).toBeInTheDocument();
    expect(screen.getByText("Deep dive")).toBeInTheDocument();
    expect(screen.getByText("60 min")).toBeInTheDocument();
    expect(publicIndexApi.get).toHaveBeenCalledWith("damir");
  });

  it("links each entry to /:handle/:slug", async () => {
    vi.mocked(publicIndexApi.get).mockResolvedValue(index);

    renderPage();

    expect(await screen.findByRole("link", { name: /Intro call/ })).toHaveAttribute(
      "href",
      "/damir/intro-call",
    );
  });

  // #325's AC: "A Handle with no Public links renders an empty index rather
  // than an error" — no name is shown either way, since the backend never
  // reveals one when links is empty (ADR-0084's enumeration guard).
  it("renders an empty index without a name for a Handle with no Public links", async () => {
    vi.mocked(publicIndexApi.get).mockResolvedValue({ hostName: "", links: [] });

    renderPage();

    expect(await screen.findByText("Nothing here yet.")).toBeInTheDocument();
    expect(screen.queryByText(index.hostName)).not.toBeInTheDocument();
  });

  // #325's AC: "An unknown Handle returns the same response as a Handle
  // with no Public links" — the page has no separate "not found" branch at
  // all, unlike PublicBookingPage: both render the same empty state.
  it("renders the same empty index for an unknown Handle", async () => {
    vi.mocked(publicIndexApi.get).mockResolvedValue({ hostName: "", links: [] });

    renderPage("/no-such-handle");

    expect(await screen.findByText("Nothing here yet.")).toBeInTheDocument();
  });

  it("shows a generic error when the request fails", async () => {
    vi.mocked(publicIndexApi.get).mockRejectedValue(new Error("network down"));

    renderPage();

    expect(await screen.findByText("Something went wrong.")).toBeInTheDocument();
  });
});
