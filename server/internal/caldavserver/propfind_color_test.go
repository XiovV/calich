package caldavserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

const propfindCalendarColor = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:" xmlns:ic="http://apple.com/ns/ical/">
  <d:prop><ic:calendar-color/></d:prop>
</d:propfind>`

const propfindGetCTagAndCalendarColor = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/" xmlns:ic="http://apple.com/ns/ical/">
  <d:prop><cs:getctag/><ic:calendar-color/></d:prop>
</d:propfind>`

func extractCalendarColor(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<calendar-color")
	if start == -1 {
		t.Fatalf("no calendar-color in response:\n%s", body)
	}
	rest := body[start:]
	open := strings.Index(rest, ">") + 1
	end := strings.Index(rest, "</calendar-color>")
	if end == -1 {
		t.Fatalf("malformed calendar-color in response:\n%s", body)
	}
	return rest[open:end]
}

func TestPropfind_CalendarColor_ExposedAsA200Property(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCalendarColor)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "404") {
		t.Fatalf("expected calendar-color to be served with a 200 status, got:\n%s", body)
	}
	extractCalendarColor(t, body) // fails the test itself if absent
}

func TestPropfind_CalendarColor_ReflectsStoredColor(t *testing.T) {
	env := newTestCalDAVEnv(t)

	// newTestCalDAVEnv creates the test calendar with color "#12809CFF".
	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCalendarColor)
	defer resp.Body.Close()

	got := extractCalendarColor(t, readBody(t, resp))
	if got != "#12809CFF" {
		t.Fatalf("expected the stored hex #12809CFF, got %q", got)
	}
}

func TestPropfind_CalendarColor_StableAcrossRepeatedGETs_ChangesAfterUpdate(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	path := calendarPath(env.userID, env.calendarID)

	resp1 := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCalendarColor)
	color1 := extractCalendarColor(t, readBody(t, resp1))
	resp1.Body.Close()

	resp2 := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCalendarColor)
	color2 := extractCalendarColor(t, readBody(t, resp2))
	resp2.Body.Close()
	if color1 != color2 {
		t.Fatalf("expected a stable color when nothing changed, got %q then %q", color1, color2)
	}

	if _, err := env.calendarService.Update(ctx, env.userID, env.calendarID, service.CalendarWrite{Name: "Personal", Color: "#8E44ADFF"}); err != nil {
		t.Fatalf("update calendar color: %v", err)
	}

	resp3 := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCalendarColor)
	defer resp3.Body.Close()
	color3 := extractCalendarColor(t, readBody(t, resp3))
	if color3 != "#8E44ADFF" {
		t.Fatalf("expected the updated hex #8E44ADFF, got %q", color3)
	}
}

func TestPropfind_BothGetCTagAndCalendarColor_BothExposedInOnePass(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindGetCTagAndCalendarColor)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Count(body, "<multistatus") > 1 {
		t.Fatalf("expected exactly one multistatus root, got:\n%s", body)
	}
	extractGetCTag(t, body)
	color := extractCalendarColor(t, body)
	if color != "#12809CFF" {
		t.Fatalf("expected the stored hex #12809CFF, got %q", color)
	}
}

func TestPropfind_GetCTagOnly_CalendarColorUntouched(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindGetCTag)
	defer resp.Body.Close()

	body := readBody(t, resp)
	extractGetCTag(t, body) // still works, unaffected by calendar-color's existence
	if strings.Contains(body, "calendar-color") {
		t.Fatalf("expected no calendar-color in a getctag-only request, got:\n%s", body)
	}
}

// A shared Calendar's calendar-color resolves per principal (ADR-0038): the
// Editor who set a personal colour override sees it, while the Owner —
// PROPFINDing the very same Calendar at their own principal path — still
// sees the Calendar's own stored colour.
func TestPropfind_CalendarColor_ResolvesPerPrincipal_OverrideAppliesOnlyToTheEditor(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	if _, err := env.calendarService.SetColorOverride(context.Background(), editorID, env.calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	editorResp := propfind(t, env.srv, calendarPath(editorID, env.calendarID), "editor@example.com", editorSecret, "0", propfindCalendarColor)
	defer editorResp.Body.Close()
	editorColor := extractCalendarColor(t, readBody(t, editorResp))
	if editorColor != "#654321FF" {
		t.Fatalf("expected the editor's resolved colour to be their override, got %q", editorColor)
	}

	ownerResp := propfind(t, env.srv, calendarPath(env.userID, env.calendarID), "admin@example.com", env.appPasswordSecret, "0", propfindCalendarColor)
	defer ownerResp.Body.Close()
	ownerColor := extractCalendarColor(t, readBody(t, ownerResp))
	if ownerColor != "#12809CFF" {
		t.Fatalf("expected the owner's resolved colour to stay the calendar's own, got %q", ownerColor)
	}
}

func TestPropfind_CalendarColorOnly_GetCTagUntouched(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	if _, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCalendarColor)
	defer resp.Body.Close()

	body := readBody(t, resp)
	extractCalendarColor(t, body) // still works, unaffected by getctag's existence
	if strings.Contains(body, "getctag") {
		t.Fatalf("expected no getctag in a calendar-color-only request, got:\n%s", body)
	}
}
