package caldavserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/service"
)

const propfindGetCTag = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:prop><cs:getctag/></d:prop>
</d:propfind>`

func extractGetCTag(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<getctag")
	if start == -1 {
		t.Fatalf("no getctag in response:\n%s", body)
	}
	rest := body[start:]
	open := strings.Index(rest, ">") + 1
	end := strings.Index(rest, "</getctag>")
	if end == -1 {
		t.Fatalf("malformed getctag in response:\n%s", body)
	}
	return rest[open:end]
}

func TestPropfind_GetCTag_ExposedAsA200Property(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindGetCTag)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "404") {
		t.Fatalf("expected getctag to be served with a 200 status, got:\n%s", body)
	}
	extractGetCTag(t, body) // fails the test itself if absent
}

func TestPropfind_GetCTag_StableUntilCalendarMutates(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	path := calendarPath(env.userID, env.calendarID)

	resp1 := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindGetCTag)
	ctag1 := extractGetCTag(t, readBody(t, resp1))
	resp1.Body.Close()

	resp2 := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindGetCTag)
	ctag2 := extractGetCTag(t, readBody(t, resp2))
	resp2.Body.Close()
	if ctag1 != ctag2 {
		t.Fatalf("expected a stable ctag when nothing changed, got %q then %q", ctag1, ctag2)
	}

	if _, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	resp3 := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindGetCTag)
	defer resp3.Body.Close()
	ctag3 := extractGetCTag(t, readBody(t, resp3))
	if ctag3 == ctag2 {
		t.Fatalf("expected the ctag to change after a mutation, still %q", ctag3)
	}
}
