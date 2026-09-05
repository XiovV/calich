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
