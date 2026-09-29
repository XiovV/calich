// event_busy_test.go covers #319/ADR-0086's Busy field: Create/Update
// persisting and round-tripping it, that it is settable independently on an
// Override from its Master, and that an unrelated field edit leaves it
// untouched.
package service

import (
	"context"
	"testing"
	"time"
)

func TestEventService_Create_PersistsBusy(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end, Busy: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !event.Busy {
		t.Fatalf("expected a timed Event's create to persist Busy, got %+v", event)
	}
}

// TestEventService_Create_AllDayFree covers an all-day Event's own create
// path passing Busy false through unchanged — this service never re-derives
// Busy from AllDay itself (ADR-0086 leaves that departure to the caller).
func TestEventService_Create_AllDayFree(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{
		CalendarID: calendarID, Title: "Holiday", Start: start, End: end, AllDay: true, Busy: false,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if event.Busy {
		t.Fatalf("expected an all-day create's own Busy:false to persist as Free, got %+v", event)
	}
}

func TestEventService_Update_TogglesBusy(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end, Busy: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end, Busy: false,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Busy {
		t.Fatalf("expected update to flip the Event to Free, got %+v", updated)
	}

	fetched, err := svc.Get(ctx, userID, event.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Busy {
		t.Fatalf("expected Free to persist, got %+v", fetched)
	}
}

// TestEventService_CreateOverride_BusyIndependentOfMaster covers ADR-0086's
// "settable on a Master and independently on an Override" requirement — one
// Occurrence of a series can flip to Free without touching its Master or
// becoming anything other than an ordinary Override.
func TestEventService_CreateOverride_BusyIndependentOfMaster(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	master, err := svc.Create(ctx, userID, "master", EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY", Busy: true,
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, userID, "override", EventWrite{
		CalendarID: calendarID, Title: "Standup (optional today)",
		Start: time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC),
		ParentID: &master.ID, RecurrenceID: &recurrenceID, Busy: false,
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	if override.Busy {
		t.Fatalf("expected the Override to be Free, got %+v", override)
	}

	refetchedMaster, err := svc.Get(ctx, userID, master.ID)
	if err != nil {
		t.Fatalf("get master: %v", err)
	}
	if !refetchedMaster.Busy {
		t.Fatalf("expected the Master to remain Busy, unaffected by its Override, got %+v", refetchedMaster)
	}
}

// TestEventService_Update_UnrelatedFieldEditLeavesBusyAlone covers Busy
// surviving a plain edit of another field — Update always rewrites every
// column from write, so a caller must carry the row's current Busy forward,
// exactly as it must for title or color.
func TestEventService_Update_UnrelatedFieldEditLeavesBusyAlone(t *testing.T) {
	svc, userID, calendarID := newTestEventService(t)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := svc.Create(ctx, userID, "evt-1", EventWrite{
		CalendarID: calendarID, Title: "Standup", Start: start, End: end, Busy: false,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(ctx, userID, event.ID, EventWrite{
		CalendarID: calendarID, Title: "Standup (renamed)", Start: start, End: end, Busy: false,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Busy {
		t.Fatalf("expected Free to survive an unrelated rename, got %+v", updated)
	}
}
