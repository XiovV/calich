import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { EventAttendeesSection } from "./EventAttendeesSection";
import type { WorkspaceMember } from "../lib/workspaceMembersApi";

// Same convention as AccountSection.test.tsx: the *Api modules are mocked,
// authStore is real — this covers what the section renders off the fetched
// Attendees/Members/Groups, not the request/response shape itself.
vi.mock("../lib/attendeesApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/attendeesApi")>("../lib/attendeesApi");
  return {
    ...actual,
    attendeesApi: { ...actual.attendeesApi, list: vi.fn() },
  };
});
vi.mock("../lib/workspaceMembersApi", async () => {
  const actual =
    await vi.importActual<typeof import("../lib/workspaceMembersApi")>("../lib/workspaceMembersApi");
  return {
    ...actual,
    workspaceMembersApi: { ...actual.workspaceMembersApi, list: vi.fn() },
  };
});
vi.mock("../lib/groupsApi", async () => {
  const actual = await vi.importActual<typeof import("../lib/groupsApi")>("../lib/groupsApi");
  return {
    ...actual,
    groupsApi: { ...actual.groupsApi, list: vi.fn() },
  };
});

const { attendeesApi } = await import("../lib/attendeesApi");
const { workspaceMembersApi } = await import("../lib/workspaceMembersApi");
const { groupsApi } = await import("../lib/groupsApi");
const { useAuthStore } = await import("../lib/authStore");

function member(userId: number, name: string): WorkspaceMember {
  return { userId, name, email: `${name.toLowerCase()}@example.com`, role: "member", createdAt: "2026-01-01T00:00:00Z" };
}

const organizer = member(1, "Organizer");
const otherMember = member(2, "Other Member");

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({
    status: "authenticated",
    accessToken: "token-123",
    user: {
      id: organizer.userId,
      name: organizer.name,
      mustChangePassword: false,
      email: organizer.email,
      emailReminderChannelAvailable: false,
      googleProviderAvailable: false,
      invitationRepliesConfigured: false,
      syncedDeviceRemindersEnabled: false,
      weekStart: 1,
      defaultView: "week",
      timeFormat: "24h",
      workingHoursStart: null,
      workingHoursEnd: null,
    },
    pendingEmail: null,
  });
  vi.mocked(groupsApi.list).mockResolvedValue([]);
});

function renderSection() {
  return render(
    <EventAttendeesSection
      eventId="evt-1"
      canManage
      organizerName={organizer.name}
      allowEmailInvite={false}
    />,
  );
}

// #237: the section used to combine two independent empty-state fallbacks —
// "Nobody has been invited yet." (the Attendee list is empty) and "Everyone
// and every group in this workspace is already invited." (the invite picker
// is empty) — which contradict each other in a solo Workspace, where the
// picker starts empty even though nobody's invited. Each of the three
// reachable states below must show exactly one honest sentence.
describe("EventAttendeesSection empty states (#237)", () => {
  it("says there's nobody else to invite in a solo Workspace with no Attendees", async () => {
    vi.mocked(attendeesApi.list).mockResolvedValue([]);
    vi.mocked(workspaceMembersApi.list).mockResolvedValue([organizer]);
    renderSection();

    expect(await screen.findByText("There is nobody else in this workspace to invite.")).toBeInTheDocument();
    expect(screen.queryByText("Nobody has been invited yet.")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Everyone and every group in this workspace is already invited."),
    ).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Person or group to invite")).not.toBeInTheDocument();
  });

  it("shows the empty-list message and an invite picker when other Members exist and nobody's invited", async () => {
    vi.mocked(attendeesApi.list).mockResolvedValue([]);
    vi.mocked(workspaceMembersApi.list).mockResolvedValue([organizer, otherMember]);
    renderSection();

    expect(await screen.findByText("Nobody has been invited yet.")).toBeInTheDocument();
    expect(screen.getByLabelText("Person or group to invite")).toBeInTheDocument();
    expect(
      screen.queryByText("There is nobody else in this workspace to invite."),
    ).not.toBeInTheDocument();
  });

  // An email-shaped Attendee (#200, ADR-0058) touches neither availableUsers
  // nor availableGroups, so it must not be mistaken for "every Member is
  // already invited" in a solo Workspace — there was never anyone to invite
  // there in the first place.
  it("says nothing false when a solo Workspace has only invited an outside email address", async () => {
    vi.mocked(attendeesApi.list).mockResolvedValue([
      { userId: null, email: "outsider@example.com", response: "needs-action" },
    ]);
    vi.mocked(workspaceMembersApi.list).mockResolvedValue([organizer]);
    renderSection();

    await screen.findByText("outsider@example.com");
    expect(
      screen.queryByText("Everyone and every group in this workspace is already invited."),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Nobody has been invited yet.")).not.toBeInTheDocument();
    expect(
      screen.queryByText("There is nobody else in this workspace to invite."),
    ).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Person or group to invite")).not.toBeInTheDocument();
  });

  it("shows only the already-invited message once every Member is already an Attendee", async () => {
    vi.mocked(attendeesApi.list).mockResolvedValue([
      { userId: otherMember.userId, name: otherMember.name, email: otherMember.email, response: "needs-action" },
    ]);
    vi.mocked(workspaceMembersApi.list).mockResolvedValue([organizer, otherMember]);
    renderSection();

    expect(
      await screen.findByText("Everyone and every group in this workspace is already invited."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Nobody has been invited yet.")).not.toBeInTheDocument();
    expect(
      screen.queryByText("There is nobody else in this workspace to invite."),
    ).not.toBeInTheDocument();
  });
});
