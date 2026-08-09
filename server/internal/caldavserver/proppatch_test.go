package caldavserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

func proppatch(t *testing.T, srv *httptest.Server, path, username, password, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest("PROPPATCH", srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build PROPPATCH request: %v", err)
	}
	req.Header.Set("Content-Type", "application/xml")
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PROPPATCH %s: %v", path, err)
	}
	return resp
}

func proppatchSetCalendarColor(hex string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:ic="http://apple.com/ns/ical/">
  <d:set>
    <d:prop><ic:calendar-color>%s</ic:calendar-color></d:prop>
  </d:set>
</d:propertyupdate>`, hex)
}

const proppatchRemoveCalendarColor = `<?xml version="1.0" encoding="UTF-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:ic="http://apple.com/ns/ical/">
  <d:remove>
    <d:prop><ic:calendar-color/></d:prop>
  </d:remove>
</d:propertyupdate>`

func proppatchSetDisplayName(name string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<d:propertyupdate xmlns:d="DAV:">
  <d:set>
    <d:prop><d:displayname>%s</d:displayname></d:prop>
  </d:set>
</d:propertyupdate>`, name)
}

const proppatchRemoveDisplayName = `<?xml version="1.0" encoding="UTF-8"?>
<d:propertyupdate xmlns:d="DAV:">
  <d:remove>
    <d:prop><d:displayname/></d:prop>
  </d:remove>
</d:propertyupdate>`

const proppatchUnsupportedProperty = `<?xml version="1.0" encoding="UTF-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:x="urn:test">
  <d:set>
    <d:prop><x:foo>bar</x:foo></d:prop>
  </d:set>
</d:propertyupdate>`

func proppatchSetDisplayNameAndCalendarColor(name, hex string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<d:propertyupdate xmlns:d="DAV:" xmlns:ic="http://apple.com/ns/ical/">
  <d:set>
    <d:prop>
      <d:displayname>%s</d:displayname>
      <ic:calendar-color>%s</ic:calendar-color>
    </d:prop>
  </d:set>
</d:propertyupdate>`, name, hex)
}

func TestPropPatch_SetArbitraryColor_Returns200AndPersistsExactly(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	// A color matching none of the app's 8 Swatches (ADR-0029: any hex is
	// valid, not just a curated set) — round-trips exactly, no snapping.
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("#8f45ae"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "200 OK") {
		t.Fatalf("expected a 200 OK propstat, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Color != "#8F45AEFF" {
		t.Fatalf("expected the calendar's color to be updated to #8F45AEFF, got %q", cal.Color)
	}
}

func TestPropPatch_SetShortHexColor_WidensWithFFAlpha_Returns200(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	// A PROPPATCH response only echoes property names with a status (RFC
	// 4918 §14.16), never values, so the widened value is verified via a
	// follow-up GET, not this response's body.
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("#1af"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "calendar-color") || !strings.Contains(body, "200 OK") {
		t.Fatalf("expected a 200 OK propstat for calendar-color, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Color != "#11AAFFFF" {
		t.Fatalf("expected the calendar's color to widen to #11AAFFFF, got %q", cal.Color)
	}
}

func TestPropPatch_SetMalformedColor_Returns409AndLeavesColorUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("not-a-color"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "409 Conflict") {
		t.Fatalf("expected a 409 Conflict propstat, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Color != "#12809CFF" {
		t.Fatalf("expected the calendar's color to stay unchanged (#12809CFF), got %q", cal.Color)
	}
}

func TestPropPatch_RemoveCalendarColor_Returns403AndLeavesColorUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchRemoveCalendarColor)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "403 Forbidden") {
		t.Fatalf("expected a 403 Forbidden propstat, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Color != "#12809CFF" {
		t.Fatalf("expected the calendar's color to stay unchanged (#12809CFF), got %q", cal.Color)
	}
}

func TestPropPatch_UnsupportedProperty_Returns403AndLeavesCalendarUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchUnsupportedProperty)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "403 Forbidden") {
		t.Fatalf("expected a 403 Forbidden propstat, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Personal" {
		t.Fatalf("expected the calendar's name to stay unchanged, got %q", cal.Name)
	}
}

func TestPropPatch_SetDisplayName_Returns200AndPersists(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayName("Renamed"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "displayname") || !strings.Contains(body, "200 OK") {
		t.Fatalf("expected a 200 OK propstat for displayname, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Renamed" {
		t.Fatalf("expected the calendar's name to be updated to Renamed, got %q", cal.Name)
	}
}

func TestPropPatch_SetEmptyDisplayName_Returns409AndLeavesNameUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayName(""))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "409 Conflict") {
		t.Fatalf("expected a 409 Conflict propstat, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Personal" {
		t.Fatalf("expected the calendar's name to stay unchanged, got %q", cal.Name)
	}
}

func TestPropPatch_RemoveDisplayName_Returns403AndLeavesNameUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchRemoveDisplayName)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "403 Forbidden") {
		t.Fatalf("expected a 403 Forbidden propstat, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Personal" {
		t.Fatalf("expected the calendar's name to stay unchanged, got %q", cal.Name)
	}
}

func TestPropPatch_SetDisplayNameAndCalendarColor_BothValid_BothApplied(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayNameAndCalendarColor("Renamed", "#8e44ad"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "displayname") || !strings.Contains(body, "calendar-color") || !strings.Contains(body, "200 OK") {
		t.Fatalf("expected one 200 OK propstat covering both displayname and calendar-color, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Renamed" || cal.Color != "#8E44ADFF" {
		t.Fatalf("expected both name and color to be updated, got name=%q color=%q", cal.Name, cal.Color)
	}
}

func TestPropPatch_SetDisplayNameAndMalformedColor_NeitherApplied_ValidPropertyReports424(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayNameAndCalendarColor("Renamed", "not-a-color"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "409 Conflict") {
		t.Fatalf("expected a 409 Conflict propstat for calendar-color, got:\n%s", body)
	}
	if !strings.Contains(body, "424 Failed Dependency") {
		t.Fatalf("expected a 424 Failed Dependency propstat for displayname, got:\n%s", body)
	}
	if strings.Contains(body, "200 OK") {
		t.Fatalf("expected no 200 OK propstat when one property fails, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Personal" || cal.Color != "#12809CFF" {
		t.Fatalf("expected neither name nor color to change, got name=%q color=%q", cal.Name, cal.Color)
	}
}

func TestPropPatch_SetDisplayName_VisibleOnNextPropfind(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	patchResp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayName("Renamed"))
	patchResp.Body.Close()

	resp := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if !strings.Contains(body, "Renamed") {
		t.Fatalf("expected the renamed calendar to be visible on PROPFIND, got:\n%s", body)
	}
	if strings.Contains(body, "Personal") {
		t.Fatalf("expected the old name to no longer be served, got:\n%s", body)
	}
}

func TestPropPatch_UnknownCalendarPath_Returns404(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, "does-not-exist")
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("#8e44ad"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPropPatch_ThenPropfindTwice_RoundTripStable(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	patchResp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("#8f45ae"))
	patchResp.Body.Close()

	resp1 := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindCalendarColor)
	color1 := extractCalendarColor(t, readBody(t, resp1))
	resp1.Body.Close()

	resp2 := propfind(t, env.srv, path, "admin", env.appPasswordSecret, "0", propfindCalendarColor)
	defer resp2.Body.Close()
	color2 := extractCalendarColor(t, readBody(t, resp2))

	if color1 != "#8F45AEFF" || color2 != "#8F45AEFF" {
		t.Fatalf("expected both GETs to return the canonical hex #8F45AEFF, got %q then %q", color1, color2)
	}
}

// The Owner's rename or recolour of a Subscribed Calendar is refused
// (ADR-0032, #89) — a Subscription's read-only privilege set would be
// incoherent with either, so the property is staged Forbidden rather than
// applied, and the Calendar stays untouched. Unlike before #106/ADR-0038,
// the request itself still resolves to a 207 Multi-Status — only
// calendar-color's per-property authorization decides Forbidden, since a
// non-owner's colour override PROPPATCHes the same subscribed collection
// successfully (see TestPropPatch_NonOwnerColorOverride_SucceedsOnSubscribedCalendar).
func TestPropPatch_SubscribedCalendarIsForbidden(t *testing.T) {
	env := newTestCalDAVEnv(t)

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := env.calendarService.Create(t.Context(), env.userID, env.workspaceID, "sub-cal-1", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	path := calendarPath(env.userID, subCalendar.ID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayNameAndCalendarColor("Renamed", "#8e44ad"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "displayname") || !strings.Contains(body, "calendar-color") || !strings.Contains(body, "403 Forbidden") {
		t.Fatalf("expected a 403 Forbidden propstat covering both displayname and calendar-color, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, subCalendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Feed" || cal.Color != "#123456FF" {
		t.Fatalf("expected the subscribed calendar to stay unchanged, got name=%q color=%q", cal.Name, cal.Color)
	}
}

// A non-owner's colour override PROPPATCHes successfully even on a
// Subscribed Calendar (ADR-0038's acceptance criteria: "a User's override
// wins over [publisher-tracking] for that User") — the override never
// touches calendars.color or the feed-tracking shadow columns, so the
// Subscription's read-only clamp on the Owner's own writes doesn't apply to
// it.
func TestPropPatch_NonOwnerColorOverride_SucceedsOnSubscribedCalendar(t *testing.T) {
	env := newTestCalDAVEnv(t)

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := env.calendarService.Create(t.Context(), env.userID, env.workspaceID, "sub-cal-1", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}
	viewer, err := env.users.Create(t.Context(), "viewer", "hash", false)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if err := env.workspaces.AddMember(t.Context(), env.workspaceID, viewer.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add viewer as workspace member: %v", err)
	}
	if _, err := env.calendarService.Share(t.Context(), env.userID, subCalendar.ID, "viewer", repository.RoleViewer); err != nil {
		t.Fatalf("share subscribed calendar: %v", err)
	}
	created, err := env.appPasswordService.Create(t.Context(), viewer.ID, "Test device")
	if err != nil {
		t.Fatalf("create app password for viewer: %v", err)
	}

	path := calendarPath(viewer.ID, subCalendar.ID)
	resp := proppatch(t, env.srv, path, "viewer", created.Secret, proppatchSetCalendarColor("#8e44ad"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "200 OK") {
		t.Fatalf("expected a 200 OK propstat for the viewer's colour override, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, subCalendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Color != "#123456FF" {
		t.Fatalf("expected the subscribed calendar's own colour to stay unchanged, got %q", cal.Color)
	}
}

// The multi-property rename path (displayname + calendar-color in one
// request) still works on an owned Calendar — the subscribed guard must not
// have broadened beyond subscribed collections.
func TestPropPatch_OwnedCalendarMultiPropertyRename_StillWorks(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayNameAndCalendarColor("Renamed", "#8e44ad"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name != "Renamed" || cal.Color != "#8E44ADFF" {
		t.Fatalf("expected both name and color to be updated, got name=%q color=%q", cal.Name, cal.Color)
	}
}

// A Viewer — read-only, per ADR-0034/ADR-0035 — may still PROPPATCH their
// own colour override (ADR-0038): the property is a personal display
// preference, not a write to the Calendar's Events or the Calendar itself,
// so it's open to any Access level that clears CanRead, same as
// SetReminderOverride's gate.
func TestPropPatch_ViewerSetsOwnColorOverride_Returns200(t *testing.T) {
	env := newTestCalDAVEnv(t)
	viewerID, viewerSecret := env.addSharedUser(t, "viewer", repository.RoleViewer)

	path := calendarPath(viewerID, env.calendarID)
	resp := proppatch(t, env.srv, path, "viewer", viewerSecret, proppatchSetCalendarColor("#654321"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "200 OK") {
		t.Fatalf("expected the viewer's colour override to succeed with a 200 OK propstat, got:\n%s", body)
	}

	view, err := env.calendarService.AccessWithColor(t.Context(), viewerID, env.calendarID)
	if err != nil {
		t.Fatalf("resolve viewer's view: %v", err)
	}
	if view.Color != "#654321FF" {
		t.Fatalf("expected the viewer's resolved colour to be their override, got %q", view.Color)
	}

	ownerCal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if ownerCal.Color != "#12809CFF" {
		t.Fatalf("expected the owner's own colour to stay unchanged, got %q", ownerCal.Color)
	}
}

// A non-owner's <remove> of calendar-color clears their own override,
// falling back to the Calendar's own colour — unlike the Owner's, whose
// backing column is NOT NULL, an override row simply may not exist, so
// there's a real "unset" to perform (ADR-0038's acceptance criteria: "A
// User ... can ... clear it to fall back to the Owner's").
func TestPropPatch_NonOwnerRemoveColorOverride_Returns200AndFallsBackToOwnersColor(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	if _, err := env.calendarService.SetColorOverride(t.Context(), editorID, env.calendarID, "#654321"); err != nil {
		t.Fatalf("set color override: %v", err)
	}

	path := calendarPath(editorID, env.calendarID)
	resp := proppatch(t, env.srv, path, "editor", editorSecret, proppatchRemoveCalendarColor)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "200 OK") {
		t.Fatalf("expected the editor's override removal to succeed with a 200 OK propstat, got:\n%s", body)
	}

	view, err := env.calendarService.AccessWithColor(t.Context(), editorID, env.calendarID)
	if err != nil {
		t.Fatalf("resolve editor's view: %v", err)
	}
	if view.Color != "#12809CFF" {
		t.Fatalf("expected the editor's resolved colour to fall back to the owner's after removal, got %q", view.Color)
	}
}

// A Viewer's rename attempt stays refused regardless of #106/ADR-0038 —
// only calendar-color gained a non-owner path; displayname is unaffected.
func TestPropPatch_ViewerCannotRename(t *testing.T) {
	env := newTestCalDAVEnv(t)
	viewerID, viewerSecret := env.addSharedUser(t, "viewer", repository.RoleViewer)

	path := calendarPath(viewerID, env.calendarID)
	resp := proppatch(t, env.srv, path, "viewer", viewerSecret, proppatchSetDisplayName("Renamed by viewer"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "403 Forbidden") {
		t.Fatalf("expected a 403 Forbidden propstat, got:\n%s", body)
	}

	cal, err := env.calendarService.Get(t.Context(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name == "Renamed by viewer" {
		t.Fatalf("expected the rename to not have taken effect")
	}
}

func TestPropPatch_NoLongerReturns501(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("#8e44ad"))
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotImplemented {
		t.Fatalf("expected PROPPATCH to be handled, got 501 Not Implemented")
	}
}
