package caldavserver

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// rawPut issues a PUT request directly (bypassing caldav.Client, which
// doesn't support conditional headers) so If-Match/If-None-Match can be
// exercised.
func rawPut(t *testing.T, env testCalDAVEnv, path string, cal *ical.Calendar, ifMatch string) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		t.Fatalf("encode calendar: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, env.srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("build PUT request: %v", err)
	}
	req.Header.Set("Content-Type", ical.MIMEType)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	req.SetBasicAuth("admin@example.com", env.appPasswordSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

// rawDelete issues a DELETE request directly (bypassing caldav.Client's
// RemoveAll, which doesn't support conditional headers) so If-Match can be
// exercised.
func rawDelete(t *testing.T, env testCalDAVEnv, path, ifMatch string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodDelete, env.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build DELETE request: %v", err)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	req.SetBasicAuth("admin@example.com", env.appPasswordSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

func newTestCalDAVClient(t *testing.T, env testCalDAVEnv) *caldav.Client {
	t.Helper()
	httpClient := webdav.HTTPClientWithBasicAuth(env.srv.Client(), "admin@example.com", env.appPasswordSecret)
	client, err := caldav.NewClient(httpClient, env.srv.URL)
	if err != nil {
		t.Fatalf("new caldav client: %v", err)
	}
	return client
}

func uidOf(t *testing.T, cal *ical.Calendar) string {
	t.Helper()
	events := cal.Events()
	if len(events) == 0 {
		t.Fatalf("expected at least one VEVENT")
	}
	uid, err := events[0].Props.Text(ical.PropUID)
	if err != nil {
		t.Fatalf("read UID: %v", err)
	}
	return uid
}

func TestGetCalendarObject_NonRecurringEvent(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, created.ID)

	obj, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}
	if obj.ETag == "" {
		t.Fatalf("expected a non-empty ETag")
	}
	if uid := uidOf(t, obj.Data); uid != "evt-1" {
		t.Fatalf("expected UID evt-1, got %q", uid)
	}

	// A second GET of the unchanged Event must produce the same ETag —
	// derived from the reconstruction, not request-local state (ADR-0026).
	again, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("second GetCalendarObject: %v", err)
	}
	if again.ETag != obj.ETag {
		t.Fatalf("expected a stable ETag across GETs, got %q then %q", obj.ETag, again.ETag)
	}
}

func TestGetCalendarObject_UnknownID_Returns404(t *testing.T) {
	env := newTestCalDAVEnv(t)
	client := newTestCalDAVClient(t, env)

	_, err := client.GetCalendarObject(context.Background(), calendarObjectPath(env.userID, env.calendarID, "does-not-exist"))
	if err == nil {
		t.Fatalf("expected an error for an unknown calendar object")
	}
}

func TestGetCalendarObject_OverrideID_Returns404(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	master, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	override, err := env.eventService.Create(ctx, env.userID, "evt-1-override", service.EventWrite{CalendarID: env.calendarID, Title: "Standup (moved)", Start: time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	_, err = client.GetCalendarObject(ctx, calendarObjectPath(env.userID, env.calendarID, override.ID))
	if err == nil {
		t.Fatalf("expected an error fetching an Override's id directly")
	}
}

func TestGetCalendarObject_RecurringSeries_IncludesOverrideAndExdate(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	master, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-1-override", service.EventWrite{CalendarID: env.calendarID, Title: "Standup (moved)", Start: time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	if err := env.eventService.AddException(ctx, env.userID, master.ID, time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	obj, err := client.GetCalendarObject(ctx, calendarObjectPath(env.userID, env.calendarID, master.ID))
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}

	events := obj.Data.Events()
	if len(events) != 2 {
		t.Fatalf("expected master + override VEVENTs, got %d", len(events))
	}
	for _, e := range events {
		uid, err := e.Props.Text(ical.PropUID)
		if err != nil || uid != "evt-1" {
			t.Fatalf("expected every VEVENT to share the master's UID, got %q (err %v)", uid, err)
		}
	}

	masterVEvent := events[0]
	if len(masterVEvent.Props[ical.PropExceptionDates]) != 1 {
		t.Fatalf("expected the master VEVENT to carry one EXDATE, got %v", masterVEvent.Props[ical.PropExceptionDates])
	}
}

func TestCalendarQuery_TimeRange_ReturnsOnlySeriesWithOccurrenceInRange(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	inRange, err := env.eventService.Create(ctx, env.userID, "evt-in-range", service.EventWrite{CalendarID: env.calendarID, Title: "In range", Start: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 10, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create in-range event: %v", err)
	}
	if _, err := env.eventService.Create(ctx, env.userID, "evt-out-of-range", service.EventWrite{CalendarID: env.calendarID, Title: "Out of range", Start: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 7, 10, 9, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("create out-of-range event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	results, err := client.QueryCalendar(ctx, calendarPath(env.userID, env.calendarID), &caldav.CalendarQuery{
		CompFilter: caldav.CompFilter{
			Name: ical.CompCalendar,
			Comps: []caldav.CompFilter{
				{
					Name:  ical.CompEvent,
					Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
					End:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("QueryCalendar: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected exactly one series in range, got %d", len(results))
	}
	if uid := uidOf(t, results[0].Data); uid != inRange.ID {
		t.Fatalf("expected the in-range series %q, got %q", inRange.ID, uid)
	}
}

func TestCalendarMultiget_ReturnsRequestedObjects(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	first, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "First", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create first event: %v", err)
	}
	second, err := env.eventService.Create(ctx, env.userID, "evt-2", service.EventWrite{CalendarID: env.calendarID, Title: "Second", Start: time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create second event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	firstPath := calendarObjectPath(env.userID, env.calendarID, first.ID)
	secondPath := calendarObjectPath(env.userID, env.calendarID, second.ID)

	results, err := client.MultiGetCalendar(ctx, calendarPath(env.userID, env.calendarID), &caldav.CalendarMultiGet{
		Paths: []string{firstPath, secondPath},
	})
	if err != nil {
		t.Fatalf("MultiGetCalendar: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected both requested objects, got %d", len(results))
	}

	gotUIDs := map[string]bool{}
	for _, obj := range results {
		gotUIDs[uidOf(t, obj.Data)] = true
	}
	if !gotUIDs["evt-1"] || !gotUIDs["evt-2"] {
		t.Fatalf("expected UIDs evt-1 and evt-2, got %v", gotUIDs)
	}
}

func TestPutCalendarObject_CreatesNewEvent_PutThenGetRoundTrips(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	newEvent := repository.Event{
		ID:        "device-uid-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	cal, err := icalendar.SeriesToICal(newEvent, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, "device-uid-1")

	putResult, err := client.PutCalendarObject(ctx, path, cal)
	if err != nil {
		t.Fatalf("PutCalendarObject: %v", err)
	}
	if putResult.ETag == "" {
		t.Fatalf("expected a non-empty ETag from PUT")
	}

	got, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}
	if got.ETag != putResult.ETag {
		t.Fatalf("expected the PUT response ETag to match the next GET's, got %q then %q", putResult.ETag, got.ETag)
	}
	if uid := uidOf(t, got.Data); uid != "device-uid-1" {
		t.Fatalf("expected the client-authored UID to become the master's id, got %q", uid)
	}

	stored, err := env.eventService.Get(ctx, env.userID, "device-uid-1")
	if err != nil {
		t.Fatalf("get stored event: %v", err)
	}
	if stored.Title != "Standup" {
		t.Fatalf("expected the web app to see the device-authored event, got %+v", stored)
	}
}

func TestPutCalendarObject_UpdatesExistingEvent(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	updated := created
	updated.Title = "Standup (renamed)"
	cal, err := icalendar.SeriesToICal(updated, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, "evt-1")
	if _, err := client.PutCalendarObject(ctx, path, cal); err != nil {
		t.Fatalf("PutCalendarObject: %v", err)
	}

	stored, err := env.eventService.Get(ctx, env.userID, "evt-1")
	if err != nil {
		t.Fatalf("get stored event: %v", err)
	}
	if stored.Title != "Standup (renamed)" {
		t.Fatalf("expected the edit to take effect, got %+v", stored)
	}
}

func TestPutCalendarObject_EditingOneOccurrence_CreatesOverride_OthersUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := env.eventService.Create(ctx, env.userID, "evt-master", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := masterStart.AddDate(0, 0, 7)
	override := repository.Event{
		ID:           "evt-override-placeholder",
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
		Title:        "Standup (moved)",
		Start:        recurrenceID.Add(2 * time.Hour),
		End:          recurrenceID.Add(2*time.Hour + 30*time.Minute),
		CreatedAt:    masterStart,
	}
	cal, err := icalendar.SeriesToICal(master, []repository.Event{override}, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)
	if _, err := client.PutCalendarObject(ctx, path, cal); err != nil {
		t.Fatalf("PutCalendarObject: %v", err)
	}

	storedMaster, overrides, err := env.eventService.GetSeries(ctx, env.userID, master.ID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if storedMaster.Title != "Standup" || storedMaster.Rrule != "FREQ=WEEKLY;BYDAY=TU" {
		t.Fatalf("expected the series' other occurrences (the master's own pattern) to be unchanged, got %+v", storedMaster)
	}
	if len(overrides) != 1 || overrides[0].Title != "Standup (moved)" {
		t.Fatalf("expected exactly one override with the edited title, got %+v", overrides)
	}
}

func TestPutCalendarObject_IfMatch_StaleETag_Returns412(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	updated := created
	updated.Title = "Standup (renamed)"
	cal, err := icalendar.SeriesToICal(updated, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}

	path := calendarObjectPath(env.userID, env.calendarID, "evt-1")
	resp := rawPut(t, env, path, cal, `"stale-etag"`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 for a stale If-Match, got %d", resp.StatusCode)
	}

	stored, err := env.eventService.Get(ctx, env.userID, "evt-1")
	if err != nil {
		t.Fatalf("get stored event: %v", err)
	}
	if stored.Title != "Standup" {
		t.Fatalf("expected the rejected PUT to leave the event unchanged, got %+v", stored)
	}
}

func TestPutCalendarObject_IfMatch_MatchingETag_Succeeds(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, "evt-1")
	current, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}

	updated := created
	updated.Title = "Standup (renamed)"
	cal, err := icalendar.SeriesToICal(updated, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}

	resp := rawPut(t, env, path, cal, `"`+current.ETag+`"`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected a matching If-Match to succeed, got %d", resp.StatusCode)
	}

	stored, err := env.eventService.Get(ctx, env.userID, "evt-1")
	if err != nil {
		t.Fatalf("get stored event: %v", err)
	}
	if stored.Title != "Standup (renamed)" {
		t.Fatalf("expected the edit to take effect, got %+v", stored)
	}
}

func TestPutCalendarObject_UnmodeledData_NormalizedAway_StableETag(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	newEvent := repository.Event{
		ID:        "device-uid-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	cal, err := icalendar.SeriesToICal(newEvent, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	// CATEGORIES isn't modeled by this app (ADR-0026) — it must be silently
	// dropped, not rejected, and must not cause the PUT-response ETag to
	// mismatch the next GET's.
	cal.Children[0].Props.SetText("CATEGORIES", "Work")

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, "device-uid-1")

	putResult, err := client.PutCalendarObject(ctx, path, cal)
	if err != nil {
		t.Fatalf("PutCalendarObject: %v", err)
	}

	got, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}
	if got.ETag != putResult.ETag {
		t.Fatalf("expected the PUT response ETag to match the next GET's despite normalization, got %q then %q", putResult.ETag, got.ETag)
	}
}

func TestDeleteCalendarObject_DeletesSeriesIncludingOverridesAndExceptions(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	masterStart := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	master, err := env.eventService.Create(ctx, env.userID, "evt-master", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: masterStart, End: masterStart.Add(30 * time.Minute), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := masterStart.AddDate(0, 0, 7)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-override", service.EventWrite{CalendarID: env.calendarID, Title: "Standup (moved)", Start: recurrenceID.Add(2 * time.Hour), End: recurrenceID.Add(2*time.Hour + 30*time.Minute), ParentID: &master.ID, RecurrenceID: &recurrenceID}); err != nil {
		t.Fatalf("create override: %v", err)
	}
	if err := env.eventService.AddException(ctx, env.userID, master.ID, masterStart.AddDate(0, 0, 14)); err != nil {
		t.Fatalf("add exception: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)
	if err := client.RemoveAll(ctx, path); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := env.eventService.Get(ctx, env.userID, master.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the master to be deleted, got err=%v", err)
	}
	if _, err := env.eventService.Get(ctx, env.userID, "evt-override"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the cascaded override to be deleted, got err=%v", err)
	}
}

func TestDeleteCalendarObject_UnknownID_Returns404(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	client := newTestCalDAVClient(t, env)
	err := client.RemoveAll(ctx, calendarObjectPath(env.userID, env.calendarID, "does-not-exist"))
	if err == nil {
		t.Fatalf("expected an error deleting an unknown calendar object")
	}
}

func TestDeleteCalendarObject_OverrideID_Returns404(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	master, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=WEEKLY;BYDAY=TU"})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	override, err := env.eventService.Create(ctx, env.userID, "evt-1-override", service.EventWrite{CalendarID: env.calendarID, Title: "Standup (moved)", Start: time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC), ParentID: &master.ID, RecurrenceID: &recurrenceID})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	err = client.RemoveAll(ctx, calendarObjectPath(env.userID, env.calendarID, override.ID))
	if err == nil {
		t.Fatalf("expected an error deleting an Override's id directly")
	}

	if _, err := env.eventService.Get(ctx, env.userID, override.ID); err != nil {
		t.Fatalf("expected the override to survive a rejected delete of its id, got err=%v", err)
	}
}

func TestDeleteCalendarObject_IfMatch_StaleETag_Returns412(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarObjectPath(env.userID, env.calendarID, "evt-1")
	resp := rawDelete(t, env, path, `"stale-etag"`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 for a stale If-Match, got %d", resp.StatusCode)
	}

	if _, err := env.eventService.Get(ctx, env.userID, created.ID); err != nil {
		t.Fatalf("expected the rejected DELETE to leave the event in place, got err=%v", err)
	}
}

func TestDeleteCalendarObject_IfMatch_MatchingETag_Succeeds(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	if _, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	client := newTestCalDAVClient(t, env)
	path := calendarObjectPath(env.userID, env.calendarID, "evt-1")
	current, err := client.GetCalendarObject(ctx, path)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}

	resp := rawDelete(t, env, path, `"`+current.ETag+`"`)
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		t.Fatalf("expected a matching If-Match to succeed, got %d", resp.StatusCode)
	}

	if _, err := env.eventService.Get(ctx, env.userID, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the event to be deleted, got err=%v", err)
	}
}

func TestDeleteCalendarObject_IfMatch_Wildcard_Succeeds(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	if _, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarObjectPath(env.userID, env.calendarID, "evt-1")
	resp := rawDelete(t, env, path, "*")
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		t.Fatalf("expected a wildcard If-Match to succeed against any existing object, got %d", resp.StatusCode)
	}

	if _, err := env.eventService.Get(ctx, env.userID, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the event to be deleted, got err=%v", err)
	}
}
