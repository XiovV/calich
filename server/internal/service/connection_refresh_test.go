package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

func googleEventItem(id, summary, startDateTime, endDateTime, tz string) map[string]any {
	return map[string]any{
		"id":      id,
		"etag":    `"etag-` + id + `"`,
		"summary": summary,
		"start":   map[string]any{"dateTime": startDateTime, "timeZone": tz},
		"end":     map[string]any{"dateTime": endDateTime, "timeZone": tz},
	}
}

// importOneCalendar drives the Calendar picker's whole round trip — connect,
// list, import — for one calendar named externalID, returning the resulting
// Linked Calendar. FullRefresh tests use this as their setup since
// ImportCalendars now runs the first Full Refresh itself (#287).
func importOneCalendar(t *testing.T, svc *ConnectionService, auth *AuthService, userID, workspaceID int64, externalID string) repository.Calendar {
	t.Helper()

	connectionID := connectUser(t, svc, auth, userID)
	created, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{externalID})
	if err != nil {
		t.Fatalf("import calendars: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 created calendar, got %d", len(created))
	}
	return created[0]
}

func TestConnectionService_ImportCalendars_RunsInitialFullRefreshSynchronously(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected the initial import to have already fetched 1 event, got %d", len(masters))
	}
	if masters[0].Title != "Dentist" {
		t.Fatalf("expected title %q, got %q", "Dentist", masters[0].Title)
	}

	got, err := svc.calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Source.LastSyncedAt == nil {
		t.Fatalf("expected LastSyncedAt to be set after the initial full refresh")
	}
}

// TestConnectionService_ImportCalendars_NameFollowsSummaryOverride guards a
// shared calendar the account holder has locally renamed at Google (a
// calendarList entry's summaryOverride) — the picker already resolves that
// name correctly (toPickerCalendar's displayName()), but ImportCalendars
// runs its Linked Calendar's first Full Refresh synchronously right after
// creating it (#287), and that refresh's own name-follows-provider
// mechanism (#289, ADR-0032) must keep resolving summaryOverride too, not
// regress to events.list's bare, override-unaware summary.
func TestConnectionService_ImportCalendars_NameFollowsSummaryOverride(t *testing.T) {
	google := newFakeGoogleServer(t)
	item := googleCalendarItem("primary", "Work", "#0b8043", "reader", true)
	item["summaryOverride"] = "DD's Work"
	google.calendarListItems = []map[string]any{item}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {}}
	google.eventsSummaryByCalendar = map[string]string{"primary": "Work"}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	// importOneCalendar's returned Calendar is captured before
	// runInitialFullRefresh runs (ImportCalendars appends it to created,
	// then refreshes) — re-fetch to see what actually landed afterward.
	got, err := svc.calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "DD's Work" {
		t.Fatalf("expected the picker's summaryOverride-aware name to survive the initial full refresh, got %q", got.Name)
	}
}

func TestConnectionService_ImportCalendars_InitialFullRefreshFailureDoesNotUndoTheCalendar(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	// eventsStatus is only forced *after* the calendar list + import calls
	// above succeeded once already — but ImportCalendars re-fetches
	// calendarList itself, so force the events endpoint specifically to fail
	// while everything else keeps working.
	google.eventsStatus = 401

	created, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{"primary"})
	if err != nil {
		t.Fatalf("expected ImportCalendars to succeed despite the refresh failing, got %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected the calendar to still be created, got %d", len(created))
	}

	got, err := svc.calendars.Get(context.Background(), userID, created[0].ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Source.ErrorClass == nil || *got.Source.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected the Source to carry a needs-attention error, got %+v", got.Source)
	}
}

func TestConnectionService_FullRefresh_CreatesMasterOverrideAndException(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			{
				"id":         "series-1",
				"summary":    "Standup",
				"start":      map[string]any{"dateTime": "2026-01-05T09:00:00-05:00", "timeZone": "America/New_York"},
				"end":        map[string]any{"dateTime": "2026-01-05T09:15:00-05:00", "timeZone": "America/New_York"},
				"recurrence": []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"},
			},
			{
				"id":                "series-1_20260112",
				"recurringEventId":  "series-1",
				"originalStartTime": map[string]any{"dateTime": "2026-01-12T09:00:00-05:00", "timeZone": "America/New_York"},
				"summary":           "Standup (moved)",
				"start":             map[string]any{"dateTime": "2026-01-12T10:00:00-05:00", "timeZone": "America/New_York"},
				"end":               map[string]any{"dateTime": "2026-01-12T10:15:00-05:00", "timeZone": "America/New_York"},
				"status":            "confirmed",
			},
			{
				"id":                "series-1_20260119",
				"recurringEventId":  "series-1",
				"originalStartTime": map[string]any{"dateTime": "2026-01-19T09:00:00-05:00", "timeZone": "America/New_York"},
				"status":            "cancelled",
			},
		},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, overridesByParent, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected 1 master, got %d", len(masters))
	}
	master := masters[0]
	if master.Rrule != "FREQ=WEEKLY;BYDAY=MO" {
		t.Fatalf("expected rrule to survive, got %q", master.Rrule)
	}
	if master.ExternalUID == nil || *master.ExternalUID != "series-1" {
		t.Fatalf("expected ExternalUID series-1, got %v", master.ExternalUID)
	}
	if len(master.Exdates) != 1 {
		t.Fatalf("expected 1 exdate from the cancelled instance, got %d", len(master.Exdates))
	}

	overrides := overridesByParent[master.ID]
	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(overrides))
	}
	if overrides[0].Title != "Standup (moved)" {
		t.Fatalf("expected override title, got %q", overrides[0].Title)
	}
}

func TestConnectionService_FullRefresh_StoresRSVPConferenceURLGuestCountAndEtag(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			{
				"id":      "evt-1",
				"etag":    `"the-etag"`,
				"summary": "Team lunch",
				"start":   map[string]any{"dateTime": "2026-04-01T12:00:00-04:00", "timeZone": "America/New_York"},
				"end":     map[string]any{"dateTime": "2026-04-01T13:00:00-04:00", "timeZone": "America/New_York"},
				"attendees": []map[string]any{
					{"self": true, "responseStatus": "accepted"},
					{"responseStatus": "needsAction"},
					{"resource": true, "responseStatus": "accepted"},
				},
				"conferenceData": map[string]any{
					"entryPoints": []map[string]any{
						{"entryPointType": "video", "uri": "https://meet.google.com/abc-defg-hij"},
					},
				},
			},
		},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected 1 master, got %d", len(masters))
	}
	m := masters[0]
	if m.RSVPStatus == nil || *m.RSVPStatus != "accepted" {
		t.Fatalf("expected RSVP accepted, got %v", m.RSVPStatus)
	}
	if m.ConferenceURL == nil || *m.ConferenceURL != "https://meet.google.com/abc-defg-hij" {
		t.Fatalf("expected conference URL, got %v", m.ConferenceURL)
	}
	if m.GuestCount != 1 {
		t.Fatalf("expected guest count 1, got %d", m.GuestCount)
	}
	if m.ProviderEtag == nil || *m.ProviderEtag != "the-etag" {
		t.Fatalf("expected provider etag stored with quotes stripped, got %v", m.ProviderEtag)
	}
}

func TestConnectionService_FullRefresh_PaginatesThroughEveryPage(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	var items []map[string]any
	for i := 0; i < 5; i++ {
		id := "evt-" + string(rune('a'+i))
		items = append(items, googleEventItem(id, "Event "+string(rune('A'+i)), "2026-01-0"+string(rune('1'+i))+"T10:00:00-05:00", "2026-01-0"+string(rune('1'+i))+"T11:00:00-05:00", "America/New_York"))
	}
	google.eventsByCalendar = map[string][]map[string]any{"primary": items}
	google.eventsPageSize = 2

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 5 {
		t.Fatalf("expected all 5 events across every page to be fetched, got %d", len(masters))
	}
}

func TestConnectionService_FullRefresh_SecondRunReconciles(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	// Google renames the event and adds a second one; the stored series
	// should update in place (same master id) and the new one should be
	// created — Full mode's ordinary upsert path (ADR-0053).
	google.eventsByCalendar["primary"] = []map[string]any{
		googleEventItem("evt-1", "Dentist (rescheduled)", "2026-01-16T10:00:00-05:00", "2026-01-16T11:00:00-05:00", "America/New_York"),
		googleEventItem("evt-2", "Follow-up", "2026-01-17T10:00:00-05:00", "2026-01-17T11:00:00-05:00", "America/New_York"),
	}

	result, err := svc.FullRefresh(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	if result.Updated != 1 || result.Created != 1 {
		t.Fatalf("expected 1 updated and 1 created, got %+v", result)
	}

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 2 {
		t.Fatalf("expected 2 masters, got %d", len(masters))
	}
}

func TestConnectionService_FullRefresh_AbsentEventIsTombstoned(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	google.eventsByCalendar["primary"] = nil

	result, err := svc.FullRefresh(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	if result.Tombstoned != 1 {
		t.Fatalf("expected 1 tombstoned (Full mode: absence means deletion), got %+v", result)
	}

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 0 {
		t.Fatalf("expected no masters left, got %d", len(masters))
	}
}

func TestConnectionService_FullRefresh_UnmappableRecurringInstanceIsNeverTombstoned(t *testing.T) {
	// Regression test for ADR-0053/ADR-0050's own "present but unparseable
	// is never a reason to tombstone", specifically for a recurring
	// instance's master somehow missing from a fetch (google_mapper.go's
	// defensive orphan case) — the stored series must survive untouched.
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			{
				"id":         "series-1",
				"summary":    "Standup",
				"start":      map[string]any{"dateTime": "2026-01-05T09:00:00-05:00", "timeZone": "America/New_York"},
				"end":        map[string]any{"dateTime": "2026-01-05T09:15:00-05:00", "timeZone": "America/New_York"},
				"recurrence": []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"},
			},
		},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	// Next fetch names an instance of series-1 but, pathologically, never
	// includes series-1's own master row.
	google.eventsByCalendar["primary"] = []map[string]any{
		{
			"id":                "series-1_20260112",
			"recurringEventId":  "series-1",
			"originalStartTime": map[string]any{"dateTime": "2026-01-12T09:00:00-05:00", "timeZone": "America/New_York"},
			"summary":           "Standup (moved)",
			"start":             map[string]any{"dateTime": "2026-01-12T10:00:00-05:00", "timeZone": "America/New_York"},
			"end":               map[string]any{"dateTime": "2026-01-12T10:15:00-05:00", "timeZone": "America/New_York"},
			"status":            "confirmed",
		},
	}

	result, err := svc.FullRefresh(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	if result.Tombstoned != 0 {
		t.Fatalf("expected zero tombstones for an orphaned instance, got %+v", result)
	}
	if result.Unparseable != 1 {
		t.Fatalf("expected the series to be counted as unparseable/skipped, got %+v", result)
	}

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected the original series to still be stored untouched, got %d", len(masters))
	}
	if masters[0].Title != "Standup" {
		t.Fatalf("expected the master's original content untouched, got %q", masters[0].Title)
	}
}

func TestConnectionService_FullRefresh_UnsupportedRecurrenceLinesAreDroppedAndCounted(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {
			{
				"id":      "series-1",
				"summary": "Irregular meeting",
				"start":   map[string]any{"dateTime": "2026-02-01T09:00:00-05:00", "timeZone": "America/New_York"},
				"end":     map[string]any{"dateTime": "2026-02-01T09:30:00-05:00", "timeZone": "America/New_York"},
				"recurrence": []string{
					"RRULE:FREQ=WEEKLY",
					"RDATE:20260203T140000Z",
				},
			},
		},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	connectionID := connectUser(t, svc, auth, userID)

	result, err := svc.ImportCalendars(context.Background(), userID, workspaceID, connectionID, []string{"primary"})
	if err != nil {
		t.Fatalf("import calendars: %v", err)
	}

	refreshResult, err := svc.FullRefresh(context.Background(), userID, result[0].ID)
	if err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	if refreshResult.DroppedRecurrenceLines != 1 {
		t.Fatalf("expected 1 dropped recurrence line surfaced, got %+v", refreshResult)
	}
}

func TestConnectionService_FullRefresh_RetriesOnceWithAFreshTokenAfter401(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	// The cached access_token from Connect/Callback has "expired" by the
	// time this Full Refresh runs — the first /events call must fail with
	// 401 before doFullRefresh mints a fresh one and retries.
	google.eventsFailFirstNCalls = 1

	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected the retry to still fetch the event after the first 401, got %d", len(masters))
	}
	if google.eventsCallCount != 2 {
		t.Fatalf("expected exactly 2 calls to /events (the failed one plus the retry), got %d", google.eventsCallCount)
	}
}

func TestConnectionService_FullRefresh_RevokedRefreshTokenIsNeedsAttention(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "someone@gmail.com", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {googleEventItem("evt-1", "Dentist", "2026-01-15T10:00:00-05:00", "2026-01-15T11:00:00-05:00", "America/New_York")},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	// The cached token is now rejected (expired) and the refresh grant
	// itself fails with Google's standard 400 invalid_grant for a revoked
	// refresh_token. eventsCallCount is reset so eventsFailFirstNCalls
	// counts from this call alone, not the initial import's own successful
	// one.
	google.eventsCallCount = 0
	google.eventsFailFirstNCalls = 1
	google.tokenStatus = 400

	if _, err := svc.FullRefresh(context.Background(), userID, calendar.ID); err == nil {
		t.Fatalf("expected the refresh to fail")
	}

	got, err := svc.calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Source.ErrorClass == nil || *got.Source.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected a revoked refresh_token (400) to be classified needs-attention, got %+v", got.Source)
	}

	// #291: the same dead grant also moves the Connection itself into
	// Expired — EventService.requireLiveConnection is what a Linked
	// Calendar's next edit attempt consults, and it has nothing to do with
	// this Source's own error_class/error_message.
	conn, err := svc.connections.GetByID(context.Background(), userID, *got.Source.ConnectionID)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.Status != repository.ConnectionStatusExpired {
		t.Fatalf("expected the connection recorded expired, got %q", conn.Status)
	}
}

func TestConnectionService_FullRefresh_NotFoundForNonLinkedCalendar(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, _, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)

	ownCalendar, err := svc.calendars.Create(context.Background(), userID, workspaceID, "uuid-1", CalendarWrite{Name: "Personal", Color: "#FF0000FF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	if _, err := svc.FullRefresh(context.Background(), userID, ownCalendar.ID); !errors.Is(err, ErrRefreshNotLinked) {
		t.Fatalf("expected ErrRefreshNotLinked, got %v", err)
	}
}

// --- #289: Linked Calendar presentation is local ---

// setEventColorDirectly stands in for a future Write-back's local recolour
// (#290+ — not built yet) so these tests can exercise "an Event recoloured
// here survives a Refresh" without it: it writes straight through the
// repository, exactly the shape a real local edit would leave behind (Color
// changed, ProviderColor — the shadow — untouched).
func setEventColorDirectly(t *testing.T, svc *ConnectionService, eventID string, color *string) {
	t.Helper()
	ctx := context.Background()

	existing, err := svc.events.events.GetByID(ctx, eventID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	fields := repository.EventFields{
		CalendarID: existing.CalendarID, Title: existing.Title, Start: existing.Start, End: existing.End,
		AllDay: existing.AllDay, Rrule: existing.Rrule, Tzid: existing.Tzid,
		Description: existing.Description, Location: existing.Location, URL: existing.URL,
		Color:         color,
		ProviderEtag:  existing.ProviderEtag,
		RSVPStatus:    existing.RSVPStatus,
		ConferenceURL: existing.ConferenceURL,
		GuestCount:    existing.GuestCount,
		ProviderColor: existing.ProviderColor,
	}
	if _, err := svc.events.events.Update(ctx, eventID, fields, existing.ChangeSeq, existing.Sequence); err != nil {
		t.Fatalf("set event color directly: %v", err)
	}
}

func TestConnectionService_FullRefresh_SeedsEventColorFromColorID(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Work", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {{
			"id":      "evt-1",
			"summary": "Tomato meeting",
			"colorId": "11",
			"start":   map[string]any{"dateTime": "2026-01-15T10:00:00-05:00", "timeZone": "America/New_York"},
			"end":     map[string]any{"dateTime": "2026-01-15T11:00:00-05:00", "timeZone": "America/New_York"},
		}},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected 1 event, got %d", len(masters))
	}
	if masters[0].Color == nil || *masters[0].Color != "#DC2127FF" {
		t.Fatalf("expected Color seeded from colorId 11, got %v", masters[0].Color)
	}
	if masters[0].ProviderColor == nil || *masters[0].ProviderColor != "#DC2127FF" {
		t.Fatalf("expected ProviderColor (the shadow) seeded identically, got %v", masters[0].ProviderColor)
	}
}

func TestConnectionService_FullRefresh_UntouchedEventFollowsProviderRecolour(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Work", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {{
			"id": "evt-1", "summary": "Meeting", "colorId": "5",
			"start": map[string]any{"dateTime": "2026-01-15T10:00:00-05:00", "timeZone": "America/New_York"},
			"end":   map[string]any{"dateTime": "2026-01-15T11:00:00-05:00", "timeZone": "America/New_York"},
		}},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	// The Provider recolours the same event (colorId 5 -> 11) before the
	// next Full Refresh.
	google.eventsByCalendar["primary"][0]["colorId"] = "11"

	if _, err := svc.FullRefresh(context.Background(), userID, calendar.ID); err != nil {
		t.Fatalf("second full refresh: %v", err)
	}

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected 1 event, got %d", len(masters))
	}
	if masters[0].Color == nil || *masters[0].Color != "#DC2127FF" {
		t.Fatalf("expected the untouched Event to follow the Provider's recolour, got %v", masters[0].Color)
	}
}

func TestConnectionService_FullRefresh_LocallyRecolouredEventSurvivesProviderColourUnchanged(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Work", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{
		"primary": {{
			"id": "evt-1", "summary": "Meeting", "colorId": "5",
			"start": map[string]any{"dateTime": "2026-01-15T10:00:00-05:00", "timeZone": "America/New_York"},
			"end":   map[string]any{"dateTime": "2026-01-15T11:00:00-05:00", "timeZone": "America/New_York"},
		}},
	}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	masters, _, err := svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	customColor := "#123456FF"
	setEventColorDirectly(t, svc, masters[0].ID, &customColor)

	// The Provider's own colour (colorId 5) is unchanged on the next Refresh.
	if _, err := svc.FullRefresh(context.Background(), userID, calendar.ID); err != nil {
		t.Fatalf("second full refresh: %v", err)
	}

	masters, _, err = svc.events.ListSeriesByCalendar(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if masters[0].Color == nil || *masters[0].Color != customColor {
		t.Fatalf("expected the local recolour to survive an unchanged Provider colour, got %v", masters[0].Color)
	}
}

func TestConnectionService_FullRefresh_CalendarNameFollowsProviderRename(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Family", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {}}
	google.eventsSummaryByCalendar = map[string]string{"primary": "Family"}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	if calendar.Name != "Family" {
		t.Fatalf("expected the picker's own name at import, got %q", calendar.Name)
	}

	// Nobody has renamed the Calendar here — a rename at the Provider must
	// reach it.
	google.eventsSummaryByCalendar["primary"] = "Family (renamed)"
	google.calendarListItems[0]["summary"] = "Family (renamed)"

	if _, err := svc.FullRefresh(context.Background(), userID, calendar.ID); err != nil {
		t.Fatalf("second full refresh: %v", err)
	}

	got, err := svc.calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "Family (renamed)" {
		t.Fatalf("expected the untouched Calendar to follow the Provider's rename, got %q", got.Name)
	}
}

func TestConnectionService_FullRefresh_RenamedCalendarHereKeepsItsNameAcrossLaterRefreshes(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Family", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {}}
	google.eventsSummaryByCalendar = map[string]string{"primary": "Family"}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")

	// The User renames the Linked Calendar here.
	renamed, err := svc.calendars.Update(context.Background(), userID, calendar.ID, CalendarWrite{Name: "Work", Color: calendar.Color}, false, false)
	if err != nil {
		t.Fatalf("rename calendar: %v", err)
	}
	if renamed.Name != "Work" {
		t.Fatalf("expected the rename to take, got %q", renamed.Name)
	}

	// The Provider's own name is unrelated and unchanged — the local rename
	// must survive regardless.
	if _, err := svc.FullRefresh(context.Background(), userID, calendar.ID); err != nil {
		t.Fatalf("full refresh: %v", err)
	}
	got, err := svc.calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "Work" {
		t.Fatalf("expected the local rename to survive, got %q", got.Name)
	}

	// The Provider now also renames its own calendar — the local rename must
	// still win, and the rename never reaches the Provider (there is no
	// Write-back yet — #290+ — so nothing could push it even if this failed
	// silently; the fake server's own calendarList fixture is untouched by
	// this Refresh, which is the other half of that guarantee).
	google.eventsSummaryByCalendar["primary"] = "Family (renamed at Google too)"
	if _, err := svc.FullRefresh(context.Background(), userID, calendar.ID); err != nil {
		t.Fatalf("third full refresh: %v", err)
	}
	got, err = svc.calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "Work" {
		t.Fatalf("expected the local rename to keep winning over a later Provider rename, got %q", got.Name)
	}
}

func TestConnectionService_FullRefresh_CalendarColorIgnoresLaterProviderRecolours(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.calendarListItems = []map[string]any{googleCalendarItem("primary", "Work", "#0b8043", "owner", true)}
	google.eventsByCalendar = map[string][]map[string]any{"primary": {}}

	svc, auth, userID, workspaceID := newTestConnectionServiceWithWorkspace(t, google)
	calendar := importOneCalendar(t, svc, auth, userID, workspaceID, "primary")
	seededColor := calendar.Color

	// A later Refresh must never move the Calendar's own colour — there is no
	// publisher value to track beneath it (ADR-0052), unlike the name.
	if _, err := svc.FullRefresh(context.Background(), userID, calendar.ID); err != nil {
		t.Fatalf("second full refresh: %v", err)
	}

	got, err := svc.calendars.Get(context.Background(), userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Color != seededColor {
		t.Fatalf("expected the Calendar's colour to stay exactly as seeded at import (%q), got %q", seededColor, got.Color)
	}
}
