package service

import (
	"context"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// getSource reads a Linked Calendar's Source row for the assertions below.
func getSource(t *testing.T, svc *ConnectionService, userID int64, calendarID string) *repository.Source {
	t.Helper()
	cal, err := svc.calendars.Get(context.Background(), userID, calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Source == nil {
		t.Fatalf("expected calendar %s to carry a Source", calendarID)
	}
	return cal.Source
}

func masterIDsByExternalUID(t *testing.T, svc *ConnectionService, userID int64, calendarID string) map[string]string {
	t.Helper()
	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendarID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	out := make(map[string]string, len(masters))
	for _, m := range masters {
		if m.ExternalUID != nil {
			out[*m.ExternalUID] = m.ID
		}
	}
	return out
}

func TestConnectionService_FullRefresh_StoresACursor(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}
	google.nextSyncToken = "cursor-after-import"

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	src := getSource(t, svc, userID, calendar.ID)
	if src.Cursor == nil || *src.Cursor != "cursor-after-import" {
		t.Fatalf("expected the initial Full Refresh to store the cursor, got %v", src.Cursor)
	}
	if src.NextRefreshAt == nil {
		t.Fatalf("expected next_refresh_at scheduled so the poller picks the Linked Calendar up")
	}
}

func TestConnectionService_DeltaRefresh_OneChangedSeriesLeavesOthersUntouched(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York"),
			googleEventItem("evt-2", "Haircut", "2026-01-16T10:00:00-05:00", "2026-01-16T11:00:00-05:00", "America/New_York"),
			googleEventItem("evt-3", "Standup", "2026-01-17T10:00:00-05:00", "2026-01-17T11:00:00-05:00", "America/New_York"),
		},
	}
	google.nextSyncToken = "cursor-1"

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	before := masterIDsByExternalUID(t, svc, userID, calendar.ID)
	if len(before) != 3 {
		t.Fatalf("expected 3 series imported, got %d", len(before))
	}

	// Since cursor-1, only evt-1 changed.
	google.eventsDeltaByToken = map[string][]map[string]any{
		"cursor-1": {googleEventItem("evt-1", "Dentist (rescheduled)", "2026-01-18T10:00:00-05:00", "2026-01-18T11:00:00-05:00", "America/New_York")},
	}
	google.nextSyncToken = "cursor-2"

	result, err := svc.RefreshLinked(context.Background(), userID, calendar.ID, RefreshModeDelta)
	if err != nil {
		t.Fatalf("delta refresh: %v", err)
	}

	if result.Updated != 1 || result.Created != 0 || result.Tombstoned != 0 {
		t.Fatalf("expected exactly one upsert and zero tombstones, got %+v", result)
	}
	if google.lastSyncTokenSeen != "cursor-1" {
		t.Fatalf("expected the delta request to present the stored cursor, got %q", google.lastSyncTokenSeen)
	}

	after := masterIDsByExternalUID(t, svc, userID, calendar.ID)
	for uid, id := range before {
		if after[uid] != id {
			t.Fatalf("row id for %s changed: %s -> %s (delta must preserve ids)", uid, id, after[uid])
		}
	}

	masters, _, _ := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	for _, m := range masters {
		if *m.ExternalUID == "evt-1" && m.Title != "Dentist (rescheduled)" {
			t.Fatalf("expected evt-1 retitled, got %q", m.Title)
		}
		if *m.ExternalUID == "evt-2" && m.Title != "Haircut" {
			t.Fatalf("expected evt-2 untouched, got %q", m.Title)
		}
	}

	src := getSource(t, svc, userID, calendar.ID)
	if src.Cursor == nil || *src.Cursor != "cursor-2" {
		t.Fatalf("expected the fresh cursor stored, got %v", src.Cursor)
	}
}

func TestConnectionService_DeltaRefresh_ExplicitCancellationDeletesTheSeries(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York"),
			googleEventItem("evt-2", "Haircut", "2026-01-16T10:00:00-05:00", "2026-01-16T11:00:00-05:00", "America/New_York"),
		},
	}
	google.nextSyncToken = "cursor-1"

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	google.eventsDeltaByToken = map[string][]map[string]any{
		"cursor-1": {{"id": "evt-1", "status": "cancelled"}},
	}
	google.nextSyncToken = "cursor-2"

	result, err := svc.RefreshLinked(context.Background(), userID, calendar.ID, RefreshModeDelta)
	if err != nil {
		t.Fatalf("delta refresh: %v", err)
	}
	if result.Tombstoned != 1 {
		t.Fatalf("expected exactly one explicit deletion, got %+v", result)
	}

	after := masterIDsByExternalUID(t, svc, userID, calendar.ID)
	if _, gone := after["evt-1"]; gone {
		t.Fatalf("expected evt-1 deleted, still present")
	}
	if _, ok := after["evt-2"]; !ok {
		t.Fatalf("expected evt-2 to survive")
	}
}

// If the Provider hands back no fresh cursor on a successful Delta Refresh,
// the stored one is kept rather than nil-ed — nil-ing it would force a
// needless Full Refresh next cycle.
func TestConnectionService_DeltaRefresh_MissingNextTokenKeepsThePriorCursor(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}
	google.nextSyncToken = "cursor-1"

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	google.eventsDeltaByToken = map[string][]map[string]any{
		"cursor-1": {googleEventItem("evt-1", "Dentist (moved)", "2026-01-18T10:00:00-05:00", "2026-01-18T11:00:00-05:00", "America/New_York")},
	}
	google.nextSyncToken = "" // Provider omits it this round

	if _, err := svc.RefreshLinked(context.Background(), userID, calendar.ID, RefreshModeDelta); err != nil {
		t.Fatalf("delta refresh: %v", err)
	}

	src := getSource(t, svc, userID, calendar.ID)
	if src.Cursor == nil || *src.Cursor != "cursor-1" {
		t.Fatalf("expected the prior cursor kept, got %v", src.Cursor)
	}
}

func TestConnectionService_DeltaRefresh_ExpiredCursorFallsBackToFullPreservingIDs(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York"),
			googleEventItem("evt-2", "Haircut", "2026-01-16T10:00:00-05:00", "2026-01-16T11:00:00-05:00", "America/New_York"),
		},
	}
	google.nextSyncToken = "cursor-1"

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	before := masterIDsByExternalUID(t, svc, userID, calendar.ID)

	// The cursor is gone; the full listing (still both events) is unchanged.
	google.syncTokenExpired = true
	google.nextSyncToken = "cursor-recovered"

	result, err := svc.RefreshLinked(context.Background(), userID, calendar.ID, RefreshModeDelta)
	if err != nil {
		t.Fatalf("delta refresh with expired cursor: %v", err)
	}
	if result.Tombstoned != 0 {
		t.Fatalf("a full-refresh recovery over an unchanged listing must tombstone nothing, got %+v", result)
	}

	after := masterIDsByExternalUID(t, svc, userID, calendar.ID)
	for uid, id := range before {
		if after[uid] != id {
			t.Fatalf("row id for %s changed during 410 recovery: %s -> %s", uid, id, after[uid])
		}
	}

	src := getSource(t, svc, userID, calendar.ID)
	if src.Cursor == nil || *src.Cursor != "cursor-recovered" {
		t.Fatalf("expected a fresh cursor after 410 recovery, got %v", src.Cursor)
	}
}

func TestConnectionService_DeltaRefresh_FailurePreservesStateAndSchedulesBackoff(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}
	google.nextSyncToken = "cursor-1"

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google,
		withConnectionNow(func() time.Time { return now }),
		withConnectionRefreshInterval(15*time.Minute),
	)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	beforeSrc := getSource(t, svc, userID, calendar.ID)

	// The next delta request fails outright (not a 410 — a real 500).
	google.eventsStatus = 500

	if _, err := svc.RefreshLinked(context.Background(), userID, calendar.ID, RefreshModeDelta); err == nil {
		t.Fatal("expected the delta refresh to error")
	}

	afterSrc := getSource(t, svc, userID, calendar.ID)
	if afterSrc.Cursor == nil || *afterSrc.Cursor != *beforeSrc.Cursor {
		t.Fatalf("a failed refresh must not touch the cursor, got %v -> %v", beforeSrc.Cursor, afterSrc.Cursor)
	}
	if afterSrc.FailureCount != 1 || afterSrc.ErrorClass == nil {
		t.Fatalf("expected failure recorded, got count=%d class=%v", afterSrc.FailureCount, afterSrc.ErrorClass)
	}
	if afterSrc.NextRefreshAt == nil || !afterSrc.NextRefreshAt.After(now) {
		t.Fatalf("expected a backoff-scheduled retry in the future, got %v", afterSrc.NextRefreshAt)
	}

	// The one imported event is still there — failure never deletes.
	masters, _, _ := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if len(masters) != 1 {
		t.Fatalf("expected the last good Events preserved, got %d masters", len(masters))
	}
}

func TestConnectionService_DeltaRefresh_RequestedWithNoCursorIsACallerError(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.nextSyncToken = "" // the import stores no cursor

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	if _, err := svc.RefreshLinked(context.Background(), userID, calendar.ID, RefreshModeDelta); err == nil {
		t.Fatal("expected an error: Delta mode requested with no stored cursor is a caller bug, not a silent Full")
	}
}
