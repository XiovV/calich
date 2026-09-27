package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// setUpBookingLinkSlotsFixture bootstraps a User with their default
// Workspace, a book-into Calendar, a Mon-Fri 09:00-17:00 Schedule in
// Etc/UTC, and a 60-minute Booking Link with no minimum notice and a
// generous horizon — TestDeriveSlotsForMonth_InProgressEvent builds on it
// directly, bypassing the HTTP handler (which always derives against the
// real wall clock) so the test can supply its own "now".
func setUpBookingLinkSlotsFixture(t *testing.T) (g *Graph, userID, workspaceID, linkID int64, calendarID string) {
	t.Helper()
	ctx := context.Background()

	g = newTestGraph(t)
	user, _, err := g.Auth.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	workspaces, err := g.Workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list workspaces: %v (%d)", err, len(workspaces))
	}
	workspaceID = workspaces[0].ID

	calendarID = "11111111-1111-1111-1111-111111111111"
	if _, err := g.Calendars.Create(ctx, user.ID, workspaceID, calendarID, CalendarWrite{Name: "Work", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	ranges := make([]repository.AvailabilityRange, 5)
	for i, weekday := range []int{1, 2, 3, 4, 5} {
		ranges[i] = repository.AvailabilityRange{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}
	}
	schedule, err := g.AvailabilitySchedules.Create(ctx, user.ID, "Default", "Etc/UTC", ranges)
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	link, err := g.BookingLinks.Create(ctx, user.ID, workspaceID, BookingLinkWrite{
		Title: "Intro call", Slug: "intro-call", DurationMinutes: 60, Visibility: "public",
		AvailabilityScheduleID: schedule.ID, BookIntoCalendarID: calendarID,
		MinimumNoticeMinutes: 0, BookingHorizonDays: 30, TasksInConflictSet: true,
	})
	if err != nil {
		t.Fatalf("create booking link: %v", err)
	}

	return g, user.ID, workspaceID, link.ID, calendarID
}

// TestDeriveSlotsForMonth_InProgressEventClosesItsSlot covers an event
// already running at derivation time — started before "now", still Busy
// through part of its own scheduled slot — which must still close that
// slot. recurrence.ExpandOccurrences (and rrule-go's own Between) report an
// Occurrence only when its *start* falls in the window handed to them, so
// naively passing `now` as that lower bound silently drops an in-progress
// Event; expandBusyOccurrences widens it by the Event's own duration to
// compensate.
func TestDeriveSlotsForMonth_InProgressEventClosesItsSlot(t *testing.T) {
	g, userID, workspaceID, linkID, calendarID := setUpBookingLinkSlotsFixture(t)
	ctx := context.Background()

	monday := nextWeekdayUTC(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Monday)
	eventStart := monday.Add(10 * time.Hour)
	eventEnd := monday.Add(12 * time.Hour) // a 2-hour Busy event
	// now sits mid-Occurrence — the event started at 10:00 and is still
	// running — but the *candidate slot* this test checks (11:00-12:00) is
	// still ahead of now, so minimum notice alone can't be what excludes
	// it: only the Busy interval can.
	now := eventStart.Add(30 * time.Minute)
	laterSlot := monday.Add(11 * time.Hour)

	if _, err := g.Events.Create(ctx, userID, "evt-in-progress", EventWrite{
		CalendarID: calendarID, Title: "Already running", Busy: true,
		Start: eventStart, End: eventEnd,
	}); err != nil {
		t.Fatalf("create in-progress event: %v", err)
	}

	slots, err := g.BookingLinks.DeriveSlotsForMonth(ctx, userID, workspaceID, linkID, monday.Year(), monday.Month(), now)
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	for _, s := range slots {
		if s.Equal(laterSlot) {
			t.Fatalf("expected the in-progress event's remaining 11:00 slot to stay closed, got %v", slots)
		}
	}
}

func nextWeekdayUTC(from time.Time, weekday time.Weekday) time.Time {
	for from.Weekday() != weekday {
		from = from.AddDate(0, 0, 1)
	}
	return from
}
