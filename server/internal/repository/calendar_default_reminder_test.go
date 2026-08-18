package repository

import (
	"context"
	"testing"

	"github.com/XiovV/calich/server/internal/db"
)

// newTestCalendarDefaultReminderRepository returns a CalendarDefaultReminderRepository
// plus two real user ids and a real calendar id to satisfy foreign keys.
func newTestCalendarDefaultReminderRepository(t *testing.T) (repo *CalendarDefaultReminderRepository, userID, otherUserID int64, calendarID string) {
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

	return NewCalendarDefaultReminderRepository(sqlDB), user.ID, otherUser.ID, cal.ID
}

func TestCalendarDefaultReminderRepository_ReplaceByCalendarID_WholesaleReplacesOneList(t *testing.T) {
	repo, userID, _, calendarID := newTestCalendarDefaultReminderRepository(t)
	ctx := context.Background()

	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, false, []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace timed: %v", err)
	}
	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, true, []Reminder{{OffsetMinutes: 1440, Channel: "email"}}); err != nil {
		t.Fatalf("replace all-day: %v", err)
	}

	timed, allDay, err := repo.ListByCalendarID(ctx, userID, calendarID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(timed) != 1 || timed[0].OffsetMinutes != 10 || timed[0].Channel != "notification" {
		t.Fatalf("unexpected timed list: %+v", timed)
	}
	if len(allDay) != 1 || allDay[0].OffsetMinutes != 1440 || allDay[0].Channel != "email" {
		t.Fatalf("unexpected all-day list: %+v", allDay)
	}

	// Retuning the timed list must not touch the all-day one.
	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, false, []Reminder{{OffsetMinutes: 30, Channel: "email"}}); err != nil {
		t.Fatalf("retune timed: %v", err)
	}
	timed, allDay, err = repo.ListByCalendarID(ctx, userID, calendarID)
	if err != nil {
		t.Fatalf("list after retune: %v", err)
	}
	if len(timed) != 1 || timed[0].OffsetMinutes != 30 {
		t.Fatalf("timed list not replaced: %+v", timed)
	}
	if len(allDay) != 1 || allDay[0].OffsetMinutes != 1440 {
		t.Fatalf("all-day list unexpectedly changed: %+v", allDay)
	}
}

func TestCalendarDefaultReminderRepository_ScopedPerUser(t *testing.T) {
	repo, userID, otherUserID, calendarID := newTestCalendarDefaultReminderRepository(t)
	ctx := context.Background()

	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, false, []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace for userID: %v", err)
	}

	otherTimed, _, err := repo.ListByCalendarID(ctx, otherUserID, calendarID)
	if err != nil {
		t.Fatalf("list for otherUserID: %v", err)
	}
	if len(otherTimed) != 0 {
		t.Fatalf("expected no defaults for otherUserID, got %+v", otherTimed)
	}
}

// A nil userIDs asks for every User's defaults — the firing engine's audience
// (ADR-0021, ADR-0064).
func TestCalendarDefaultReminderRepository_ListByCalendarIDs_EveryUser(t *testing.T) {
	repo, userID, otherUserID, calendarID := newTestCalendarDefaultReminderRepository(t)
	ctx := context.Background()

	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, false, []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace timed for userID: %v", err)
	}
	if err := repo.ReplaceByCalendarID(ctx, otherUserID, calendarID, true, []Reminder{{OffsetMinutes: 1440, Channel: "email"}}); err != nil {
		t.Fatalf("replace all-day for otherUserID: %v", err)
	}

	timedByCalendarUser, allDayByCalendarUser, err := repo.ListByCalendarIDs(ctx, []string{calendarID}, nil)
	if err != nil {
		t.Fatalf("list by calendar ids: %v", err)
	}
	if len(timedByCalendarUser[calendarID][userID]) != 1 {
		t.Fatalf("expected userID's timed default, got %+v", timedByCalendarUser)
	}
	if len(allDayByCalendarUser[calendarID][otherUserID]) != 1 {
		t.Fatalf("expected otherUserID's all-day default, got %+v", allDayByCalendarUser)
	}
	if len(timedByCalendarUser[calendarID][otherUserID]) != 0 {
		t.Fatalf("otherUserID should have no timed default: %+v", timedByCalendarUser)
	}
}

func TestCalendarDefaultReminderRepository_CalendarIDsWithAny(t *testing.T) {
	repo, userID, _, calendarID := newTestCalendarDefaultReminderRepository(t)
	ctx := context.Background()

	ids, err := repo.CalendarIDsWithAny(ctx)
	if err != nil {
		t.Fatalf("list before any default set: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no calendars, got %v", ids)
	}

	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, false, []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	ids, err = repo.CalendarIDsWithAny(ctx)
	if err != nil {
		t.Fatalf("list after default set: %v", err)
	}
	if len(ids) != 1 || ids[0] != calendarID {
		t.Fatalf("expected [%s], got %v", calendarID, ids)
	}
}

func TestCalendarDefaultReminderRepository_OffsetMinutesRange(t *testing.T) {
	repo, userID, otherUserID, calendarID := newTestCalendarDefaultReminderRepository(t)
	ctx := context.Background()

	minOffset, maxOffset, err := repo.OffsetMinutesRange(ctx)
	if err != nil {
		t.Fatalf("range before any default set: %v", err)
	}
	if minOffset != 0 || maxOffset != 0 {
		t.Fatalf("expected no default to widen a window by anything, got [%d, %d]", minOffset, maxOffset)
	}

	// Spread across both lists and both Users: the range is what the firing
	// engine widens one window by, so it spans every default anywhere.
	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, false, []Reminder{
		{OffsetMinutes: 10, Channel: "notification"},
		{OffsetMinutes: 20160, Channel: "email"},
	}); err != nil {
		t.Fatalf("replace timed: %v", err)
	}
	if err := repo.ReplaceByCalendarID(ctx, otherUserID, calendarID, true, []Reminder{
		{OffsetMinutes: -15, Channel: "notification"},
	}); err != nil {
		t.Fatalf("replace all-day: %v", err)
	}

	minOffset, maxOffset, err = repo.OffsetMinutesRange(ctx)
	if err != nil {
		t.Fatalf("range after defaults set: %v", err)
	}
	if minOffset != -15 || maxOffset != 20160 {
		t.Fatalf("expected [-15, 20160], got [%d, %d]", minOffset, maxOffset)
	}
}

func TestCalendarDefaultReminderRepository_DeleteByUserAndCalendar_ClearsBothLists(t *testing.T) {
	repo, userID, _, calendarID := newTestCalendarDefaultReminderRepository(t)
	ctx := context.Background()

	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, false, []Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("replace timed: %v", err)
	}
	if err := repo.ReplaceByCalendarID(ctx, userID, calendarID, true, []Reminder{{OffsetMinutes: 1440, Channel: "email"}}); err != nil {
		t.Fatalf("replace all-day: %v", err)
	}

	if err := repo.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	timed, allDay, err := repo.ListByCalendarID(ctx, userID, calendarID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(timed) != 0 || len(allDay) != 0 {
		t.Fatalf("expected both lists cleared, got timed=%+v allDay=%+v", timed, allDay)
	}

	// Idempotent: clearing again is a no-op, not an error.
	if err := repo.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		t.Fatalf("delete again: %v", err)
	}
}
