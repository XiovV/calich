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
// foreign key, read-only at the Source (ADR-0080's default resolution only
// looks at Source.Kind, so mode is incidental here — createLinkedCalendarWithMode
// is the variant that varies it).
func (env testCalDAVEnv) createLinkedCalendar(t *testing.T, id, name string) {
	t.Helper()
	env.createLinkedCalendarWithMode(t, id, name, repository.SourceModeReadOnly)
}

// createLinkedCalendarWithMode is createLinkedCalendar's mode-parameterized
// sibling, for a test asserting the CalDAV privilege set a writable Linked
// Calendar's Owner gets versus a read-only one's (ADR-0075, ADR-0080) — the
// mode has never varied in this package before Exposure gave a Linked
// Calendar's own Owner a reason to actually reach its collection.
func (env testCalDAVEnv) createLinkedCalendarWithMode(t *testing.T, id, name string, mode repository.SourceMode) {
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
		Mode:               mode,
		ConnectionID:       &conn.ID,
		ExternalCalendarID: &externalID,
	}); err != nil {
		t.Fatalf("create linked calendar: %v", err)
	}
}

// TestPropfind_HomeSet_ExcludesLinkedCalendarFromOwnerByDefault covers
// ADR-0080's default for a Linked Calendar's own Owner: absent an explicit
// Exposure choice, it stays out of their own CalDAV home-set — the
// connecting User almost certainly already syncs it natively at the
// Provider, unchanged from ADR-0074's reasoning even though the rule is no
// longer unconditional. The ordinary Calendar env.calendarID ("cal-1") is
// unaffected and still appears, exactly like
// TestPropfind_HomeSet_ListsOneCollectionPerCalendar.
func TestPropfind_HomeSet_ExcludesLinkedCalendarFromOwnerByDefault(t *testing.T) {
	env := newTestCalDAVEnv(t)
	env.createLinkedCalendar(t, uuid.NewString(), "Linked")

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", env.userID)
	resp := propfind(t, env.srv, homeSetPath, "admin@example.com", env.appPasswordSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "Linked") {
		t.Fatalf("expected the Linked Calendar to be absent from its Owner's home-set by default, got:\n%s", body)
	}
	wantCollection := fmt.Sprintf("/dav/%d/calendars/%s/", env.userID, env.calendarID)
	if !strings.Contains(body, wantCollection) {
		t.Fatalf("expected the ordinary calendar to still be listed, got:\n%s", body)
	}
}

// TestPropfind_HomeSet_IncludesLinkedCalendarInAccessorsSeatByDefault covers
// ADR-0080's default for a Workspace Member a Linked Calendar was Shared
// to: present by default in their own home-set, since they have no native
// Provider copy of it to duplicate. This is the inverted acceptance
// criterion ADR-0074 (and this test, before Exposure) asserted the opposite
// of — the single most important behavioural change #297 makes. The Member
// is Shared both an ordinary Calendar and the Linked one; both reach their
// home-set.
func TestPropfind_HomeSet_IncludesLinkedCalendarInAccessorsSeatByDefault(t *testing.T) {
	env := newTestCalDAVEnv(t)
	memberID, memberSecret := env.addWorkspaceMember(t, "member")

	linkedID := uuid.NewString()
	env.createLinkedCalendar(t, linkedID, "Linked")

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
	wantOrdinary := fmt.Sprintf("/dav/%d/calendars/%s/", memberID, env.calendarID)
	wantLinked := fmt.Sprintf("/dav/%d/calendars/%s/", memberID, linkedID)
	if !strings.Contains(body, wantOrdinary) {
		t.Fatalf("expected the ordinary Shared calendar to be listed at the accessor's own path, got:\n%s", body)
	}
	if !strings.Contains(body, wantLinked) {
		t.Fatalf("expected the Shared Linked Calendar to be present in the accessor's home-set by default, got:\n%s", body)
	}
}

// TestPropfind_HomeSet_OwnerExplicitExposureOverridesDefault covers an
// explicit Exposure choice overriding the default in the "on" direction
// (ADR-0080's acceptance criteria): the Owner who has moved off native
// Provider sync can turn their own Linked Calendar back on.
func TestPropfind_HomeSet_OwnerExplicitExposureOverridesDefault(t *testing.T) {
	env := newTestCalDAVEnv(t)
	linkedID := uuid.NewString()
	env.createLinkedCalendar(t, linkedID, "Linked")

	if err := env.calendarService.SetExposure(context.Background(), env.userID, linkedID, true); err != nil {
		t.Fatalf("set exposure: %v", err)
	}

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", env.userID)
	resp := propfind(t, env.srv, homeSetPath, "admin@example.com", env.appPasswordSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	wantCollection := fmt.Sprintf("/dav/%d/calendars/%s/", env.userID, linkedID)
	if !strings.Contains(body, wantCollection) {
		t.Fatalf("expected an explicit Exposure override to include the Owner's own Linked Calendar, got:\n%s", body)
	}
}

// TestPropfind_HomeSet_AccessorExplicitExposureOverridesDefault covers the
// same override in the "off" direction: a Workspace Member who doesn't want
// a Shared Linked Calendar cluttering their devices can turn it off, even
// though it defaults to on for them.
func TestPropfind_HomeSet_AccessorExplicitExposureOverridesDefault(t *testing.T) {
	env := newTestCalDAVEnv(t)
	memberID, memberSecret := env.addWorkspaceMember(t, "member")

	linkedID := uuid.NewString()
	env.createLinkedCalendar(t, linkedID, "Linked")
	if _, _, err := env.calendarService.Share(context.Background(), env.userID, linkedID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share linked calendar: %v", err)
	}
	if err := env.calendarService.SetExposure(context.Background(), memberID, linkedID, false); err != nil {
		t.Fatalf("set exposure: %v", err)
	}

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", memberID)
	resp := propfind(t, env.srv, homeSetPath, "member@example.com", memberSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "Linked") {
		t.Fatalf("expected an explicit Exposure override to exclude the accessor's own Shared Linked Calendar, got:\n%s", body)
	}
}

// TestPropfind_HomeSet_RevokedShareWinsOverLeftoverExposureRow covers
// "Access resolves first, Exposure second" (ADR-0080's acceptance
// criteria): a stray Exposure row saying "show it" must never resurrect a
// Calendar the caller's Access to was revoked. RevokeShare already clears
// this row along the ordinary path (mirroring ADR-0038's colour-override
// cleanup); the row is recreated directly against the repository here so
// the assertion is about resolution order, not about that cleanup happening
// to run.
func TestPropfind_HomeSet_RevokedShareWinsOverLeftoverExposureRow(t *testing.T) {
	env := newTestCalDAVEnv(t)
	memberID, memberSecret := env.addWorkspaceMember(t, "member")

	linkedID := uuid.NewString()
	env.createLinkedCalendar(t, linkedID, "Linked")
	if _, _, err := env.calendarService.Share(context.Background(), env.userID, linkedID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share linked calendar: %v", err)
	}
	if err := env.calendarService.RevokeShare(context.Background(), env.userID, linkedID, memberID); err != nil {
		t.Fatalf("revoke share: %v", err)
	}

	exposures := repository.NewCalendarExposureRepository(env.db)
	if err := exposures.Upsert(context.Background(), memberID, linkedID, true); err != nil {
		t.Fatalf("upsert stray exposure row: %v", err)
	}

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", memberID)
	resp := propfind(t, env.srv, homeSetPath, "member@example.com", memberSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "Linked") {
		t.Fatalf("expected a revoked Share to exclude the Calendar despite a stray Exposure row saying exposed, got:\n%s", body)
	}

	path := fmt.Sprintf("/dav/%d/calendars/%s/", memberID, linkedID)
	directResp := propfind(t, env.srv, path, "member@example.com", memberSecret, "0", propfindDisplayName)
	defer directResp.Body.Close()
	if directResp.StatusCode != 404 {
		t.Fatalf("expected 404 on the direct path despite the stray Exposure row, got %d", directResp.StatusCode)
	}
}

// TestPropfind_HomeSet_StillListsSubscribedCalendar guards Exposure's
// default resolution against over-reach: a Subscribed Calendar defaults to
// exposed for everyone, Owner included, exactly as ADR-0032 established —
// only a Linked Calendar's own Owner gets the unexposed default.
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

// TestPropfind_LinkedCalendar_DirectPathReturnsNotFound covers ADR-0080's
// default exclusion holding even for a stale or guessed URL, mirroring
// ListCalendars' own condition rather than just hiding the collection from
// a listing.
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

// TestPropfind_LinkedCalendar_AccessorExplicitlyHiddenDirectPathReturnsNotFound
// covers "not exposed ⇒ 404 on the direct path, matching the listing"
// (ADR-0080's acceptance criteria) for an explicit override rather than a
// default: an accessor who turned their own Shared Linked Calendar off gets
// the same 404 a stale URL would.
func TestPropfind_LinkedCalendar_AccessorExplicitlyHiddenDirectPathReturnsNotFound(t *testing.T) {
	env := newTestCalDAVEnv(t)
	memberID, memberSecret := env.addWorkspaceMember(t, "member")

	linkedID := uuid.NewString()
	env.createLinkedCalendar(t, linkedID, "Linked")
	if _, _, err := env.calendarService.Share(context.Background(), env.userID, linkedID, "member@example.com", repository.RoleViewer); err != nil {
		t.Fatalf("share linked calendar: %v", err)
	}
	if err := env.calendarService.SetExposure(context.Background(), memberID, linkedID, false); err != nil {
		t.Fatalf("set exposure: %v", err)
	}

	path := fmt.Sprintf("/dav/%d/calendars/%s/", memberID, linkedID)
	resp := propfind(t, env.srv, path, "member@example.com", memberSecret, "0", propfindDisplayName)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for an explicitly-hidden Linked Calendar's direct path, got %d", resp.StatusCode)
	}
}
