package reminder

import (
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

func at(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}

func TestDue_NonRecurringEvent_FiresWhenItsTriggerFallsInTheWindow(t *testing.T) {
	event := repository.Event{
		ID:     "evt-1",
		UserID: 1,
		Title:  "Standup",
		Start:  at(2026, 1, 1, 9, 0),
		End:    at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 10, Channel: "notification"},
		},
	}

	// The trigger is 08:50 (10 minutes before the 09:00 start).
	due, err := Due(event, at(2026, 1, 1, 8, 45), at(2026, 1, 1, 8, 55))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due reminder, got %+v", due)
	}
	if due[0].ReminderID != 100 || !due[0].OccurrenceStart.Equal(event.Start) {
		t.Fatalf("unexpected due reminder: %+v", due[0])
	}
	if due[0].Title != "Standup" {
		t.Fatalf("expected due reminder to carry the Event's title, got %+v", due[0])
	}
}

func TestDue_NonRecurringEvent_DoesNotFireOutsideTheWindow(t *testing.T) {
	event := repository.Event{
		ID:     "evt-1",
		Start:  at(2026, 1, 1, 9, 0),
		End:    at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 10, Channel: "notification"},
		},
	}

	due, err := Due(event, at(2026, 1, 1, 8, 0), at(2026, 1, 1, 8, 30))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected no due reminders, got %+v", due)
	}
}

// The window is a half-open (from, to] — a trigger that lands exactly on
// `from` is not due until the next tick; one landing exactly on `to` is.
func TestDue_WindowIsExclusiveOfFromAndInclusiveOfTo(t *testing.T) {
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 0, Channel: "notification"},
		},
	}

	onFrom, err := Due(event, at(2026, 1, 1, 9, 0), at(2026, 1, 1, 9, 5))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(onFrom) != 0 {
		t.Fatalf("expected a trigger exactly on `from` to not be due yet, got %+v", onFrom)
	}

	onTo, err := Due(event, at(2026, 1, 1, 8, 55), at(2026, 1, 1, 9, 0))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(onTo) != 1 {
		t.Fatalf("expected a trigger exactly on `to` to be due, got %+v", onTo)
	}
}

func TestDue_RecurringEvent_ExpandsEachOccurrencesTrigger(t *testing.T) {
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
		Rrule: "FREQ=DAILY",
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 10, Channel: "notification"},
		},
	}

	// The third daily occurrence (Jan 3, 09:00) triggers at 08:50.
	due, err := Due(event, at(2026, 1, 3, 8, 45), at(2026, 1, 3, 8, 55))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || !due[0].OccurrenceStart.Equal(at(2026, 1, 3, 9, 0)) {
		t.Fatalf("unexpected due reminders: %+v", due)
	}
}

func TestDue_RecurringEvent_SkipsAnExdatedOccurrence(t *testing.T) {
	event := repository.Event{
		ID:      "evt-1",
		Start:   at(2026, 1, 1, 9, 0),
		End:     at(2026, 1, 1, 9, 30),
		Rrule:   "FREQ=DAILY",
		Exdates: []time.Time{at(2026, 1, 3, 9, 0)},
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 10, Channel: "notification"},
		},
	}

	due, err := Due(event, at(2026, 1, 3, 8, 45), at(2026, 1, 3, 8, 55))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected the exdated occurrence's reminder to be suppressed, got %+v", due)
	}
}

func TestDue_AllDayEvent_AnchorsAt9AMOnTheOccurrenceDateInsteadOfMidnight(t *testing.T) {
	event := repository.Event{
		ID:     "evt-1",
		Start:  at(2026, 1, 1, 0, 0), // stored midnight, per ADR-0017.
		End:    at(2026, 1, 2, 0, 0),
		AllDay: true,
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 0, Channel: "notification"}, // "at time of event"
		},
	}

	// Nothing fires at midnight...
	atMidnight, err := Due(event, at(2025, 12, 31, 23, 55), at(2026, 1, 1, 0, 0))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(atMidnight) != 0 {
		t.Fatalf("expected no all-day reminder to fire at midnight, got %+v", atMidnight)
	}

	// ...it fires at 09:00 instead.
	at9am, err := Due(event, at(2026, 1, 1, 8, 55), at(2026, 1, 1, 9, 0))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(at9am) != 1 {
		t.Fatalf("expected the all-day reminder to fire at 09:00, got %+v", at9am)
	}
}

func TestDue_AllDayEvent_OneDayBeforeFiresTheMorningBefore(t *testing.T) {
	event := repository.Event{
		ID:     "evt-1",
		Start:  at(2026, 1, 2, 0, 0),
		End:    at(2026, 1, 3, 0, 0),
		AllDay: true,
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 1440, Channel: "notification"}, // 1 day before
		},
	}

	// 09:00 on Jan 2 minus 1 day = 09:00 on Jan 1.
	due, err := Due(event, at(2026, 1, 1, 8, 55), at(2026, 1, 1, 9, 0))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected the 1-day-before reminder to fire the morning before, got %+v", due)
	}
}

func TestDue_MultipleRemindersOnOneEvent_EachEvaluatedIndependently(t *testing.T) {
	event := repository.Event{
		ID:    "evt-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 10, Channel: "notification"},
			{ID: 101, OffsetMinutes: 1440, Channel: "email"},
		},
	}

	// Only the 1-day-before (email) reminder's trigger falls in this window.
	due, err := Due(event, at(2025, 12, 31, 8, 55), at(2025, 12, 31, 9, 0))
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].ReminderID != 101 || due[0].Channel != "email" {
		t.Fatalf("unexpected due reminders: %+v", due)
	}
}

func TestDue_ZonedRecurringEvent_KeepsWallClockAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata available: %v", err)
	}
	tzid := "Europe/Berlin"

	// Daily 10:00 Berlin wall-clock, spanning the EU spring-forward
	// (2026-03-29, clocks jump from 02:00 to 03:00 CEST).
	start := time.Date(2026, 3, 1, 10, 0, 0, 0, loc).UTC()
	event := repository.Event{
		ID:    "evt-1",
		Start: start,
		End:   start.Add(30 * time.Minute),
		Rrule: "FREQ=DAILY",
		Tzid:  &tzid,
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 0, Channel: "notification"},
		},
	}

	// March 30's occurrence should still trigger at 10:00 Berlin time (which
	// is 08:00 UTC post-transition, not 09:00 as it would be pre-transition).
	postTransition := time.Date(2026, 3, 30, 10, 0, 0, 0, loc)
	due, err := Due(event, postTransition.Add(-5*time.Minute), postTransition)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected the post-DST occurrence to still trigger at 10:00 Berlin wall-clock, got %+v", due)
	}
}

// An Override never adds an Exdate to its Master (unlike an Exception), so
// DueAll must exclude the overridden slot from the Master's own expansion
// itself — otherwise the Master would fire its stale Reminder for that slot
// alongside the Override's own (ADR-0016, ADR-0020).
func TestDueAll_OverriddenOccurrence_FiresOnlyTheOverridesOwnReminder(t *testing.T) {
	recurrenceID := at(2026, 1, 3, 9, 0)
	master := repository.Event{
		ID:    "master-1",
		Start: at(2026, 1, 1, 9, 0),
		End:   at(2026, 1, 1, 9, 30),
		Rrule: "FREQ=DAILY",
		Reminders: []repository.Reminder{
			{ID: 100, OffsetMinutes: 10, Channel: "notification"},
		},
	}
	override := repository.Event{
		ID:           "override-1",
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
		Start:        at(2026, 1, 3, 14, 0), // moved to a different time of day
		End:          at(2026, 1, 3, 14, 30),
		Reminders: []repository.Reminder{
			{ID: 200, OffsetMinutes: 5, Channel: "email"},
		},
	}
	events := []repository.Event{master, override}

	// The Master's stale slot (its own Reminder, 10 min before 09:00 = 08:50)
	// must not fire...
	staleWindow, err := DueAll(events, at(2026, 1, 3, 8, 45), at(2026, 1, 3, 8, 55))
	if err != nil {
		t.Fatalf("due all: %v", err)
	}
	if len(staleWindow) != 0 {
		t.Fatalf("expected the overridden Master slot not to fire, got %+v", staleWindow)
	}

	// ...only the Override's own Reminder, at its own (moved) time, does.
	overrideWindow, err := DueAll(events, at(2026, 1, 3, 13, 50), at(2026, 1, 3, 13, 55))
	if err != nil {
		t.Fatalf("due all: %v", err)
	}
	if len(overrideWindow) != 1 || overrideWindow[0].ReminderID != 200 {
		t.Fatalf("expected only the Override's own Reminder to fire, got %+v", overrideWindow)
	}
}
