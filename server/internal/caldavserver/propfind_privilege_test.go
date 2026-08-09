package caldavserver

import (
	"strings"
	"testing"

	"github.com/XiovV/calendar/server/internal/service"
)

const propfindCurrentUserPrivilegeSet = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop><d:current-user-privilege-set/></d:prop>
</d:propfind>`

// An owned Calendar's collection keeps go-webdav's default read+write
// current-user-privilege-set untouched (ADR-0032): applyPrivilegeSetPatch
// declines to patch it.
func TestPropfind_CurrentUserPrivilegeSet_OwnedCalendarGrantsWrite(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindCurrentUserPrivilegeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if !strings.Contains(body, "<write") {
		t.Fatalf("expected an owned calendar's privilege set to still grant write, got:\n%s", body)
	}
}

// A Subscribed Calendar's collection advertises read without write (ADR-0032,
// #89), so a client whose privilege-set support is good enough to act on it
// stops offering edits that can only be refused.
func TestPropfind_CurrentUserPrivilegeSet_SubscribedCalendarIsReadOnly(t *testing.T) {
	env := newTestCalDAVEnv(t)

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := env.calendarService.Create(t.Context(), env.userID, env.workspaceID, "sub-cal-1", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	path := calendarPath(env.userID, subCalendar.ID)
	resp := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindCurrentUserPrivilegeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "<write") {
		t.Fatalf("expected a subscribed calendar's privilege set to omit write, got:\n%s", body)
	}
	if !strings.Contains(body, "<read") {
		t.Fatalf("expected a subscribed calendar's privilege set to still grant read, got:\n%s", body)
	}
	if !strings.Contains(body, "200 OK") {
		t.Fatalf("expected the privilege-set propstat to be 200 OK, got:\n%s", body)
	}
}
