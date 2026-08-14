import { describe, expect, it } from "vitest";
import { shouldShowAttachmentsRow } from "./attachmentsSection";

describe("shouldShowAttachmentsRow", () => {
  it("is true for a writable event with no attachments — the row is the 'Add attachment' affordance", () => {
    expect(shouldShowAttachmentsRow(false, 0)).toBe(true);
  });

  it("is true for a writable event with attachments", () => {
    expect(shouldShowAttachmentsRow(false, 3)).toBe(true);
  });

  it("is false for a read-only event with no attachments — nothing to show and no affordance to offer", () => {
    expect(shouldShowAttachmentsRow(true, 0)).toBe(false);
  });

  it("is true for a read-only event with attachments — the list itself is the content", () => {
    expect(shouldShowAttachmentsRow(true, 2)).toBe(true);
  });
});
