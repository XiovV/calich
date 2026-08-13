// event_create_attendee_test.go covers #187/ADR-0055: Attendees named as
// part of EventService.Create itself, written inside the same transaction
// as the Event row, rather than reachable only through AddAttendee/
// AddGroupAttendee once the Event already exists.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// createAttendeeFixture mirrors groupAttendeeFixture (event_group_attendee_test.go):
// an owner's Calendar inside a Workspace, two plain Members, a Group
// containing both, an outsider belonging to no Workspace, and a second
// Workspace with its own Group — the targets the cross-Workspace refusal
// tests invite instead.
type createAttendeeFixture struct {
	events         *EventService
	users          *repository.UserRepository
	groups         *repository.GroupRepository
	ownerID        int64
	member1ID      int64
	member2ID      int64
	outsiderID     int64
	calendarID     string
	groupID        int64
	outsideGroupID int64
}

func newCreateAttendeeFixture(t *testing.T) createAttendeeFixture {
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
	outsider, err := users.Create(ctx, "outsider", "outsider@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
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
	eventService := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, groupRepo, repository.NewNotificationRepository(sqlDB), nil, 1000)

	return createAttendeeFixture{
		events: eventService, users: users, groups: groupRepo,
		ownerID: owner.ID, member1ID: member1.ID, member2ID: member2.ID, outsiderID: outsider.ID,
		calendarID: cal.ID, groupID: group.ID, outsideGroupID: outsideGroup.ID,
	}
}

func createAttendeeTestWrite(f createAttendeeFixture, opts EventWrite) EventWrite {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	opts.CalendarID = f.calendarID
	opts.Title = "Discuss tech stack"
	opts.Start = start
	opts.End = end
	return opts
}

// TestEventService_Create_InvitesExplicitUsers covers #187's core
// acceptance criterion: Users named in AttendeeUserIDs are Attendees the
// instant Create returns, with no separate AddAttendee call needed.
func TestEventService_Create_InvitesExplicitUsers(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.member1ID, f.member2ID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
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
	}
}

// TestEventService_Create_InvitesExplicitUsers_WritesOneInviteNotificationEach
// covers ADR-0061: inviting Users at create time writes exactly one invite
// Notification per invited User, naming the Event.
func TestEventService_Create_InvitesExplicitUsers_WritesOneInviteNotificationEach(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.member1ID, f.member2ID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, memberID := range []int64{f.member1ID, f.member2ID} {
		notifications, err := f.events.notifications.ListRecentByUser(ctx, memberID, 10)
		if err != nil {
			t.Fatalf("list notifications for %d: %v", memberID, err)
		}
		if len(notifications) != 1 {
			t.Fatalf("expected 1 notification for member %d, got %+v", memberID, notifications)
		}
		if notifications[0].Kind != repository.KindInvite || notifications[0].EventID != event.ID || notifications[0].Title != event.Title {
			t.Fatalf("unexpected notification for member %d: %+v", memberID, notifications[0])
		}
	}
}

// TestEventService_Create_RecurringEventWritesExactlyOneInviteNotification
// covers ADR-0061's core recurrence acceptance criterion: an invite
// concerns the Event series rather than one Occurrence, so inviting a User
// to a recurring Event produces exactly one Notification, not one per
// expansion.
func TestEventService_Create_RecurringEventWritesExactlyOneInviteNotification(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	write := createAttendeeTestWrite(f, EventWrite{AttendeeUserIDs: []int64{f.member1ID}})
	write.Rrule = "FREQ=DAILY"

	if _, err := f.events.Create(ctx, f.ownerID, "evt-1", write); err != nil {
		t.Fatalf("create: %v", err)
	}

	notifications, err := f.events.notifications.ListRecentByUser(ctx, f.member1ID, 10)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("expected exactly 1 notification for a recurring event's invite, got %+v", notifications)
	}
	if notifications[0].OccurrenceStart != nil {
		t.Fatalf("expected a nil occurrence start, got %v", notifications[0].OccurrenceStart)
	}
}

// TestEventService_Create_InvitesGroupExpandedToMembers covers #187's Group
// acceptance criterion: a Group named in AttendeeGroupIDs expands to its
// current members at create time, exactly like AddGroupAttendee.
func TestEventService_Create_InvitesGroupExpandedToMembers(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeGroupIDs: []int64{f.groupID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected 2 attendees, got %+v", attendees)
	}
}

// TestEventService_Create_DeduplicatesUserNamedIndividuallyAndViaGroup
// covers ADR-0055's dedup guarantee: a User named both explicitly and
// through a Group they belong to produces exactly one attendees row, since
// (event_id, user_id) is a primary key.
func TestEventService_Create_DeduplicatesUserNamedIndividuallyAndViaGroup(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs:  []int64{f.member1ID},
		AttendeeGroupIDs: []int64{f.groupID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected 2 attendees (member1 deduplicated), got %+v", attendees)
	}
}

// TestEventService_Create_RefusesUnknownExplicitUser covers ADR-0055's
// strict-explicit-target policy: an unknown User fails the whole create —
// no Event, no Attendees, transaction rolled back.
func TestEventService_Create_RefusesUnknownExplicitUser(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	_, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{999999},
	}))
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	if _, err := f.events.Get(ctx, f.ownerID, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected no event created, got %v", err)
	}
}

// TestEventService_Create_RefusesDisabledExplicitUser covers ADR-0055's
// strict-explicit-target policy applied to a Disabled User, mirroring
// AddAttendee's own refusal (ADR-0037).
func TestEventService_Create_RefusesDisabledExplicitUser(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	if _, err := f.users.SetDisabled(ctx, f.member1ID, true); err != nil {
		t.Fatalf("disable member1: %v", err)
	}

	_, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.member1ID},
	}))
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// TestEventService_Create_RefusesExplicitUserOutsideWorkspace covers
// ADR-0055's strict-explicit-target policy applied to a User outside the
// Calendar's own Workspace.
func TestEventService_Create_RefusesExplicitUserOutsideWorkspace(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	_, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeUserIDs: []int64{f.outsiderID},
	}))
	if !errors.Is(err, ErrAttendeeTargetNotInWorkspace) {
		t.Fatalf("expected ErrAttendeeTargetNotInWorkspace, got %v", err)
	}
}

// TestEventService_Create_RefusesUnknownExplicitGroup covers ADR-0055's
// strict policy applied to the Group id itself, distinct from lenient
// member expansion.
func TestEventService_Create_RefusesUnknownExplicitGroup(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	_, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeGroupIDs: []int64{999999},
	}))
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}

// TestEventService_Create_RefusesExplicitGroupOutsideWorkspace covers
// ADR-0055's strict policy applied to a Group belonging to another
// Workspace.
func TestEventService_Create_RefusesExplicitGroupOutsideWorkspace(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	_, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeGroupIDs: []int64{f.outsideGroupID},
	}))
	if !errors.Is(err, ErrAttendeeTargetNotInWorkspace) {
		t.Fatalf("expected ErrAttendeeTargetNotInWorkspace, got %v", err)
	}
}

// TestEventService_Create_SkipsDisabledGroupMember covers ADR-0055's
// lenient Group-expansion policy: a Disabled member inside an otherwise
// valid Group is skipped, not a create failure.
func TestEventService_Create_SkipsDisabledGroupMember(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	if _, err := f.users.SetDisabled(ctx, f.member1ID, true); err != nil {
		t.Fatalf("disable member1: %v", err)
	}

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{
		AttendeeGroupIDs: []int64{f.groupID},
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].UserID == nil || *attendees[0].UserID != f.member2ID {
		t.Fatalf("expected only member2, got %+v", attendees)
	}
}

// TestEventService_Create_WithNoAttendeesStaysUnaffected is a regression
// guard: a create carrying no Attendees at all behaves exactly as it did
// before #187 — no Workspace lookup, no Attendee rows.
func TestEventService_Create_WithNoAttendeesStaysUnaffected(t *testing.T) {
	f := newCreateAttendeeFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", createAttendeeTestWrite(f, EventWrite{}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 0 {
		t.Fatalf("expected no attendees, got %+v", attendees)
	}
}
