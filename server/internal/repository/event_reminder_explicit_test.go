package repository

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/db"
)

// newTestEventReminderExplicitRepository returns an EventRepository and
// EventReminderExplicitRepository sharing one in-memory database, plus two
// real user ids and two real event ids to satisfy foreign keys.
func newTestEventReminderExplicitRepository(t *testing.T) (events *EventRepository, explicit *EventReminderExplicitRepository, userID, otherUserID int64, eventID, otherEventID, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	otherUser, err := users.Create(context.Background(), "user-b", "user-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(context.Background(), "workspace-a", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	cal, err := calendars.Create(context.Background(), user.ID, workspace.ID, "cal-1", CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	events = NewEventRepository(sqlDB)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	ev, err := events.Create(context.Background(), "evt-1", &user.ID, EventFields{CalendarID: cal.ID, Title: "Standup", Start: start, End: end}, 1)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	otherEv, err := events.Create(context.Background(), "evt-2", &user.ID, EventFields{CalendarID: cal.ID, Title: "Retro", Start: start, End: end}, 2)
	if err != nil {
		t.Fatalf("create other event: %v", err)
	}

	return events, NewEventReminderExplicitRepository(sqlDB), user.ID, otherUser.ID, ev.ID, otherEv.ID, cal.ID
}

func TestEventReminderExplicitRepository_MarkIsIdempotent(t *testing.T) {
	_, explicit, userID, _, eventID, _, _ := newTestEventReminderExplicitRepository(t)
	ctx := context.Background()

	if err := explicit.Mark(ctx, userID, eventID); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := explicit.Mark(ctx, userID, eventID); err != nil {
		t.Fatalf("mark again: %v", err)
	}

	markers, err := explicit.ListByEventIDs(ctx, []string{eventID}, []int64{userID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !markers[eventID][userID] {
		t.Fatalf("expected eventID marked explicit for userID")
	}
}

func TestEventReminderExplicitRepository_ScopedPerUser(t *testing.T) {
	_, explicit, userID, otherUserID, eventID, _, _ := newTestEventReminderExplicitRepository(t)
	ctx := context.Background()

	if err := explicit.Mark(ctx, userID, eventID); err != nil {
		t.Fatalf("mark: %v", err)
	}

	markers, err := explicit.ListByEventIDs(ctx, []string{eventID}, []int64{otherUserID})
	if err != nil {
		t.Fatalf("list for otherUserID: %v", err)
	}
	if markers[eventID][otherUserID] {
		t.Fatalf("otherUserID must not see userID's explicit marker")
	}
}

func TestEventReminderExplicitRepository_ListByEventIDs_EveryUser(t *testing.T) {
	_, explicit, userID, otherUserID, eventID, _, _ := newTestEventReminderExplicitRepository(t)
	ctx := context.Background()

	if err := explicit.Mark(ctx, userID, eventID); err != nil {
		t.Fatalf("mark userID: %v", err)
	}
	if err := explicit.Mark(ctx, otherUserID, eventID); err != nil {
		t.Fatalf("mark otherUserID: %v", err)
	}

	markers, err := explicit.ListByEventIDs(ctx, []string{eventID}, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !markers[eventID][userID] || !markers[eventID][otherUserID] {
		t.Fatalf("expected both users marked, got %+v", markers)
	}
}

func TestEventReminderExplicitRepository_CopyByEventID(t *testing.T) {
	_, explicit, userID, otherUserID, eventID, otherEventID, _ := newTestEventReminderExplicitRepository(t)
	ctx := context.Background()

	if err := explicit.Mark(ctx, userID, eventID); err != nil {
		t.Fatalf("mark userID: %v", err)
	}
	if err := explicit.Mark(ctx, otherUserID, eventID); err != nil {
		t.Fatalf("mark otherUserID: %v", err)
	}

	if err := explicit.CopyByEventID(ctx, eventID, otherEventID); err != nil {
		t.Fatalf("copy: %v", err)
	}

	markers, err := explicit.ListByEventIDs(ctx, []string{otherEventID}, nil)
	if err != nil {
		t.Fatalf("list copied markers: %v", err)
	}
	if !markers[otherEventID][userID] || !markers[otherEventID][otherUserID] {
		t.Fatalf("expected both markers copied onto otherEventID, got %+v", markers)
	}
}

func TestEventReminderExplicitRepository_DeleteByUserAndCalendar(t *testing.T) {
	_, explicit, userID, _, eventID, otherEventID, calendarID := newTestEventReminderExplicitRepository(t)
	ctx := context.Background()

	if err := explicit.Mark(ctx, userID, eventID); err != nil {
		t.Fatalf("mark eventID: %v", err)
	}
	if err := explicit.Mark(ctx, userID, otherEventID); err != nil {
		t.Fatalf("mark otherEventID: %v", err)
	}

	if err := explicit.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	markers, err := explicit.ListByEventIDs(ctx, []string{eventID, otherEventID}, []int64{userID})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if markers[eventID][userID] || markers[otherEventID][userID] {
		t.Fatalf("expected all markers cleared, got %+v", markers)
	}
}
