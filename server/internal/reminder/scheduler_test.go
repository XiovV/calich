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
	events []repository.EventWithOwner
}

func (f fakeEventLister) ListAllWithReminders(context.Context) ([]repository.EventWithOwner, error) {
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
	cal, err := calendars.Create(ctx, user.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	events := repository.NewEventRepository(sqlDB)
	if _, err := events.Create(ctx, "evt-1", &user.ID, repository.EventFields{CalendarID: cal.ID, Title: "Standup", Start: at(2026, 1, 1, 9, 0), End: at(2026, 1, 1, 9, 30)}, 0); err != nil {
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

// newTestLedgerWithSecondUser is newTestLedger plus a second real User in
// the same database, for tests proving a Reminder fans out to every
// recipient and fires independently per User (ADR-0036).
func newTestLedgerWithSecondUser(t *testing.T) (ledger *repository.FiredReminderRepository, reminderID, ownerID, otherUserID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(ctx, "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	workspaces := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	calendars := repository.NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Family", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	shares := repository.NewCalendarShareRepository(sqlDB)
	if _, err := shares.Upsert(ctx, cal.ID, other.ID, repository.RoleEditor); err != nil {
		t.Fatalf("share calendar: %v", err)
	}
	events := repository.NewEventRepository(sqlDB)
	if _, err := events.Create(ctx, "evt-1", &owner.ID, repository.EventFields{CalendarID: cal.ID, Title: "Bin day", Start: at(2026, 1, 1, 9, 0), End: at(2026, 1, 1, 9, 30)}, 0); err != nil {
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

	return repository.NewFiredReminderRepository(sqlDB), byEvent["evt-1"][0].ID, owner.ID, other.ID
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
	scheduler := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{withOwner(event)}}, ledger, dispatcher, c.now)

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
	scheduler := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{withOwner(event)}}, ledger, dispatcher, c.now)

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
	scheduler := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{withOwner(event)}}, ledger, dispatcher, c.now)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	// A second scheduler instance (simulating an overlapping/duplicate tick)
	// whose window covers the same trigger again, backed by the same ledger.
	dispatcher2 := &fakeDispatcher{}
	c2 := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler2 := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{withOwner(event)}}, ledger, dispatcher2, c2.now)
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
	oldScheduler := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{withOwner(event)}}, ledger, dispatcher, c.now)
	_ = oldScheduler // downtime starts here; no further Tick call.

	// The new process starts long after the trigger passed.
	restartDispatcher := &fakeDispatcher{}
	restartClock := &clock{t: at(2026, 1, 2, 0, 0)}
	restarted := NewScheduler(fakeEventLister{events: []repository.EventWithOwner{withOwner(event)}}, ledger, restartDispatcher, restartClock.now)

	restartClock.set(at(2026, 1, 2, 0, 1))
	if err := restarted.Tick(context.Background()); err != nil {
		t.Fatalf("tick after restart: %v", err)
	}

	if len(restartDispatcher.dispatched) != 0 {
		t.Fatalf("expected the missed-while-down trigger to not fire after restart, got %+v", restartDispatcher.dispatched)
	}
}

// A Reminder on a shared Calendar's Event fires for every User with Access
// to it — the Owner and every Editor and Viewer alike, not just whoever
// owns the Calendar (ADR-0036).
func TestScheduler_Tick_FiresForEveryUserWithAccessToTheCalendar(t *testing.T) {
	ledger, reminderID, ownerID, otherUserID := newTestLedgerWithSecondUser(t)
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
	scheduler := NewScheduler(
		fakeEventLister{events: []repository.EventWithOwner{withRecipients(event, ownerID, ownerID, otherUserID)}},
		ledger, dispatcher, c.now,
	)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(dispatcher.dispatched) != 2 {
		t.Fatalf("expected 2 dispatched reminders (one per recipient), got %+v", dispatcher.dispatched)
	}
	gotUsers := map[int64]bool{dispatcher.dispatched[0].UserID: true, dispatcher.dispatched[1].UserID: true}
	if !gotUsers[ownerID] || !gotUsers[otherUserID] {
		t.Fatalf("expected one dispatch per recipient (%d, %d), got %+v", ownerID, otherUserID, dispatcher.dispatched)
	}
}

// The ledger's per-User uniqueness (ADR-0036) means a repeated tick
// suppresses a fire for one recipient without suppressing the other's — the
// ledger, not the scheduler, decides exactly-once, and it decides it per
// User.
func TestScheduler_Tick_ExactlyOncePerUser_OneRecipientAlreadyFiredDoesNotSuppressTheOther(t *testing.T) {
	ledger, reminderID, ownerID, otherUserID := newTestLedgerWithSecondUser(t)
	occurrenceStart := at(2026, 1, 1, 9, 0)

	// The owner already fired on some earlier tick.
	if _, err := ledger.MarkFired(context.Background(), reminderID, ownerID, occurrenceStart, at(2026, 1, 1, 8, 55)); err != nil {
		t.Fatalf("pre-mark owner fired: %v", err)
	}

	event := repository.Event{
		ID:    "evt-1",
		Start: occurrenceStart,
		End:   at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: reminderID, OffsetMinutes: 10, Channel: "notification"},
		},
	}
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(
		fakeEventLister{events: []repository.EventWithOwner{withRecipients(event, ownerID, ownerID, otherUserID)}},
		ledger, dispatcher, c.now,
	)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected only the other recipient to dispatch, got %+v", dispatcher.dispatched)
	}
	if dispatcher.dispatched[0].UserID != otherUserID {
		t.Fatalf("expected the dispatched reminder to be for user %d, got %d", otherUserID, dispatcher.dispatched[0].UserID)
	}
}
