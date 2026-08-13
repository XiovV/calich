// event_reply_test.go covers EventService.ApplyReply (#202, ADR-0059): the
// reply poller's write path, resolving an inbound METHOD:REPLY to the right
// Attendee row and setting its Response with no acting User in view.
package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/repository"
)

// replyFixture mirrors newTestEventService but also exposes the User
// repository, since ApplyReply's User-backed path needs a second account to
// invite and reply as.
type replyFixture struct {
	events     *EventService
	users      *repository.UserRepository
	ownerID    int64
	calendarID string
}

func newReplyFixture(t *testing.T) replyFixture {
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

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	cal, err := calendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewSyncRepository(sqlDB),
		NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB)),
		users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB), repository.NewNotificationRepository(sqlDB), nil, 1000)

	return replyFixture{events: events, users: users, ownerID: owner.ID, calendarID: cal.ID}
}

func TestEventService_ApplyReply_UserBackedAttendee_SetsResponse(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()

	guest, err := f.users.Create(ctx, "Guest", "guest@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: start, End: start.Add(30 * time.Minute)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := f.events.attendees.Add(ctx, event.ID, guest.ID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	applied, err := f.events.ApplyReply(ctx, icalendar.ParsedReply{
		UID:      "evt-1",
		Attendee: "guest@example.com",
		Response: repository.ResponseAccepted,
	})
	if err != nil {
		t.Fatalf("apply reply: %v", err)
	}
	if !applied {
		t.Fatalf("expected the reply to apply")
	}

	got, err := f.events.attendees.Get(ctx, "evt-1", guest.ID)
	if err != nil {
		t.Fatalf("get attendee: %v", err)
	}
	if got.Response != repository.ResponseAccepted {
		t.Fatalf("expected response %q, got %q", repository.ResponseAccepted, got.Response)
	}
}

func TestEventService_ApplyReply_EmailShapedAttendee_SetsResponse(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: start, End: start.Add(30 * time.Minute)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := f.events.attendees.AddEmail(ctx, event.ID, "outside@example.com"); err != nil {
		t.Fatalf("add email attendee: %v", err)
	}

	applied, err := f.events.ApplyReply(ctx, icalendar.ParsedReply{
		UID:      "evt-1",
		Attendee: "Outside@Example.com", // different case: address matching must be case-insensitive
		Response: repository.ResponseDeclined,
	})
	if err != nil {
		t.Fatalf("apply reply: %v", err)
	}
	if !applied {
		t.Fatalf("expected the reply to apply")
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, "evt-1")
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].Response != repository.ResponseDeclined {
		t.Fatalf("expected the email-shaped attendee's response updated, got %+v", attendees)
	}
}

func TestEventService_ApplyReply_UnknownUID_ReturnsFalseWithoutError(t *testing.T) {
	f := newReplyFixture(t)

	applied, err := f.events.ApplyReply(context.Background(), icalendar.ParsedReply{
		UID:      "no-such-event",
		Attendee: "guest@example.com",
		Response: repository.ResponseAccepted,
	})
	if err != nil {
		t.Fatalf("expected no error for an unknown UID, got %v", err)
	}
	if applied {
		t.Fatalf("expected applied=false for an unknown UID")
	}
}

func TestEventService_ApplyReply_AddressNotAnAttendee_ReturnsFalseWithoutError(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if _, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: start, End: start.Add(30 * time.Minute)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	applied, err := f.events.ApplyReply(ctx, icalendar.ParsedReply{
		UID:      "evt-1",
		Attendee: "stranger@example.com",
		Response: repository.ResponseAccepted,
	})
	if err != nil {
		t.Fatalf("expected no error for a non-Attendee address, got %v", err)
	}
	if applied {
		t.Fatalf("expected applied=false for an address that isn't an Attendee")
	}
}

func TestEventService_ApplyReply_SameReplyTwice_StaysConsistent(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()

	guest, err := f.users.Create(ctx, "Guest", "guest@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Standup", Start: start, End: start.Add(30 * time.Minute)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := f.events.attendees.Add(ctx, event.ID, guest.ID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	reply := icalendar.ParsedReply{UID: "evt-1", Attendee: "guest@example.com", Response: repository.ResponseAccepted}
	for i := 0; i < 2; i++ {
		applied, err := f.events.ApplyReply(ctx, reply)
		if err != nil {
			t.Fatalf("apply reply #%d: %v", i, err)
		}
		if !applied {
			t.Fatalf("expected applied=true on attempt #%d", i)
		}
	}

	got, err := f.events.attendees.Get(ctx, "evt-1", guest.ID)
	if err != nil {
		t.Fatalf("get attendee: %v", err)
	}
	if got.Response != repository.ResponseAccepted {
		t.Fatalf("expected response %q after a repeated reply, got %q", repository.ResponseAccepted, got.Response)
	}
}

// TestEventService_ApplyReply_RecurringOccurrence_RoutesToOverrideRow covers
// the "a reply to one Occurrence of a recurring Event updates the right row
// rather than the whole series" AC: the Override's own Attendee row changes,
// the Master's own row for the same address does not.
func TestEventService_ApplyReply_RecurringOccurrence_RoutesToOverrideRow(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	occurrenceStart := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	master, overrides, err := f.events.PutSeries(ctx, f.ownerID, f.calendarID, "series-1", SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;COUNT=4",
		Overrides: []OverrideWrite{
			{RecurrenceID: occurrenceStart, Title: "Standup (moved)", Start: occurrenceStart.Add(time.Hour), End: occurrenceStart.Add(90 * time.Minute)},
		},
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(overrides))
	}
	override := overrides[0]

	if _, err := f.events.attendees.AddEmail(ctx, master.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee to master: %v", err)
	}
	if _, err := f.events.attendees.AddEmail(ctx, override.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee to override: %v", err)
	}

	applied, err := f.events.ApplyReply(ctx, icalendar.ParsedReply{
		UID:          "series-1",
		RecurrenceID: &occurrenceStart,
		Attendee:     "guest@example.com",
		Response:     repository.ResponseDeclined,
	})
	if err != nil {
		t.Fatalf("apply reply: %v", err)
	}
	if !applied {
		t.Fatalf("expected the reply to apply")
	}

	overrideAttendees, err := f.events.ListAttendees(ctx, f.ownerID, override.ID)
	if err != nil {
		t.Fatalf("list override attendees: %v", err)
	}
	if len(overrideAttendees) != 1 || overrideAttendees[0].Response != repository.ResponseDeclined {
		t.Fatalf("expected the Override's own row to change, got %+v", overrideAttendees)
	}

	masterAttendees, err := f.events.ListAttendees(ctx, f.ownerID, master.ID)
	if err != nil {
		t.Fatalf("list master attendees: %v", err)
	}
	if len(masterAttendees) != 1 || masterAttendees[0].Response != repository.ResponseNeedsAction {
		t.Fatalf("expected the Master's own row untouched, got %+v", masterAttendees)
	}
}

// TestEventService_ApplyReply_RecurringOccurrence_NoOverride_FallsBackToMaster
// covers an Attendee invited to a whole series answering an Occurrence that
// was never split into its own Override: there is no per-Occurrence row to
// write onto yet, so the reply lands on the Master's own row — the same
// Override-then-Master resolution GetOccurrence itself already uses.
func TestEventService_ApplyReply_RecurringOccurrence_NoOverride_FallsBackToMaster(t *testing.T) {
	f := newReplyFixture(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	occurrenceStart := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	master, _, err := f.events.PutSeries(ctx, f.ownerID, f.calendarID, "series-1", SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;COUNT=4",
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if _, err := f.events.attendees.AddEmail(ctx, master.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	applied, err := f.events.ApplyReply(ctx, icalendar.ParsedReply{
		UID:          "series-1",
		RecurrenceID: &occurrenceStart,
		Attendee:     "guest@example.com",
		Response:     repository.ResponseTentative,
	})
	if err != nil {
		t.Fatalf("apply reply: %v", err)
	}
	if !applied {
		t.Fatalf("expected the reply to apply")
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, master.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].Response != repository.ResponseTentative {
		t.Fatalf("expected the Master's own row to change, got %+v", attendees)
	}
}
