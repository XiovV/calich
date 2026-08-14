package reminder

import (
	"context"
	"testing"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestDefaultReminderLedger returns a real, DB-backed FiredReminderRepository
// plus a real calendar_default_reminders row's own id and the User it
// belongs to — a default-resolved Reminder's ledger key (default_reminder_id,
// event_id, occurrence_start) is scoped to it rather than to any one Event
// (ADR-0064), unlike an explicit Reminder's own row.
func newTestDefaultReminderLedger(t *testing.T) (ledger *repository.FiredReminderRepository, defaultReminderID, userID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(ctx, "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspaces := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	calendars := repository.NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, user.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Family", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	// fired_default_reminders.event_id is a real foreign key, unlike
	// fakeEventLister's events elsewhere in this package which never touch
	// the DB — a default-resolved fire needs a persisted Event to satisfy it.
	events := repository.NewEventRepository(sqlDB)
	for _, id := range []string{"evt-1", "evt-2"} {
		if _, err := events.Create(ctx, id, &user.ID, repository.EventFields{CalendarID: cal.ID, Title: "Bin day", Start: at(2026, 1, 1, 9, 0), End: at(2026, 1, 1, 9, 30)}, 0); err != nil {
			t.Fatalf("create event %s: %v", id, err)
		}
	}

	defaults := repository.NewCalendarDefaultReminderRepository(sqlDB)
	if err := defaults.ReplaceByCalendarID(ctx, user.ID, cal.ID, false, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("set default: %v", err)
	}
	timed, _, err := defaults.ListByCalendarID(ctx, user.ID, cal.ID)
	if err != nil {
		t.Fatalf("list default: %v", err)
	}

	return repository.NewFiredReminderRepository(sqlDB), timed[0].ID, user.ID
}

// A default-resolved Reminder (repository.Reminder.DefaultReminderID set,
// ID left zero) fires through the scheduler's MarkDefaultFired ledger
// branch, and fires independently per Event it resolves onto — the same
// default applies to every Event on the Calendar with no explicit rows.
func TestScheduler_Tick_FiresADefaultResolvedReminderPerEventIndependently(t *testing.T) {
	ledger, defaultReminderID, userID := newTestDefaultReminderLedger(t)

	makeEvent := func(id string) repository.EventWithOwner {
		return repository.EventWithOwner{
			Event: repository.Event{ID: id, Start: at(2026, 1, 1, 9, 0), End: at(2026, 1, 1, 9, 30)},
			RemindersByUser: map[int64][]repository.Reminder{
				userID: {{DefaultReminderID: defaultReminderID, OffsetMinutes: 10, Channel: "notification"}},
			},
		}
	}

	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(
		fakeEventLister{events: []repository.EventWithOwner{makeEvent("evt-1"), makeEvent("evt-2")}},
		ledger, dispatcher, c.now,
	)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(dispatcher.dispatched) != 2 {
		t.Fatalf("expected the same default to fire once per Event, got %+v", dispatcher.dispatched)
	}
	for _, d := range dispatcher.dispatched {
		if d.ReminderID != 0 {
			t.Fatalf("expected a default-resolved fire to carry no explicit ReminderID, got %+v", d)
		}
		if d.DefaultReminderID != defaultReminderID {
			t.Fatalf("expected DefaultReminderID %d, got %+v", defaultReminderID, d)
		}
	}
}

// A repeated tick covering the same default-resolved trigger on the same
// Event must not redispatch — MarkDefaultFired's own ledger enforces
// exactly-once exactly like MarkFired's does for an explicit Reminder.
func TestScheduler_Tick_DefaultResolvedReminderExactlyOnce(t *testing.T) {
	ledger, defaultReminderID, userID := newTestDefaultReminderLedger(t)
	event := repository.EventWithOwner{
		Event: repository.Event{ID: "evt-1", Start: at(2026, 1, 1, 9, 0), End: at(2026, 1, 1, 9, 30)},
		RemindersByUser: map[int64][]repository.Reminder{
			userID: {{DefaultReminderID: defaultReminderID, OffsetMinutes: 10, Channel: "notification"}},
		},
	}

	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{event}}, ledger, dispatcher, c.now)
	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	dispatcher2 := &fakeDispatcher{}
	c2 := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler2 := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{event}}, ledger, dispatcher2, c2.now)
	c2.set(at(2026, 1, 1, 8, 55))
	if err := scheduler2.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected the first tick to dispatch once, got %+v", dispatcher.dispatched)
	}
	if len(dispatcher2.dispatched) != 0 {
		t.Fatalf("expected the repeated tick to dispatch nothing (already fired), got %+v", dispatcher2.dispatched)
	}
}
