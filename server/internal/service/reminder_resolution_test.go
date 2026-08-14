package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestReminderResolutionFixture returns an EventService plus an owner and
// a member sharing one Calendar (member has Editor Access via a Share) —
// the two-User setup ADR-0064's Default reminders resolution tests need.
func newTestReminderResolutionFixture(t *testing.T) (svc *EventService, ownerID, memberID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	ctx := context.Background()
	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
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

	calendarService := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarDefaultReminderRepository(sqlDB), repository.NewEventReminderExplicitRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	if _, _, err := calendarService.Share(ctx, owner.ID, cal.ID, member.Email, repository.RoleEditor); err != nil {
		t.Fatalf("share calendar with member: %v", err)
	}

	svc = NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewCalendarDefaultReminderRepository(sqlDB), repository.NewEventReminderExplicitRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB), repository.NewNotificationRepository(sqlDB), nil, 1000)

	return svc, owner.ID, member.ID, cal.ID
}

func TestGetReminders_ResolvesCalendarDefaultWhenNoneOfOwn(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].OffsetMinutes != 30 || reminders[0].DefaultReminderID == 0 {
		t.Fatalf("expected one resolved default reminder, got %+v", reminders)
	}

	// The owner set no default of their own, so they get nothing.
	ownerReminders, err := svc.GetReminders(ctx, ownerID, event.ID)
	if err != nil {
		t.Fatalf("get owner reminders: %v", err)
	}
	if len(ownerReminders) != 0 {
		t.Fatalf("expected owner to have no reminders, got %+v", ownerReminders)
	}
}

func TestSetReminders_ExplicitEmptyOptsOutOfDefault(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	// member explicitly saves an empty list — "no Reminders on this one
	// Event" (ADR-0064) — which must survive as explicit-empty, not fall
	// back to their default.
	if _, err := svc.SetReminders(ctx, memberID, event.ID, nil); err != nil {
		t.Fatalf("set reminders empty: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected explicit-empty to suppress the default, got %+v", reminders)
	}
}

func TestSetReminders_ExplicitNonEmptyOverridesDefault(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	if _, err := svc.SetReminders(ctx, memberID, event.ID, []repository.Reminder{{OffsetMinutes: 5, Channel: "email"}}); err != nil {
		t.Fatalf("set reminders: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].OffsetMinutes != 5 || reminders[0].Channel != "email" || reminders[0].DefaultReminderID != 0 {
		t.Fatalf("expected the explicit row alone, got %+v", reminders)
	}
}

func TestGetReminders_AllDayEventResolvesAllDayDefault(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set timed default: %v", err)
	}
	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, true, []repository.Reminder{{OffsetMinutes: 540, Channel: "notification"}}); err != nil {
		t.Fatalf("set all-day default: %v", err)
	}

	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Bin day", Start: day, End: day.AddDate(0, 0, 1), AllDay: true})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].OffsetMinutes != 540 {
		t.Fatalf("expected the all-day default (540), got %+v", reminders)
	}
}

func TestCreate_ResolvesCreatorsOwnDefault(t *testing.T) {
	svc, ownerID, _, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, ownerID, calendarID, false, []repository.Reminder{{OffsetMinutes: 15, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	if len(event.Reminders) != 1 || event.Reminders[0].OffsetMinutes != 15 {
		t.Fatalf("expected the creator's default pre-resolved on create, got %+v", event.Reminders)
	}
}

func TestListAllWithReminders_ResolvesDefaultsForFiring(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	events, err := svc.ListAllWithReminders(ctx)
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}

	var found *repository.EventWithOwner
	for i := range events {
		if events[i].ID == event.ID {
			found = &events[i]
		}
	}
	if found == nil {
		t.Fatalf("expected event %s to carry a resolved default, events=%+v", event.ID, events)
	}
	memberReminders := found.RemindersByUser[memberID]
	if len(memberReminders) != 1 || memberReminders[0].OffsetMinutes != 10 || memberReminders[0].DefaultReminderID == 0 {
		t.Fatalf("expected member's resolved default, got %+v", memberReminders)
	}
	if len(found.RemindersByUser[ownerID]) != 0 {
		t.Fatalf("owner set no default and no explicit rows, expected none, got %+v", found.RemindersByUser[ownerID])
	}
}

func TestListAllWithReminders_ExplicitEmptyOptsOutOfFiring(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := svc.SetReminders(ctx, memberID, event.ID, nil); err != nil {
		t.Fatalf("opt out: %v", err)
	}

	events, err := svc.ListAllWithReminders(ctx)
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}
	for i := range events {
		if events[i].ID == event.ID {
			if len(events[i].RemindersByUser[memberID]) != 0 {
				t.Fatalf("expected member's explicit opt-out to suppress firing, got %+v", events[i].RemindersByUser[memberID])
			}
		}
	}
}

// TestGetSeries_DoesNotResolveDefaults confirms the CalDAV/ICS-export read
// path (GetSeries -> hydrateSeries -> attachViewerReminders) stays scoped to
// explicit Reminders only — a default that only resolves does not yet
// appear as a VALARM anywhere (#213's job, not this one's).
func TestGetSeries_DoesNotResolveDefaults(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	master, _, err := svc.GetSeries(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if len(master.Reminders) != 0 {
		t.Fatalf("expected GetSeries to stay explicit-only (no resolved default), got %+v", master.Reminders)
	}

	// The live web-app path (Get), by contrast, does resolve it.
	webEvent, err := svc.Get(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(webEvent.Reminders) != 1 {
		t.Fatalf("expected Get to resolve the default, got %+v", webEvent.Reminders)
	}
}

// TestPutSeries_EmptyRemindersMarksExplicit confirms a CalDAV PUT clearing a
// User's VALARMs (an empty Reminders list) marks the same explicit-empty
// opt-out the web app's dedicated SetReminders does (ADR-0064) — "A PUT's
// VALARMs write the PUTting User's Reminders" means the Calendar default
// must not silently reassert on the next resolved (web-app) read.
func TestPutSeries_EmptyRemindersMarksExplicit(t *testing.T) {
	svc, ownerID, _, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, ownerID, calendarID, false, []repository.Reminder{{OffsetMinutes: 30, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	master, _, err := svc.PutSeries(ctx, ownerID, calendarID, "evt-1", SeriesWrite{
		Title: "Standup", Start: start, End: start.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}

	reminders, err := svc.GetReminders(ctx, ownerID, master.ID)
	if err != nil {
		t.Fatalf("get reminders: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected the PUT's empty VALARM set to opt out of the default, got %+v", reminders)
	}
}

func TestRevokeShare_ClearsDefaultRemindersAndExplicitMarkers(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := svc.SetReminders(ctx, memberID, event.ID, nil); err != nil {
		t.Fatalf("opt out: %v", err)
	}

	if err := svc.calendars.RevokeShare(ctx, ownerID, calendarID, memberID); err != nil {
		t.Fatalf("revoke share: %v", err)
	}

	timed, allDay, err := svc.calendars.GetDefaultReminders(ctx, memberID, calendarID)
	if err == nil {
		t.Fatalf("expected member's default reminders lookup to fail once they've lost Access, got timed=%+v allDay=%+v", timed, allDay)
	}

	// Re-share and confirm the default is gone (not silently reactivated)
	// and the old explicit-empty marker no longer suppresses a fresh
	// default — the member simply has neither anymore.
	if _, _, err := svc.calendars.Share(ctx, ownerID, calendarID, "member@example.com", repository.RoleEditor); err != nil {
		t.Fatalf("re-share: %v", err)
	}
	reminders, err := svc.GetReminders(ctx, memberID, event.ID)
	if err != nil {
		t.Fatalf("get reminders after re-share: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected no reminders after re-share (default was cleared), got %+v", reminders)
	}
}
