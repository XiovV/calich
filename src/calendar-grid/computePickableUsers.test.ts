import { describe, expect, it } from "vitest";
import { computePickableUsers } from "./computePickableUsers";
import type { WorkspaceMember } from "../lib/workspaceMembersApi";

function member(userId: number, name: string): WorkspaceMember {
  return { userId, name, email: `${name.toLowerCase()}@example.com`, role: "member", createdAt: "2026-01-01T00:00:00Z" };
}

describe("computePickableUsers", () => {
  it("excludes the signed-in caller, even though they're not yet an invited or staged Attendee", () => {
    const availableUsers = [member(1, "Damir H"), member(2, "Alice")];

    const result = computePickableUsers(availableUsers, new Set(), new Set(), 1);

    expect(result).toEqual([member(2, "Alice")]);
  });
});
