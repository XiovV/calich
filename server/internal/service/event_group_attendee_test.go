// event_group_attendee_test.go covers #162/ADR-0046's Group-targeted
// Attendee invite: a one-time snapshot expansion into individual attendees
// rows, distinct from Group Shares (ADR-0045, calendar_group_share_test.go)
// which resolve membership dynamically instead.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// groupAttendeeFixture is #162's fixture: an Owner's Calendar inside a
// Workspace, a Group ("Tech team") containing two of that Workspace's
// Members, and a second Workspace with its own Group — the target the
// "Group must belong to this Workspace" refusal test invites instead.
type groupAttendeeFixture struct {
	events         *EventService
	users          *repository.UserRepository
	groups         *repository.GroupRepository
	ownerID        int64
	member1ID      int64
	member2ID      int64
	calendarID     string
	groupID        int64
	outsideGroupID int64
}

func newGroupAttendeeFixture(t *testing.T) groupAttendeeFixture {
	t.Helper()
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member1, err := users.Create(ctx, "member1", "member1@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member1: %v", err)
	}
	member2, err := users.Create(ctx, "member2", "member2@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member2: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	for _, m := range []int64{owner.ID, member1.ID, member2.ID} {
		if err := workspaceRepo.AddMember(ctx, workspace.ID, m, repository.WorkspaceRoleMember); err != nil {
			t.Fatalf("add workspace member: %v", err)
		}
	}

	groupRepo := repository.NewGroupRepository(sqlDB)
	group, err := groupRepo.Create(ctx, workspace.ID, "Tech team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := groupRepo.AddMember(ctx, group.ID, member1.ID); err != nil {
		t.Fatalf("add member1 to group: %v", err)
	}
	if err := groupRepo.AddMember(ctx, group.ID, member2.ID); err != nil {
		t.Fatalf("add member2 to group: %v", err)
	}

	// A second Workspace with its own Group, belonging to neither owner nor
	// the Calendar below — the target for the cross-Workspace refusal test.
	otherWorkspace, err := workspaceRepo.Create(ctx, "Other Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	outsideGroup, err := groupRepo.Create(ctx, otherWorkspace.ID, "Design team")
	if err != nil {
		t.Fatalf("create outside group: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	cal, err := calendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	calendarService := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), groupRepo)
	eventService := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, groupRepo)

	return groupAttendeeFixture{
		events: eventService, users: users, groups: groupRepo,
		ownerID: owner.ID, member1ID: member1.ID, member2ID: member2.ID,
		calendarID: cal.ID, groupID: group.ID, outsideGroupID: outsideGroup.ID,
	}
}

func createGroupAttendeeTestEvent(t *testing.T, f groupAttendeeFixture, id string) repository.Event {
	t.Helper()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	event, err := f.events.Create(context.Background(), f.ownerID, id, EventWrite{CalendarID: f.calendarID, Title: "Discuss tech stack", Start: start, End: end})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	return event
}

// TestEventService_AddGroupAttendee_ExpandsToMemberRows covers #162's core
// acceptance criterion: inviting a Group creates one Attendee row per
// current Group member, and each behaves exactly like an individually
// invited one — independent visibility, own default response.
func TestEventService_AddGroupAttendee_ExpandsToMemberRows(t *testing.T) {
	f := newGroupAttendeeFixture(t)
	ctx := context.Background()
	event := createGroupAttendeeTestEvent(t, f, "evt-1")

	added, err := f.events.AddGroupAttendee(ctx, f.ownerID, event.ID, f.groupID)
	if err != nil {
		t.Fatalf("add group attendee: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("expected 2 attendees added, got %+v", added)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected 2 attendees, got %+v", attendees)
	}
	for _, memberID := range []int64{f.member1ID, f.member2ID} {
		got, err := f.events.Get(ctx, memberID, event.ID)
		if err != nil {
			t.Fatalf("expected member %d to see event, got %v", memberID, err)
		}
		if got.ID != event.ID {
			t.Fatalf("expected event %q, got %q", event.ID, got.ID)
		}

		attendee, err := f.events.attendees.Get(ctx, event.ID, memberID)
		if err != nil {
			t.Fatalf("expected attendee row for member %d: %v", memberID, err)
		}
		if attendee.Response != repository.ResponseNeedsAction {
			t.Fatalf("expected needs-action, got %q", attendee.Response)
		}

		// Each Attendee's response is independently theirs to set, exactly
		// like an individually invited one.
		if _, err := f.events.SetResponse(ctx, memberID, event.ID, repository.ResponseAccepted); err != nil {
			t.Fatalf("set response for member %d: %v", memberID, err)
		}
	}

	other, err := f.events.attendees.Get(ctx, event.ID, f.member1ID)
	if err != nil {
		t.Fatalf("get member1 attendee: %v", err)
	}
	if other.Response != repository.ResponseAccepted {
		t.Fatalf("expected member1's own response to have taken independently, got %q", other.Response)
	}
}

// TestEventService_AddGroupAttendee_LaterMembershipChangeDoesNotAlterExistingAttendees
// covers #162's snapshot acceptance criterion: adding a new member to the
// Group after the invite does not retroactively add them as an Attendee,
// and removing an already-invited member from the Group does not remove
// their Attendee row.
func TestEventService_AddGroupAttendee_LaterMembershipChangeDoesNotAlterExistingAttendees(t *testing.T) {
	f := newGroupAttendeeFixture(t)
	ctx := context.Background()
	event := createGroupAttendeeTestEvent(t, f, "evt-1")

	if _, err := f.events.AddGroupAttendee(ctx, f.ownerID, event.ID, f.groupID); err != nil {
		t.Fatalf("add group attendee: %v", err)
	}

	// A newcomer joins the Group after the invite was already expanded.
	newcomer, err := f.users.Create(ctx, "newcomer", "newcomer@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create newcomer: %v", err)
	}
	if err := f.groups.AddMember(ctx, f.groupID, newcomer.ID); err != nil {
		t.Fatalf("add newcomer to group: %v", err)
	}

	// member1 leaves the Group after the invite was already expanded.
	if err := f.groups.RemoveMember(ctx, f.groupID, f.member1ID); err != nil {
		t.Fatalf("remove member1 from group: %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected the original 2 attendees untouched, got %+v", attendees)
	}

	if _, err := f.events.attendees.Get(ctx, event.ID, f.member1ID); err != nil {
		t.Fatalf("expected member1's attendee row to survive their leaving the group: %v", err)
	}
	if _, err := f.events.attendees.Get(ctx, event.ID, newcomer.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected newcomer to have no attendee row, got %v", err)
	}
}

// TestEventService_AddGroupAttendee_RefusesGroupOutsideWorkspace covers
// #162's "invite target Group must belong to the Event's Calendar's
// Workspace" acceptance criterion.
func TestEventService_AddGroupAttendee_RefusesGroupOutsideWorkspace(t *testing.T) {
	f := newGroupAttendeeFixture(t)
	ctx := context.Background()
	event := createGroupAttendeeTestEvent(t, f, "evt-1")

	_, err := f.events.AddGroupAttendee(ctx, f.ownerID, event.ID, f.outsideGroupID)
	if !errors.Is(err, ErrAttendeeTargetNotInWorkspace) {
		t.Fatalf("expected ErrAttendeeTargetNotInWorkspace, got %v", err)
	}
}

func TestEventService_AddGroupAttendee_RefusesUnknownGroup(t *testing.T) {
	f := newGroupAttendeeFixture(t)
	ctx := context.Background()
	event := createGroupAttendeeTestEvent(t, f, "evt-1")

	_, err := f.events.AddGroupAttendee(ctx, f.ownerID, event.ID, 999999)
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}

func TestEventService_AddGroupAttendee_RefusesNonEditorCaller(t *testing.T) {
	f := newGroupAttendeeFixture(t)
	ctx := context.Background()
	event := createGroupAttendeeTestEvent(t, f, "evt-1")

	// member1 has no Access to the Calendar, so they may not invite a Group
	// to its Events.
	_, err := f.events.AddGroupAttendee(ctx, f.member1ID, event.ID, f.groupID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestEventService_AddGroupAttendee_SkipsAlreadyInvitedMembers covers
// re-inviting a Group (or a Group overlapping a prior individual invite)
// without failing the whole expansion over one already-existing row.
func TestEventService_AddGroupAttendee_SkipsAlreadyInvitedMembers(t *testing.T) {
	f := newGroupAttendeeFixture(t)
	ctx := context.Background()
	event := createGroupAttendeeTestEvent(t, f, "evt-1")

	if _, err := f.events.AddAttendee(ctx, f.ownerID, event.ID, f.member1ID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	added, err := f.events.AddGroupAttendee(ctx, f.ownerID, event.ID, f.groupID)
	if err != nil {
		t.Fatalf("add group attendee: %v", err)
	}
	if len(added) != 1 || added[0].UserID != f.member2ID {
		t.Fatalf("expected only member2 newly added, got %+v", added)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected 2 attendees total, got %+v", attendees)
	}
}

// TestEventService_AddGroupAttendee_SkipsDisabledMembers covers the "behaves
// exactly like an individually-invited one" acceptance criterion from the
// other direction: a Disabled Group member is refused by AddAttendee
// (ADR-0037), so expanding a Group containing one must not produce an
// Attendee row AddAttendee itself would have refused to create.
func TestEventService_AddGroupAttendee_SkipsDisabledMembers(t *testing.T) {
	f := newGroupAttendeeFixture(t)
	ctx := context.Background()
	event := createGroupAttendeeTestEvent(t, f, "evt-1")

	if _, err := f.users.SetDisabled(ctx, f.member1ID, true); err != nil {
		t.Fatalf("disable member1: %v", err)
	}

	added, err := f.events.AddGroupAttendee(ctx, f.ownerID, event.ID, f.groupID)
	if err != nil {
		t.Fatalf("add group attendee: %v", err)
	}
	if len(added) != 1 || added[0].UserID != f.member2ID {
		t.Fatalf("expected only member2 added, got %+v", added)
	}

	if _, err := f.events.attendees.Get(ctx, event.ID, f.member1ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected no attendee row for the disabled member, got %v", err)
	}
}
