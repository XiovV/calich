package caldavserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

// createLinkedCalendar creates a Linked Calendar in env's own Workspace,
// backed by a freshly-inserted Connection row to satisfy calendar_sources'
// foreign key — the ADR-0074 fixture: a Calendar whose Source is
// Connection-kind must never reach a CalDAV home-set, unlike every other
// kind this package's other PROPFIND tests exercise.
func (env testCalDAVEnv) createLinkedCalendar(t *testing.T, id, name string) {
	t.Helper()

	connections := repository.NewConnectionRepository(env.db)
	conn, err := connections.Upsert(context.Background(), env.userID, repository.ProviderGoogle, "someone@gmail.com", repository.ConnectionFields{
		RefreshToken: "encrypted-refresh-token",
		Scopes:       "openid email https://www.googleapis.com/auth/calendar.events https://www.googleapis.com/auth/calendar.calendarlist.readonly",
		Status:       repository.ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	externalID := "external-" + id
	if _, err := env.calendarService.CreateSubscribed(context.Background(), env.userID, env.workspaceID, id, service.CalendarWrite{
		Name: name, Color: "#12809CFF",
	}, repository.SourceFields{
		Kind:               repository.SourceKindConnection,
		Mode:               repository.SourceModeReadOnly,
		ConnectionID:       &conn.ID,
		ExternalCalendarID: &externalID,
	}); err != nil {
		t.Fatalf("create linked calendar: %v", err)
	}
}

// TestPropfind_HomeSet_ExcludesLinkedCalendar covers ADR-0074's "a Linked
// Calendar appears in no principal's CalDAV home-set" for the Owner's own
// side — the ordinary Calendar env.calendarID ("cal-1") is unaffected and
// still appears, exactly like TestPropfind_HomeSet_ListsOneCollectionPerCalendar.
func TestPropfind_HomeSet_ExcludesLinkedCalendar(t *testing.T) {
	env := newTestCalDAVEnv(t)
	env.createLinkedCalendar(t, uuid.NewString(), "Linked")

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", env.userID)
	resp := propfind(t, env.srv, homeSetPath, "admin@example.com", env.appPasswordSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "Linked") {
		t.Fatalf("expected the Linked Calendar to be absent from the home-set, got:\n%s", body)
	}
	wantCollection := fmt.Sprintf("/dav/%d/calendars/%s/", env.userID, env.calendarID)
	if !strings.Contains(body, wantCollection) {
		t.Fatalf("expected the ordinary calendar to still be listed, got:\n%s", body)
	}
}

// TestPropfind_HomeSet_ExcludesLinkedCalendarFromAccessorsSeat covers
// ADR-0074's "a Linked Calendar appears in no principal's CalDAV home-set"
// asserted from the accessor's seat rather than the Owner's — the exact
// failure mode of ADR-0054's superseded rule, where a home-set correct for
// the Owner was wrong for a Workspace Member it was Shared to. The Member is
// Shared both an ordinary Calendar and the Linked one; only the ordinary one
// reaches their home-set.
func TestPropfind_HomeSet_ExcludesLinkedCalendarFromAccessorsSeat(t *testing.T) {
	env := newTestCalDAVEnv(t)
	memberID, memberSecret := env.addWorkspaceMember(t, "member")

	linkedID := uuid.NewString()
	env.createLinkedCalendar(t, linkedID, "Linked")

	// The ordinary Calendar is Shared to the Member as a control: their
	// home-set is not simply empty of everything.
	if _, _, err := env.calendarService.Share(context.Background(), env.userID, env.calendarID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share ordinary calendar: %v", err)
	}
	if _, _, err := env.calendarService.Share(context.Background(), env.userID, linkedID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share linked calendar: %v", err)
	}

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", memberID)
	resp := propfind(t, env.srv, homeSetPath, "member@example.com", memberSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "Linked") {
		t.Fatalf("expected the Shared Linked Calendar to be absent from the accessor's home-set, got:\n%s", body)
	}
	wantCollection := fmt.Sprintf("/dav/%d/calendars/%s/", memberID, env.calendarID)
	if !strings.Contains(body, wantCollection) {
		t.Fatalf("expected the ordinary Shared calendar to still be listed at the accessor's own path, got:\n%s", body)
	}
}

// TestPropfind_HomeSet_StillListsSubscribedCalendar guards the ADR-0074
// filter against over-reach: it keys on the Source's Kind being Connection,
// never merely on a Source existing, so a Subscribed Calendar in the same
// home-set stays exposed over CalDAV exactly as ADR-0032 established.
func TestPropfind_HomeSet_StillListsSubscribedCalendar(t *testing.T) {
	env := newTestCalDAVEnv(t)

	subID := uuid.NewString()
	sourceURL := "https://example.com/feed.ics"
	if _, err := env.calendarService.CreateSubscribed(context.Background(), env.userID, env.workspaceID, subID, service.CalendarWrite{
		Name: "Feed", Color: "#12809CFF",
	}, repository.SourceFields{
		Kind:      repository.SourceKindSubscription,
		Mode:      repository.SourceModeReadOnly,
		SourceURL: &sourceURL,
	}); err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", env.userID)
	resp := propfind(t, env.srv, homeSetPath, "admin@example.com", env.appPasswordSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	wantCollection := fmt.Sprintf("/dav/%d/calendars/%s/", env.userID, subID)
	if !strings.Contains(body, wantCollection) {
		t.Fatalf("expected the Subscribed calendar to still be listed, got:\n%s", body)
	}
}

// TestPropfind_LinkedCalendar_DirectPathReturnsNotFound covers ADR-0074's
// exclusion holding even for a stale or guessed URL, mirroring ListCalendars'
// own condition rather than just hiding the collection from a listing.
func TestPropfind_LinkedCalendar_DirectPathReturnsNotFound(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendar(t, linkedID, "Linked")

	path := fmt.Sprintf("/dav/%d/calendars/%s/", env.userID, linkedID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindDisplayName)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for a Linked Calendar's direct path, got %d", resp.StatusCode)
	}
}
