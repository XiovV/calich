package reminder

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// fakeEventLister returns a fixed set of events, standing in for the
// EventService's real (DB-backed) ListAllWithReminders in scheduler tests
// that don't need real event persistence.
type fakeEventLister struct {
	events []repository.Event
}

func (f fakeEventLister) ListAllWithReminders(context.Context) ([]repository.Event, error) {
	return f.events, nil
}

// fakeDispatcher records every DueReminder it's asked to dispatch, so a test
// can assert on what actually got delivered without a real sink.
type fakeDispatcher struct {
	dispatched []DueReminder
}

func (f *fakeDispatcher) Dispatch(_ context.Context, due DueReminder) error {
	f.dispatched = append(f.dispatched, due)
	return nil
}

// newTestLedger returns a real, DB-backed FiredReminderRepository plus a real
// Reminder id to satisfy its foreign key — exactly-once is a persistence
// guarantee, so it's tested against the real ledger, not a fake.
func newTestLedger(t *testing.T) (ledger *repository.FiredReminderRepository, reminderID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(ctx, "user-a", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	calendars := repository.NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, user.ID, "cal-1", "Personal", "peacock")
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	events := repository.NewEventRepository(sqlDB)
	if _, err := events.Create(ctx, "evt-1", user.ID, cal.ID, "Standup", at(2026, 1, 1, 9, 0), at(2026, 1, 1, 9, 30), false, "", nil, nil, nil, "", "", 0); err != nil {
		t.Fatalf("create event: %v", err)
	}

	remindersRepo := repository.NewEventReminderRepository(sqlDB)
	if err := remindersRepo.ReplaceByEventID(ctx, "evt-1", []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	byEvent, err := remindersRepo.ListByEventIDs(ctx, []string{"evt-1"})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}

	return repository.NewFiredReminderRepository(sqlDB), byEvent["evt-1"][0].ID
}

// clock is a manually-advanceable time source for deterministic scheduler tests.
type clock struct{ t time.Time }

func (c *clock) now() time.Time  { return c.t }
func (c *clock) set(t time.Time) { c.t = t }

func TestScheduler_Tick_DispatchesAReminderThatBecomesDueInTheWindow(t *testing.T) {
	ledger, reminderID := newTestLedger(t)
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: reminderID, OffsetMinutes: 10, Channel: "notification"},
		},
	}
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(fakeEventLister{events: []repository.Event{event}}, ledger, dispatcher, c.now)

	// The trigger (08:50) falls in this tick's window.
	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected 1 dispatched reminder, got %+v", dispatcher.dispatched)
	}
}

func TestScheduler_Tick_DoesNotDispatchATriggerOutsideTheWindow(t *testing.T) {
	ledger, reminderID := newTestLedger(t)
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: reminderID, OffsetMinutes: 10, Channel: "notification"},
		},
	}
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 6, 0)}
	scheduler := NewScheduler(fakeEventLister{events: []repository.Event{event}}, ledger, dispatcher, c.now)

	c.set(at(2026, 1, 1, 6, 5))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(dispatcher.dispatched) != 0 {
		t.Fatalf("expected nothing dispatched, got %+v", dispatcher.dispatched)
	}
}

// A repeated tick whose window happens to re-cover an already-fired trigger
// (e.g. the same instant appearing in two overlapping windows) must not
// redispatch — the ledger, not the scheduler's own state, is what enforces
// exactly-once.
func TestScheduler_Tick_ExactlyOnceAcrossRepeatedTicks(t *testing.T) {
	ledger, reminderID := newTestLedger(t)
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: reminderID, OffsetMinutes: 10, Channel: "notification"},
		},
	}
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(fakeEventLister{events: []repository.Event{event}}, ledger, dispatcher, c.now)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	// A second scheduler instance (simulating an overlapping/duplicate tick)
	// whose window covers the same trigger again, backed by the same ledger.
	dispatcher2 := &fakeDispatcher{}
	c2 := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler2 := NewScheduler(fakeEventLister{events: []repository.Event{event}}, ledger, dispatcher2, c2.now)
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

// Simulates a process restart: a fresh Scheduler, backed by the same
// persisted ledger, whose lastTick starts at its own construction time (not
// wherever the previous process left off) — so a trigger that fell due while
// the process was down is never fired, by design (ADR-0021).
func TestScheduler_Restart_NeverCatchesUpAMissedTrigger(t *testing.T) {
	ledger, reminderID := newTestLedger(t)
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0), // trigger at 08:50 (10 minutes before)
		End:   at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: reminderID, OffsetMinutes: 10, Channel: "notification"},
		},
	}
	dispatcher := &fakeDispatcher{}
	// The old process's last tick was well before the trigger; it then went
	// down and never ticked again.
	c := &clock{t: at(2026, 1, 1, 6, 0)}
	oldScheduler := NewScheduler(fakeEventLister{events: []repository.Event{event}}, ledger, dispatcher, c.now)
	_ = oldScheduler // downtime starts here; no further Tick call.

	// The new process starts long after the trigger passed.
	restartDispatcher := &fakeDispatcher{}
	restartClock := &clock{t: at(2026, 1, 2, 0, 0)}
	restarted := NewScheduler(fakeEventLister{events: []repository.Event{event}}, ledger, restartDispatcher, restartClock.now)

	restartClock.set(at(2026, 1, 2, 0, 1))
	if err := restarted.Tick(context.Background()); err != nil {
		t.Fatalf("tick after restart: %v", err)
	}

	if len(restartDispatcher.dispatched) != 0 {
		t.Fatalf("expected the missed-while-down trigger to not fire after restart, got %+v", restartDispatcher.dispatched)
	}
}
