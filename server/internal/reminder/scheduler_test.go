package reminder

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// fakeEventLister returns a fixed set of events and their resolution, standing
// in for the EventService's real (DB-backed) ListAllWithReminders in scheduler
// tests that don't need real event persistence.
type fakeEventLister struct {
	events   []repository.Event
	resolved repository.ResolvedReminders
}

func (f fakeEventLister) ListAllWithReminders(context.Context) ([]repository.Event, repository.ResolvedReminders, error) {
	return f.events, f.resolved, nil
}

// listing is the read a scheduler test hands the engine: one Event, plus the
// resolution naming who fires which Reminders on it (ADR-0064).
func listing(event repository.Event, byUser map[int64][]repository.Reminder) fakeEventLister {
	return fakeEventLister{events: []repository.Event{event}, resolved: repository.ResolvedReminders{event.ID: byUser}}
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
	if err := remindersRepo.ReplaceByEventID(ctx, user.ID, "evt-1", []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id: %v", err)
	}
	return repository.NewFiredReminderRepository(sqlDB), firstReminderID(t, remindersRepo, user.ID, "evt-1")
}

// newTestLedgerWithSecondUser is newTestLedger plus a second real User in
// the same database, each with their own Reminder row on the same Event —
// for tests proving each User's own Reminder fires independently, with no
// fan-out from one row to the other (ADR-0064).
func newTestLedgerWithSecondUser(t *testing.T) (ledger *repository.FiredReminderRepository, ownerReminderID, otherReminderID, ownerID, otherUserID int64) {
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
	if err := remindersRepo.ReplaceByEventID(ctx, owner.ID, "evt-1", []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id (owner): %v", err)
	}
	if err := remindersRepo.ReplaceByEventID(ctx, other.ID, "evt-1", []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace by event id (other): %v", err)
	}
	return repository.NewFiredReminderRepository(sqlDB), firstReminderID(t, remindersRepo, owner.ID, "evt-1"), firstReminderID(t, remindersRepo, other.ID, "evt-1"), owner.ID, other.ID
}

// firstReminderID reads back the id of userID's own single Reminder row on
// eventID — the fired ledger's foreign key needs a real one.
func firstReminderID(t *testing.T, reminders *repository.EventReminderRepository, userID int64, eventID string) int64 {
	t.Helper()

	byEventUser, err := reminders.ListByEventIDs(context.Background(), []string{eventID}, []int64{userID})
	if err != nil {
		t.Fatalf("list by event ids: %v", err)
	}
	rows := byEventUser[eventID][userID]
	if len(rows) == 0 {
		t.Fatalf("expected a Reminder row for user %d on %s", userID, eventID)
	}
	return rows[0].ID
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
	}
	reminders := ownerOf(repository.Reminder{ID: reminderID, OffsetMinutes: 10, Channel: "notification"})
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(listing(event, reminders), ledger, dispatcher, c.now)

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
	}
	reminders := ownerOf(repository.Reminder{ID: reminderID, OffsetMinutes: 10, Channel: "notification"})
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 6, 0)}
	scheduler := NewScheduler(listing(event, reminders), ledger, dispatcher, c.now)

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
	}
	reminders := ownerOf(repository.Reminder{ID: reminderID, OffsetMinutes: 10, Channel: "notification"})
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(listing(event, reminders), ledger, dispatcher, c.now)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	// A second scheduler instance (simulating an overlapping/duplicate tick)
	// whose window covers the same trigger again, backed by the same ledger.
	dispatcher2 := &fakeDispatcher{}
	c2 := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler2 := NewScheduler(listing(event, reminders), ledger, dispatcher2, c2.now)
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
	}
	reminders := ownerOf(repository.Reminder{ID: reminderID, OffsetMinutes: 10, Channel: "notification"})
	dispatcher := &fakeDispatcher{}
	// The old process's last tick was well before the trigger; it then went
	// down and never ticked again.
	c := &clock{t: at(2026, 1, 1, 6, 0)}
	oldScheduler := NewScheduler(listing(event, reminders), ledger, dispatcher, c.now)
	_ = oldScheduler // downtime starts here; no further Tick call.

	// The new process starts long after the trigger passed.
	restartDispatcher := &fakeDispatcher{}
	restartClock := &clock{t: at(2026, 1, 2, 0, 0)}
	restarted := NewScheduler(listing(event, reminders), ledger, restartDispatcher, restartClock.now)

	restartClock.set(at(2026, 1, 2, 0, 1))
	if err := restarted.Tick(context.Background()); err != nil {
		t.Fatalf("tick after restart: %v", err)
	}

	if len(restartDispatcher.dispatched) != 0 {
		t.Fatalf("expected the missed-while-down trigger to not fire after restart, got %+v", restartDispatcher.dispatched)
	}
}

// Two Users on the same Event — each with their own Reminder row — each
// fire their own, independently: no fan-out from one row to the other
// (ADR-0064).
func TestScheduler_Tick_FiresEachUsersOwnReminderIndependently(t *testing.T) {
	ledger, ownerReminderID, otherReminderID, ownerID, otherUserID := newTestLedgerWithSecondUser(t)
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
	}
	byUser := map[int64][]repository.Reminder{
		ownerID:     {{ID: ownerReminderID, OffsetMinutes: 10, Channel: "notification"}},
		otherUserID: {{ID: otherReminderID, OffsetMinutes: 10, Channel: "notification"}},
	}
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(
		listing(event, byUser),
		ledger, dispatcher, c.now,
	)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(dispatcher.dispatched) != 2 {
		t.Fatalf("expected 2 dispatched reminders (one per User's own row), got %+v", dispatcher.dispatched)
	}
	gotUsers := map[int64]bool{dispatcher.dispatched[0].UserID: true, dispatcher.dispatched[1].UserID: true}
	if !gotUsers[ownerID] || !gotUsers[otherUserID] {
		t.Fatalf("expected one dispatch per User (%d, %d), got %+v", ownerID, otherUserID, dispatcher.dispatched)
	}
}

// The ledger's per-Reminder uniqueness means a repeated tick suppresses a
// fire for one User's own Reminder without suppressing the other's — the
// ledger, not the scheduler, decides exactly-once, and a Reminder id already
// implies its own User (ADR-0064).
func TestScheduler_Tick_ExactlyOncePerUser_OneUsersReminderAlreadyFiredDoesNotSuppressTheOthers(t *testing.T) {
	ledger, ownerReminderID, otherReminderID, ownerID, otherUserID := newTestLedgerWithSecondUser(t)
	occurrenceStart := at(2026, 1, 1, 9, 0)

	// The owner's own Reminder already fired on some earlier tick.
	if _, err := ledger.MarkFired(context.Background(), ownerReminderID, ownerID, occurrenceStart, at(2026, 1, 1, 8, 55)); err != nil {
		t.Fatalf("pre-mark owner fired: %v", err)
	}

	event := repository.Event{
		ID:    "evt-1",
		Start: occurrenceStart,
		End:   at(2026, 1, 1, 9, 30),
	}
	byUser := map[int64][]repository.Reminder{
		ownerID:     {{ID: ownerReminderID, OffsetMinutes: 10, Channel: "notification"}},
		otherUserID: {{ID: otherReminderID, OffsetMinutes: 10, Channel: "notification"}},
	}
	dispatcher := &fakeDispatcher{}
	c := &clock{t: at(2026, 1, 1, 8, 40)}
	scheduler := NewScheduler(
		listing(event, byUser),
		ledger, dispatcher, c.now,
	)

	c.set(at(2026, 1, 1, 8, 55))
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected only the other User's Reminder to dispatch, got %+v", dispatcher.dispatched)
	}
	if dispatcher.dispatched[0].UserID != otherUserID {
		t.Fatalf("expected the dispatched reminder to be for user %d, got %d", otherUserID, dispatcher.dispatched[0].UserID)
	}
}
