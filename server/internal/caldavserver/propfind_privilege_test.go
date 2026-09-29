package caldavserver

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
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
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCurrentUserPrivilegeSet)
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
	subCalendar, err := env.calendarService.CreateSubscribed(t.Context(), env.userID, env.workspaceID, "sub-cal-1", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF",
	}, repository.SourceFields{Kind: repository.SourceKindSubscription, Mode: repository.SourceModeReadOnly, SourceURL: &sourceURL})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	path := calendarPath(env.userID, subCalendar.ID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCurrentUserPrivilegeSet)
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

// TestPropfind_CurrentUserPrivilegeSet_LinkedCalendarOwnerWritableGrantsWrite
// covers a Linked Calendar's privilege set for its own Owner on a writable
// Source (ADR-0080's acceptance criteria): "expected to need no new code,
// since it already derives from Access, which clamps on the Source's mode"
// — asserted anyway, since it has never run for a Linked Calendar before
// Exposure gave its Owner a reason to reach the collection at all. The
// Owner defaults to unexposed (ADR-0080), so this needs an explicit
// Exposure override before the collection is reachable over CalDAV.
func TestPropfind_CurrentUserPrivilegeSet_LinkedCalendarOwnerWritableGrantsWrite(t *testing.T) {
	env := newTestCalDAVEnv(t)

	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("set exposure: %v", err)
	}

	path := calendarPath(env.userID, linkedID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCurrentUserPrivilegeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if !strings.Contains(body, "<write") {
		t.Fatalf("expected a writable Linked Calendar's Owner to keep write in their privilege set, got:\n%s", body)
	}
}

// TestPropfind_CurrentUserPrivilegeSet_LinkedCalendarOwnerReadOnlyIsReadOnly
// is the read-only-Source sibling: the Provider will not accept writes to
// this calendar, so Access clamps the Owner to Viewer here too (ADR-0034),
// same as ADR-0032 established for a Subscribed Calendar.
func TestPropfind_CurrentUserPrivilegeSet_LinkedCalendarOwnerReadOnlyIsReadOnly(t *testing.T) {
	env := newTestCalDAVEnv(t)

	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeReadOnly)
	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("set exposure: %v", err)
	}

	path := calendarPath(env.userID, linkedID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindCurrentUserPrivilegeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "<write") {
		t.Fatalf("expected a read-only Linked Calendar's Owner privilege set to omit write, got:\n%s", body)
	}
	if !strings.Contains(body, "<read") {
		t.Fatalf("expected a read-only Linked Calendar's Owner privilege set to still grant read, got:\n%s", body)
	}
}

// TestPropfind_CurrentUserPrivilegeSet_LinkedCalendarViewerShareIsReadOnly
// covers a Workspace Member's own Viewer Share on a Linked Calendar
// (ADR-0075's Editor-Share refusal means Viewer is the only Role reachable
// here) — exposed by default for an accessor (ADR-0080), so no explicit
// Exposure override is needed to reach the collection.
func TestPropfind_CurrentUserPrivilegeSet_LinkedCalendarViewerShareIsReadOnly(t *testing.T) {
	env := newTestCalDAVEnv(t)
	memberID, memberSecret := env.addWorkspaceMember(t, "member")

	linkedID := uuid.NewString()
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if _, _, err := env.calendarService.Share(context.Background(), env.userID, linkedID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share linked calendar: %v", err)
	}

	path := calendarPath(memberID, linkedID)
	resp := propfind(t, env.srv, path, "member@example.com", memberSecret, "0", propfindCurrentUserPrivilegeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "<write") {
		t.Fatalf("expected a Linked Calendar's Viewer Share privilege set to omit write, got:\n%s", body)
	}
	if !strings.Contains(body, "<read") {
		t.Fatalf("expected a Linked Calendar's Viewer Share privilege set to still grant read, got:\n%s", body)
	}
}
