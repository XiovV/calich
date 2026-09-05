package caldavserver

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

// putSeriesStatus PUTs one series and reports the raw status, so the mapping
// from a service validation error to an HTTP status can be asserted directly
// (caldav.Client hides the status behind a generic error).
func putSeriesStatus(t *testing.T, env testCalDAVEnv, calendarID string, master repository.Event) int {
	t.Helper()

	cal, _, err := icalendar.SeriesToICal(master, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	resp := rawPut(t, env, calendarObjectPath(env.userID, calendarID, master.ID), cal, "")
	defer resp.Body.Close()
	return resp.StatusCode
}

func validPutMaster() repository.Event {
	return repository.Event{
		ID:        "device-uid-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
}

// A device can PUT a VEVENT this app refuses to store. Each rejection has to
// reach the client as the right status — a 500 would make a native client
// retry forever rather than surface the problem.
func TestPutCalendarObject_RejectsInvalidSeries(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*repository.Event)
		calendarID string
		want       int
	}{
		{
			name:   "empty summary",
			mutate: func(e *repository.Event) { e.Title = "" },
			want:   http.StatusBadRequest,
		},
		{
			name:   "end before start",
			mutate: func(e *repository.Event) { e.End = e.Start.Add(-time.Hour) },
			want:   http.StatusBadRequest,
		},
		{
			name:   "end equal to start",
			mutate: func(e *repository.Event) { e.End = e.Start },
			want:   http.StatusBadRequest,
		},
		{
			name:   "recurrence rule with no FREQ",
			mutate: func(e *repository.Event) { e.Rrule = "INTERVAL=2" },
			want:   http.StatusBadRequest,
		},
		{
			name:       "calendar that does not exist",
			mutate:     func(e *repository.Event) {},
			calendarID: "no-such-calendar",
			want:       http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestCalDAVEnv(t)

			master := validPutMaster()
			tt.mutate(&master)

			calendarID := tt.calendarID
			if calendarID == "" {
				calendarID = env.calendarID
			}

			if got := putSeriesStatus(t, env, calendarID, master); got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

// A rejected PUT must leave nothing behind — the write is refused before
// PutSeries opens its transaction.
func TestPutCalendarObject_RejectedSeriesIsNotStored(t *testing.T) {
	env := newTestCalDAVEnv(t)

	master := validPutMaster()
	master.Title = ""

	if got := putSeriesStatus(t, env, env.calendarID, master); got != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", got)
	}

	masters, _, err := env.eventService.ListSeriesByCalendar(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 0 {
		t.Fatalf("expected the rejected series not to be stored, got %+v", masters)
	}
}

// The UID inside the VCALENDAR is the resource's identity (ADR-0025), so a
// mismatch with the .ics name is a conflict rather than a silent rename.
func TestPutCalendarObject_UIDMismatchIsConflict(t *testing.T) {
	env := newTestCalDAVEnv(t)

	master := validPutMaster()
	cal, _, err := icalendar.SeriesToICal(master, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}

	resp := rawPut(t, env, calendarObjectPath(env.userID, env.calendarID, "a-different-name"), cal, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

// A VCALENDAR carrying no VEVENT cannot be decomposed into a series. It has
// to be sent as raw bytes: go-webdav's encoder refuses to produce an empty
// VCALENDAR, so rawPut can't build this request.
func TestPutCalendarObject_CalendarWithNoVEventIsRejected(t *testing.T) {
	env := newTestCalDAVEnv(t)

	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:" + icalendar.ProdID + "\r\nEND:VCALENDAR\r\n"

	req, err := http.NewRequest(http.MethodPut,
		env.srv.URL+calendarObjectPath(env.userID, env.calendarID, "device-uid-1"),
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("build PUT request: %v", err)
	}
	req.Header.Set("Content-Type", ical.MIMEType)
	req.SetBasicAuth("admin@example.com", env.appPasswordSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("expected a 4xx rejection, got %d", resp.StatusCode)
	}
}

// A Subscribed Calendar's collection is visible over CalDAV but read-only
// (ADR-0032): PUT on one of its objects is refused with 403, not 404 — the
// object (or its intended location) exists, the write is what's refused.
func TestPutCalendarObject_SubscribedCalendarIsForbidden(t *testing.T) {
	env := newTestCalDAVEnv(t)

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := env.calendarService.CreateSubscribed(t.Context(), env.userID, env.workspaceID, "sub-cal-1", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF",
	}, repository.SourceFields{Kind: repository.SourceKindSubscription, Mode: repository.SourceModeReadOnly, SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	if got := putSeriesStatus(t, env, subCalendar.ID, validPutMaster()); got != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", got)
	}
}

// DELETE on a Subscribed Calendar's object is refused with 403 for the same
// reason PUT is (ADR-0032) — the object stays untouched.
func TestDeleteCalendarObject_SubscribedCalendarIsForbidden(t *testing.T) {
	env := newTestCalDAVEnv(t)

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := env.calendarService.CreateSubscribed(t.Context(), env.userID, env.workspaceID, "sub-cal-1", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF",
	}, repository.SourceFields{Kind: repository.SourceKindSubscription, Mode: repository.SourceModeReadOnly, SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	master := validPutMaster()
	if _, err := env.eventService.ImportSubscribedSeries(t.Context(), env.userID, subCalendar.ID, []service.SeriesWrite{
		{Title: master.Title, Start: master.Start, End: master.End},
	}); err != nil {
		t.Fatalf("seed subscribed event: %v", err)
	}

	masters, _, err := env.eventService.ListSeriesByCalendar(t.Context(), env.userID, subCalendar.ID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(masters) != 1 {
		t.Fatalf("expected 1 master, got %+v", masters)
	}

	resp := rawDelete(t, env, calendarObjectPath(env.userID, subCalendar.ID, masters[0].ID), "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// putRawVEvent PUTs a hand-built one-VEVENT calendar object, so a test can
// send properties SeriesToICal never emits — such as a VEVENT with no DTEND
// at all.
func putRawVEvent(t *testing.T, env testCalDAVEnv, uid string, props func(*ical.Event)) int {
	t.Helper()

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, icalendar.ProdID)
	cal.Props.SetText(ical.PropVersion, "2.0")

	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, uid)
	v.Props.SetDateTime(ical.PropDateTimeStamp, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	v.Props.SetText(ical.PropSummary, "Bank holiday")
	props(v)
	cal.Children = append(cal.Children, v.Component)

	resp := rawPut(t, env, calendarObjectPath(env.userID, env.calendarID, uid), cal, "")
	defer resp.Body.Close()
	return resp.StatusCode
}

// A VEVENT with DTSTART and no DTEND is valid iCalendar, so the shared codec
// resolves its end the way RFC 5545 does — DURATION where the device sent
// one, else one day for a date-valued DTSTART (#228). This is the same decode
// ICS import uses, and a device's PUT gets the same answer rather than a 400.
func TestPutCalendarObject_DTStartWithoutDTEnd_ResolvedPerRFC5545(t *testing.T) {
	env := newTestCalDAVEnv(t)

	dateProp := ical.NewProp(ical.PropDateTimeStart)
	dateProp.SetValueType(ical.ValueDate)
	dateProp.Value = "20260601"
	if got := putRawVEvent(t, env, "all-day-no-dtend", func(v *ical.Event) {
		v.Props.Add(dateProp)
	}); got != http.StatusCreated && got != http.StatusNoContent {
		t.Fatalf("expected the all-day VEVENT to be stored, got %d", got)
	}

	if got := putRawVEvent(t, env, "timed-with-duration", func(v *ical.Event) {
		v.Props.SetDateTime(ical.PropDateTimeStart, time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC))
		duration := ical.NewProp(ical.PropDuration)
		duration.Value = "PT45M"
		v.Props.Add(duration)
	}); got != http.StatusCreated && got != http.StatusNoContent {
		t.Fatalf("expected the DURATION VEVENT to be stored, got %d", got)
	}

	masters, _, err := env.eventService.ListSeriesByCalendar(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	byID := make(map[string]repository.Event, len(masters))
	for _, m := range masters {
		byID[m.ID] = m
	}

	allDay, ok := byID["all-day-no-dtend"]
	if !ok {
		t.Fatalf("expected the all-day series to be stored, got %+v", masters)
	}
	if !allDay.AllDay || !allDay.End.Equal(allDay.Start.AddDate(0, 0, 1)) {
		t.Fatalf("expected a one-day all-day Event, got %+v", allDay)
	}

	timed, ok := byID["timed-with-duration"]
	if !ok {
		t.Fatalf("expected the DURATION series to be stored, got %+v", masters)
	}
	if want := timed.Start.Add(45 * time.Minute); !timed.End.Equal(want) {
		t.Fatalf("expected end %v from DURATION, got %v", want, timed.End)
	}
}
