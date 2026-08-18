package caldavserver

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/service"
)

const propfindObjectList = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop><d:getetag/><d:resourcetype/><d:getcontenttype/></d:prop>
</d:propfind>`

// A PROPFIND with Depth: 1 on a Calendar collection enumerates that
// Calendar's objects, one <response> per series plus one for the collection
// itself. This is how a native client lists a Calendar, and it is the only
// route to Backend.ListCalendarObjects — go-webdav reaches it through
// propFindAllCalendarObjects, never through the calendar-query path the other
// tests drive.
func TestPropfind_Depth1OnCalendar_ListsOneObjectPerSeries(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	first := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup",
		Start: first, End: first.Add(30 * time.Minute),
		Rrule: "FREQ=WEEKLY",
	}); err != nil {
		t.Fatalf("create master: %v", err)
	}

	// An Override is part of its Master's object, never its own resource
	// (ADR-0025), so it must not add a second <response>.
	recurrenceID := first.AddDate(0, 0, 7)
	master := "evt-1"
	if _, err := env.eventService.Create(ctx, env.userID, "evt-1-override", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup (moved)",
		Start: recurrenceID.Add(time.Hour), End: recurrenceID.Add(90 * time.Minute),
		ParentID: &master, RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	second := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-2", service.EventWrite{
		CalendarID: env.calendarID, Title: "One-off",
		Start: second, End: second.Add(time.Hour),
	}); err != nil {
		t.Fatalf("create standalone: %v", err)
	}

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "1", propfindObjectList)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	got := string(body)

	for _, want := range []string{
		calendarObjectPath(env.userID, env.calendarID, "evt-1"),
		calendarObjectPath(env.userID, env.calendarID, "evt-2"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected an <href> for %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "evt-1-override") {
		t.Fatalf("an Override must not be listed as its own resource, got:\n%s", got)
	}
	// The two series, plus the collection itself.
	if n := strings.Count(got, "<response"); n != 3 {
		t.Fatalf("expected 3 <response> elements (collection + 2 series), got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "getetag") {
		t.Fatalf("expected each object to carry an ETag, got:\n%s", got)
	}
}

// A Depth: 1 PROPFIND on a Calendar that isn't the authenticated user's
// resolves to no collection at all.
func TestPropfind_Depth1OnUnknownCalendar_IsNotFound(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, "does-not-exist")
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "1", propfindObjectList)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
