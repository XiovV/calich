// exposure_direct_path_test.go covers ADR-0080's "Direct-path resolution
// stays mirrored with the listing" beyond PROPFIND itself: a client that
// already discovered and cached a Calendar's collection URL before Exposure
// was turned off must stop reading (or renaming) it through every other
// entry point that resolves the same path directly — calendar-query,
// calendar-multiget, a plain object GET, sync-collection, and PROPPATCH —
// not just re-discovering it via a fresh PROPFIND.
package caldavserver

import (
	"context"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

// hiddenAccessorLinkedCalendar sets up a Linked Calendar Shared to a
// Workspace Member who has explicitly turned Exposure off — the scenario
// where a device already cached the collection path before the toggle was
// flipped, one Event on it so a read path has something real to refuse.
func hiddenAccessorLinkedCalendar(t *testing.T) (env testCalDAVEnv, memberID int64, memberSecret, linkedID string, eventID string) {
	t.Helper()
	env = newTestCalDAVEnv(t)
	memberID, memberSecret = env.addWorkspaceMember(t, "member")

	linkedID = uuid.NewString()
	// Writable, purely so eventService.Create below is allowed to write the
	// fixture Event through the ordinary (non-CalDAV) write path — the
	// exposure checks under test read the resolved Calendar, not the Event.
	env.createLinkedCalendarWithMode(t, linkedID, "Linked", repository.SourceModeWritable)
	if _, _, err := env.calendarService.Share(context.Background(), env.userID, linkedID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share linked calendar: %v", err)
	}
	if err := env.calendarService.SetExposure(context.Background(), memberID, linkedID, false); err != nil {
		t.Fatalf("set exposure: %v", err)
	}

	event, err := env.eventService.Create(context.Background(), env.userID, "evt-hidden-1", service.EventWrite{
		CalendarID: linkedID, Title: "Standup",
		Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	return env, memberID, memberSecret, linkedID, event.ID
}

func TestCalendarQuery_ExplicitlyHiddenLinkedCalendar_Returns404(t *testing.T) {
	env, memberID, memberSecret, linkedID, _ := hiddenAccessorLinkedCalendar(t)

	client := newTestCalDAVClientAs(t, env, "member@example.com", memberSecret)
	_, err := client.QueryCalendar(context.Background(), calendarPath(memberID, linkedID), &caldav.CalendarQuery{
		CompFilter: caldav.CompFilter{Name: ical.CompCalendar, Comps: []caldav.CompFilter{{Name: ical.CompEvent}}},
	})
	if err == nil {
		t.Fatalf("expected calendar-query against an explicitly-hidden Linked Calendar to fail")
	}
}

func TestCalendarMultiget_ExplicitlyHiddenLinkedCalendar_Returns404(t *testing.T) {
	env, memberID, memberSecret, linkedID, eventID := hiddenAccessorLinkedCalendar(t)

	client := newTestCalDAVClientAs(t, env, "member@example.com", memberSecret)
	objPath := calendarObjectPath(memberID, linkedID, eventID)
	_, err := client.MultiGetCalendar(context.Background(), calendarPath(memberID, linkedID), &caldav.CalendarMultiGet{
		Paths: []string{objPath},
	})
	if err == nil {
		t.Fatalf("expected calendar-multiget against an explicitly-hidden Linked Calendar to fail")
	}
}

func TestGetCalendarObject_ExplicitlyHiddenLinkedCalendar_Returns404(t *testing.T) {
	env, memberID, memberSecret, linkedID, eventID := hiddenAccessorLinkedCalendar(t)

	client := newTestCalDAVClientAs(t, env, "member@example.com", memberSecret)
	_, err := client.GetCalendarObject(context.Background(), calendarObjectPath(memberID, linkedID, eventID))
	if err == nil {
		t.Fatalf("expected GET of an object on an explicitly-hidden Linked Calendar to fail")
	}
}

func TestSyncCollection_ExplicitlyHiddenLinkedCalendar_Returns404(t *testing.T) {
	env, memberID, memberSecret, linkedID, _ := hiddenAccessorLinkedCalendar(t)

	resp := report(t, env.srv, calendarPath(memberID, linkedID), "member@example.com", memberSecret, syncCollectionInitial)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for sync-collection against an explicitly-hidden Linked Calendar, got %d", resp.StatusCode)
	}
}

func TestPropPatch_ExplicitlyHiddenLinkedCalendar_Returns404(t *testing.T) {
	env, memberID, memberSecret, linkedID, _ := hiddenAccessorLinkedCalendar(t)

	resp := proppatch(t, env.srv, calendarPath(memberID, linkedID), "member@example.com", memberSecret, proppatchSetDisplayName("Hijacked"))
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for PROPPATCH against an explicitly-hidden Linked Calendar, got %d", resp.StatusCode)
	}
}
