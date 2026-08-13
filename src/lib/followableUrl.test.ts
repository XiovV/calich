import { describe, expect, it } from "vitest";
import { followableUrl } from "./followableUrl";

describe("followableUrl", () => {
  it("returns an http value unchanged", () => {
    expect(followableUrl("http://example.com/ticket")).toBe("http://example.com/ticket");
  });

  it("returns an https value unchanged", () => {
    expect(followableUrl("https://example.com/ticket")).toBe("https://example.com/ticket");
  });

  it("is undefined for a javascript: value — never rendered as an href", () => {
    expect(followableUrl("javascript:alert(1)")).toBeUndefined();
  });

  it("is undefined for a non-web scheme, e.g. Apple Calendar's message:// links", () => {
    expect(followableUrl("message://<id>@mail.example.com")).toBeUndefined();
  });

  it("is undefined for a webcal: feed link", () => {
    expect(followableUrl("webcal://example.com/feed.ics")).toBeUndefined();
  });

  it("is undefined for a bare word that doesn't parse as a URL at all", () => {
    expect(followableUrl("conference room 4")).toBeUndefined();
  });

  it("is undefined for undefined", () => {
    expect(followableUrl(undefined)).toBeUndefined();
  });

  it("is undefined for an empty string", () => {
    expect(followableUrl("")).toBeUndefined();
  });
});
