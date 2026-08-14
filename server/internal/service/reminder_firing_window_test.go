package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/reminder"
	"github.com/XiovV/calendar/server/internal/repository"
)

// The firing engine's candidate read is bounded by the tick's own window
// (#219), and the bound has to be widened by the offsets in play rather than
// by anything assumed about how long an offset can be: a Reminder's trigger is
// its Occurrence's anchor minus its own offset, so a fortnight-before default
// fires on an Occurrence a fortnight past the end of the tick. A window that
// only spans the tick itself finds no candidate at all and the cue is simply
// never delivered — this pins that it is.
//
// Timed and all-day are both covered because their anchors differ: an all-day
// Occurrence's is 09:00 on its date rather than its stored midnight start
// (ADR-0020), so the two land on opposite sides of the widening.
func TestFiringWindow_ALongOffsetStillFiresOnItsOwnTick(t *testing.T) {
	const fortnight = 14 * 24 * 60 // minutes

	cases := []struct {
		name   string
		allDay bool
		// start is the Occurrence's own start; anchor is what its offset
		// counts back from.
		start, anchor time.Time
	}{
		{
			name:   "timed",
			start:  time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
			anchor: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
		},
		{
			name:   "all-day",
			allDay: true,
			start:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			anchor: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
			ctx := context.Background()

			if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, tc.allDay, []repository.Reminder{
				{OffsetMinutes: fortnight, Channel: "notification"},
			}); err != nil {
				t.Fatalf("set default: %v", err)
			}

			write := EventWrite{CalendarID: calendarID, Title: "Bin day", Start: tc.start, End: tc.start.Add(time.Hour), AllDay: tc.allDay}
			if tc.allDay {
				write.End = tc.start.AddDate(0, 0, 1)
			}
			event, err := svc.Create(ctx, ownerID, "evt-1", write)
			if err != nil {
				t.Fatalf("create event: %v", err)
			}

			// The one minute the trigger falls in, and nothing wider.
			trigger := tc.anchor.Add(-fortnight * time.Minute)
			from, to := trigger.Add(-time.Minute), trigger

			events, resolved, err := svc.ListAllWithReminders(ctx, from, to)
			if err != nil {
				t.Fatalf("list all with reminders: %v", err)
			}
			due, err := reminder.DueAll(events, resolved, from, to)
			if err != nil {
				t.Fatalf("due all: %v", err)
			}

			if len(due) != 1 {
				t.Fatalf("expected the fortnight-before default to fire on this tick, got %+v", due)
			}
			if due[0].EventID != event.ID || due[0].UserID != memberID || !due[0].OccurrenceStart.Equal(tc.start) {
				t.Fatalf("expected the member's Reminder for %s at %s, got %+v", event.ID, tc.start, due[0])
			}
		})
	}
}

// A Master keeps generating the Occurrence slot an Override has replaced —
// creating an Override adds no Exdate — so DueAll excludes every Override's
// recurrence id from its Master's expansion, and can only do that for the
// Overrides it was handed. An Occurrence moved far enough is outside any
// window the tick could sensibly read, and its Override resolves a default
// rather than carrying a Reminder row of its own, so nothing else pulls it in:
// the read has to hand over the Overrides of the Masters it returns regardless
// of the window, or the Master fires a stale cue for a slot that no longer
// exists.
func TestFiringWindow_AnOverrideMovedOutsideTheWindowStillShadowsItsMaster(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
	}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	master, err := svc.Create(ctx, ownerID, "master", EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(30 * time.Minute), Rrule: "FREQ=DAILY",
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	// The 1 March Occurrence, moved two months on.
	recurrenceID := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	movedTo := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, ownerID, "override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)", Start: movedTo, End: movedTo.Add(30 * time.Minute),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	// The tick the replaced slot's trigger would have fallen in.
	trigger := recurrenceID.Add(-10 * time.Minute)
	from, to := trigger.Add(-time.Minute), trigger

	events, resolved, err := svc.ListAllWithReminders(ctx, from, to)
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}
	due, err := reminder.DueAll(events, resolved, from, to)
	if err != nil {
		t.Fatalf("due all: %v", err)
	}

	if len(due) != 0 {
		t.Fatalf("expected the overridden Occurrence to fire nothing from its Master, got %+v", due)
	}
}

// The other half of the same window: a tick whose trigger window is nowhere
// near the Occurrence reads no candidate and fires nothing, which is the whole
// point of bounding the read.
func TestFiringWindow_ATickNowhereNearTheTriggerFiresNothing(t *testing.T) {
	svc, ownerID, memberID, calendarID := newTestReminderResolutionFixture(t)
	ctx := context.Background()

	if _, err := svc.calendars.SetDefaultReminders(ctx, memberID, calendarID, false, []repository.Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
	}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	start := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: start.Add(time.Hour)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	from := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)

	events, resolved, err := svc.ListAllWithReminders(ctx, from, to)
	if err != nil {
		t.Fatalf("list all with reminders: %v", err)
	}
	due, err := reminder.DueAll(events, resolved, from, to)
	if err != nil {
		t.Fatalf("due all: %v", err)
	}

	if len(due) != 0 {
		t.Fatalf("expected a month-later tick to fire nothing, got %+v", due)
	}
	if containsEventID(events, "evt-1") {
		t.Fatalf("expected the Event to be outside this tick's candidate window, got %+v", events)
	}
}
