// event_color_test.go covers #147/ADR-0043's per-Event color override:
// Create/Update persisting and round-tripping a color, validation against
// NormalizeColor's hex value space, that clearing it sets the column back to
// absent rather than copying the Calendar's own color, and that setting or
// clearing requires the same Editor Access rule as title or time (ADR-0034).
package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEventService_Create_PersistsColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end,
		Color: strPtr("#ff6b35"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if event.Color == nil || *event.Color != "#FF6B35FF" {
		t.Fatalf("expected normalized color, got %v", event.Color)
	}
}

func TestEventService_Create_NoColorIsAbsent(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if event.Color != nil {
		t.Fatalf("expected no color, got %v", *event.Color)
	}
}

func TestEventService_Create_RejectsInvalidColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := svc.Create(ctx, userID, "evt-1", EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end,
		Color: strPtr("not-a-color"),
	})
	if !errors.Is(err, ErrInvalidEventColor) {
		t.Fatalf("err = %v, want ErrInvalidEventColor", err)
	}
}

func TestEventService_Update_SetsAndClearsColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end,
		Color: strPtr("#123456"),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Color == nil || *updated.Color != "#123456FF" {
		t.Fatalf("expected set color, got %v", updated.Color)
	}

	// A "Reset to Calendar color" write clears the column back to absent
	// rather than copying the Calendar's own current color (ADR-0043).
	cleared, err := svc.Update(ctx, userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if cleared.Color != nil {
		t.Fatalf("expected color cleared to absent, got %v", *cleared.Color)
	}
}

func TestEventService_Update_RejectsInvalidColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = svc.Update(ctx, userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end,
		Color: strPtr("#zzz"),
	})
	if !errors.Is(err, ErrInvalidEventColor) {
		t.Fatalf("err = %v, want ErrInvalidEventColor", err)
	}
}

// TestEventService_Update_ViewerCannotSetColor proves setting or clearing an
// Event's color requires Editor Access, the same Role rule as editing title
// or time (ADR-0034) — a Viewer's attempt is refused with the same
// ErrCalendarReadOnly every other field-edit uses.
func TestEventService_Update_ViewerCannotSetColor(t *testing.T) {
	f := newEventShareFixture(t)
	ctx := context.Background()

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = f.events.Update(ctx, f.viewerID, event.ID, EventWrite{
		CalendarID: f.calendarID, Title: "Standup", Start: shareTestStart, End: shareTestEnd,
		Color: strPtr("#123456"),
	})
	if !errors.Is(err, ErrCalendarReadOnly) {
		t.Fatalf("viewer set color err = %v, want ErrCalendarReadOnly", err)
	}
}

// The following cover PutSeries and ImportSeries (icalendar's #149 CalDAV
// round-trip writes through these) normalizing and persisting Color the
// same way Create/Update do (ADR-0043) — nil stays absent, a valid hex
// normalizes, and an invalid one is rejected before anything is written.

func TestEventService_PutSeries_PersistsMasterAndOverrideColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	recurrenceID := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)

	master, overrides, err := svc.PutSeries(ctx, userID, calendarID, "client-uid-1", SeriesWrite{
		Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY",
		Color: strPtr("#ff6b35"),
		Overrides: []OverrideWrite{
			{RecurrenceID: recurrenceID, Title: "Standup", Start: recurrenceID, End: recurrenceID.Add(30 * time.Minute), Color: strPtr("#123456")},
		},
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if master.Color == nil || *master.Color != "#FF6B35FF" {
		t.Fatalf("expected normalized master color, got %v", master.Color)
	}
	if len(overrides) != 1 || overrides[0].Color == nil || *overrides[0].Color != "#123456FF" {
		t.Fatalf("expected normalized override color, got %+v", overrides)
	}
}

func TestEventService_PutSeries_NoColorIsAbsent(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	master, _, err := svc.PutSeries(ctx, userID, calendarID, "client-uid-1", SeriesWrite{
		Title: "Standup", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("put series: %v", err)
	}
	if master.Color != nil {
		t.Fatalf("expected no color, got %v", *master.Color)
	}
}

func TestEventService_PutSeries_RejectsInvalidColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	_, _, err := svc.PutSeries(ctx, userID, calendarID, "client-uid-1", SeriesWrite{
		Title: "Standup", Start: start, End: end, Color: strPtr("not-a-color"),
	})
	if !errors.Is(err, ErrInvalidEventColor) {
		t.Fatalf("err = %v, want ErrInvalidEventColor", err)
	}
}

func TestEventService_ImportSeries_PersistsMasterAndOverrideColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	recurrenceID := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)

	n, err := svc.ImportSeries(ctx, userID, calendarID, []SeriesWrite{{
		Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY",
		Color: strPtr("#ff6b35"),
		Overrides: []OverrideWrite{
			{RecurrenceID: recurrenceID, Title: "Standup", Start: recurrenceID, End: recurrenceID.Add(30 * time.Minute), Color: strPtr("#123456")},
		},
	}})
	if err != nil {
		t.Fatalf("import series: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 series imported, got %d", n)
	}

	from := start.Add(-24 * time.Hour)
	to := recurrenceID.Add(24 * time.Hour)
	events, err := svc.List(ctx, userID, &from, &to)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var sawMasterColor, sawOverrideColor bool
	for _, e := range events {
		if e.ParentID == nil && e.Color != nil && *e.Color == "#FF6B35FF" {
			sawMasterColor = true
		}
		if e.ParentID != nil && e.Color != nil && *e.Color == "#123456FF" {
			sawOverrideColor = true
		}
	}
	if !sawMasterColor {
		t.Fatalf("expected the imported master to carry its normalized color, got %+v", events)
	}
	if !sawOverrideColor {
		t.Fatalf("expected the imported override to carry its normalized color, got %+v", events)
	}
}

func TestEventService_ImportSeries_RejectsInvalidColor(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)

	_, err := svc.ImportSeries(ctx, userID, calendarID, []SeriesWrite{{
		Title: "Standup", Start: start, End: end, Color: strPtr("not-a-color"),
	}})
	if !errors.Is(err, ErrInvalidEventColor) {
		t.Fatalf("err = %v, want ErrInvalidEventColor", err)
	}
}
