package caldavserver

import (
	"context"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"

	"github.com/XiovV/calich/server/internal/service"
)

// queryRange runs a calendar-query REPORT filtered to [from, to) and returns
// the UIDs it matched.
func queryRange(t *testing.T, env testCalDAVEnv, from, to time.Time) []string {
	t.Helper()

	results, err := newTestCalDAVClient(t, env).QueryCalendar(
		context.Background(),
		calendarPath(env.userID, env.calendarID),
		&caldav.CalendarQuery{
			CompFilter: caldav.CompFilter{
				Name:  ical.CompCalendar,
				Comps: []caldav.CompFilter{{Name: ical.CompEvent, Start: from, End: to}},
			},
		})
	if err != nil {
		t.Fatalf("QueryCalendar: %v", err)
	}

	uids := make([]string, 0, len(results))
	for _, r := range results {
		uids = append(uids, uidOf(t, r.Data))
	}
	return uids
}

// A recurring series matches a window that none of its stored columns touch —
// only an Occurrence its rrule generates falls inside. This is the branch a
// non-recurring event never reaches: the series' start/end are three weeks
// before the window.
func TestCalendarQuery_TimeRange_MatchesGeneratedOccurrence(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	// 2026-06-01 is a Monday; weekly gives 1, 8, 15, 22 June.
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-weekly", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup",
		Start: start, End: start.Add(30 * time.Minute),
		Rrule: "FREQ=WEEKLY",
	}); err != nil {
		t.Fatalf("create weekly master: %v", err)
	}

	// A window around the 22 June occurrence only.
	got := queryRange(t,
		env,
		time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	)
	if len(got) != 1 || got[0] != "evt-weekly" {
		t.Fatalf("expected the weekly series to match via a generated occurrence, got %v", got)
	}

	// A window in a gap between two occurrences matches nothing.
	got = queryRange(t,
		env,
		time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC),
	)
	if len(got) != 0 {
		t.Fatalf("expected no match in a gap between occurrences, got %v", got)
	}
}

// An Occurrence cancelled by an Exception must not make its series match:
// the rule still generates that slot, but it is suppressed (ADR-0016).
func TestCalendarQuery_TimeRange_SkipsCancelledOccurrence(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	master, err := env.eventService.Create(ctx, env.userID, "evt-weekly", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup",
		Start: start, End: start.Add(30 * time.Minute),
		Rrule: "FREQ=WEEKLY",
	})
	if err != nil {
		t.Fatalf("create weekly master: %v", err)
	}

	from := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	if got := queryRange(t, env, from, to); len(got) != 1 {
		t.Fatalf("expected a match before the occurrence is cancelled, got %v", got)
	}

	cancelled := start.AddDate(0, 0, 21) // 22 June
	if err := env.eventService.AddException(ctx, env.userID, master.ID, cancelled); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	if got := queryRange(t, env, from, to); len(got) != 0 {
		t.Fatalf("expected the cancelled occurrence not to match, got %v", got)
	}
}

// An Occurrence that starts before the window but runs into it overlaps and
// must match — the expansion window is padded left by the series' duration to
// catch exactly this.
func TestCalendarQuery_TimeRange_MatchesOccurrenceStartingBeforeWindow(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	// 23:00–01:00, weekly: the 22 June occurrence runs into 23 June.
	start := time.Date(2026, 6, 1, 23, 0, 0, 0, time.UTC)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-overnight", service.EventWrite{
		CalendarID: env.calendarID, Title: "Overnight",
		Start: start, End: start.Add(2 * time.Hour),
		Rrule: "FREQ=WEEKLY",
	}); err != nil {
		t.Fatalf("create overnight master: %v", err)
	}

	got := queryRange(t,
		env,
		time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
	)
	if len(got) != 1 || got[0] != "evt-overnight" {
		t.Fatalf("expected an occurrence starting before the window to still overlap it, got %v", got)
	}
}

// A series whose rrule stops generating before the window must not match.
func TestCalendarQuery_TimeRange_IgnoresSeriesEndedBeforeWindow(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-bounded", service.EventWrite{
		CalendarID: env.calendarID, Title: "Bounded",
		Start: start, End: start.Add(30 * time.Minute),
		Rrule: "FREQ=WEEKLY;COUNT=2",
	}); err != nil {
		t.Fatalf("create bounded master: %v", err)
	}

	got := queryRange(t,
		env,
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	)
	if len(got) != 0 {
		t.Fatalf("expected a series that stopped recurring not to match, got %v", got)
	}
}
