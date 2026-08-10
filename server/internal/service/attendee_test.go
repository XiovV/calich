package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestAttendeeService returns an EventService plus the repositories and
// ids the Attendee tests need: an owner (the Calendar's Owner and Event's
// organizer), a plain Workspace Member, an outsider who belongs to no
// Workspace, and a Calendar owned by owner inside their shared Workspace.
func newTestAttendeeService(t *testing.T) (svc *EventService, workspaceRepo *repository.WorkspaceRepository, users *repository.UserRepository, ownerID, memberID, outsiderID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users = repository.NewUserRepository(sqlDB)
	ctx := context.Background()

	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	outsider, err := users.Create(ctx, "outsider", "outsider@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create outsider: %v", err)
	}

	workspaceRepo = repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, member.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add plain member: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	cal, err := calendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	calendarService := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	svc = NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB))

	return svc, workspaceRepo, users, owner.ID, member.ID, outsider.ID, cal.ID
}

func createTestEvent(t *testing.T, svc *EventService, ownerID int64, calendarID, id string) repository.Event {
	t.Helper()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	event, err := svc.Create(context.Background(), ownerID, id, EventWrite{CalendarID: calendarID, Title: "Discuss tech stack", Start: start, End: end})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	return event
}

func TestEventService_AddAttendee_GrantsVisibilityWithNoCalendarAccess(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	// The invited member has no Share on the Calendar at all.
	if _, err := svc.Get(ctx, memberID, event.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected member to have no visibility before invite, got %v", err)
	}

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	got, err := svc.Get(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("expected attendee to see event, got %v", err)
	}
	if got.ID != event.ID {
		t.Fatalf("expected event %q, got %q", event.ID, got.ID)
	}

	// The invite grants visibility to this one Event only — the member still
	// has no Access to the Calendar itself.
	if _, err := svc.calendars.Get(ctx, memberID, calendarID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected member to have no Calendar Access, got %v", err)
	}
}

func TestEventService_AddAttendee_AppearsInVisibleEventsList(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	events, err := svc.List(ctx, memberID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("expected [%s], got %+v", event.ID, events)
	}
}

func TestEventService_AddAttendee_ListMergesCalendarAccessAndAttendeeEvents(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()

	// memberID has no Access to calendarID at all, so an Event on it they're
	// not an Attendee of stays invisible to them, while one they are an
	// Attendee of is visible — List's union, not a blanket grant.
	visible := createTestEvent(t, svc, ownerID, calendarID, "evt-visible")
	_ = createTestEvent(t, svc, ownerID, calendarID, "evt-hidden")

	if _, err := svc.AddAttendee(ctx, ownerID, visible.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	events, err := svc.List(ctx, memberID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 || events[0].ID != visible.ID {
		t.Fatalf("expected only [%s], got %+v", visible.ID, events)
	}
}

func TestEventService_AddAttendee_RefusesTargetOutsideWorkspace(t *testing.T) {
	svc, _, _, ownerID, _, outsiderID, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	_, err := svc.AddAttendee(ctx, ownerID, event.ID, outsiderID)
	if !errors.Is(err, ErrAttendeeTargetNotInWorkspace) {
		t.Fatalf("expected ErrAttendeeTargetNotInWorkspace, got %v", err)
	}
}

func TestEventService_AddAttendee_RefusesNonEditorCaller(t *testing.T) {
	svc, _, _, ownerID, memberID, outsiderID, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	// memberID has no Access to the Calendar, so they may not invite anyone
	// to its Events.
	_, err := svc.AddAttendee(ctx, memberID, event.ID, outsiderID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_AddAttendee_RefusesDisabledUser(t *testing.T) {
	svc, _, users, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := users.SetDisabled(ctx, memberID, true); err != nil {
		t.Fatalf("disable member: %v", err)
	}

	_, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestEventService_AddAttendee_AlreadyAttendeeRefused(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	_, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID)
	if !errors.Is(err, repository.ErrAlreadyAttendee) {
		t.Fatalf("expected ErrAlreadyAttendee, got %v", err)
	}
}

func TestEventService_RemoveAttendee_RevokesVisibility(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if _, err := svc.Get(ctx, memberID, event.ID); err != nil {
		t.Fatalf("expected visibility after invite: %v", err)
	}

	if err := svc.RemoveAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("remove attendee: %v", err)
	}

	if _, err := svc.Get(ctx, memberID, event.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected visibility revoked, got %v", err)
	}

	events, err := svc.List(ctx, memberID, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no visible events, got %+v", events)
	}
}

func TestEventService_RemoveAttendee_RefusesNonEditorCaller(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	// The Attendee themselves has no Editor Access, so they may not remove
	// their own invite through this path.
	if err := svc.RemoveAttendee(ctx, memberID, event.ID, memberID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_SetResponse_DefaultsToNeedsAction(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	attendee, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID)
	if err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if attendee.Response != repository.ResponseNeedsAction {
		t.Fatalf("expected needs-action, got %q", attendee.Response)
	}
}

func TestEventService_SetResponse_EachTransition(t *testing.T) {
	for _, response := range []string{repository.ResponseAccepted, repository.ResponseDeclined, repository.ResponseTentative, repository.ResponseNeedsAction} {
		t.Run(response, func(t *testing.T) {
			svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
			ctx := context.Background()
			event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

			if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
				t.Fatalf("add attendee: %v", err)
			}

			attendee, err := svc.SetResponse(ctx, memberID, event.ID, response)
			if err != nil {
				t.Fatalf("set response: %v", err)
			}
			if attendee.Response != response {
				t.Fatalf("expected %q, got %q", response, attendee.Response)
			}
		})
	}
}

func TestEventService_SetResponse_RefusesInvalidResponse(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	_, err := svc.SetResponse(ctx, memberID, event.ID, "maybe")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestEventService_SetResponse_OrganizerCannotSetAnothersResponse(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	// The organizer is not themselves an Attendee of their own Event (no
	// attendees row), so calling SetResponse as ownerID can only ever touch
	// ownerID's own (nonexistent) row — never memberID's.
	_, err := svc.SetResponse(ctx, ownerID, event.ID, repository.ResponseAccepted)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	attendee, err := svc.attendees.Get(ctx, event.ID, memberID)
	if err != nil {
		t.Fatalf("get attendee: %v", err)
	}
	if attendee.Response != repository.ResponseNeedsAction {
		t.Fatalf("expected member's response untouched, got %q", attendee.Response)
	}
}

func TestEventService_SetResponse_RefusesNonAttendee(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	_, err := svc.SetResponse(ctx, memberID, event.ID, repository.ResponseAccepted)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEventService_ListAttendees_VisibleToAttendeeWithNoCalendarAccess(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	attendees, err := svc.ListAttendees(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].UserID != memberID {
		t.Fatalf("expected [%d], got %+v", memberID, attendees)
	}
	if attendees[0].Name != "member" {
		t.Fatalf("expected name %q, got %q", "member", attendees[0].Name)
	}
}

func TestEventService_ListAttendees_RefusesCallerWithNoVisibility(t *testing.T) {
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	// memberID is a fellow Workspace Member but has neither Calendar Access
	// nor an Attendee invite of their own, so they may not list eventID's
	// Attendees.
	_, err := svc.ListAttendees(ctx, memberID, event.ID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
