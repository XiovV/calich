package caldavserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

const proppatchSetDisplayName = `<?xml version="1.0" encoding="UTF-8"?>
<d:propertyupdate xmlns:d="DAV:">
  <d:set>
    <d:prop><d:displayname>Renamed</d:displayname></d:prop>
  </d:set>
</d:propertyupdate>`

func TestPropPatch_SetExactEnumColor_Returns200AndPersists(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("#8e44ad")) // grape
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
	if cal.Color != "grape" {
		t.Fatalf("expected the calendar's color to be updated to grape, got %q", cal.Color)
	}
}

func TestPropPatch_SetArbitraryNearColor_SnapsToNearestEnum_Returns200(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	// One unit off grape (#8e44ad -> 142,68,173) in every channel. A
	// PROPPATCH response only echoes property names with a status (RFC
	// 4918 §14.16), never values, so the snapped value itself is verified
	// via a follow-up GET, not this response's body.
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetCalendarColor("#8f45ae"))
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
	if cal.Color != "grape" {
		t.Fatalf("expected the calendar's color to snap to grape, got %q", cal.Color)
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
	if cal.Color != "peacock" {
		t.Fatalf("expected the calendar's color to stay unchanged (peacock), got %q", cal.Color)
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
	if cal.Color != "peacock" {
		t.Fatalf("expected the calendar's color to stay unchanged (peacock), got %q", cal.Color)
	}
}

func TestPropPatch_UnsupportedProperty_Returns403AndLeavesCalendarUnchanged(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := proppatch(t, env.srv, path, "admin", env.appPasswordSecret, proppatchSetDisplayName)
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

	if color1 != "#8e44ad" || color2 != "#8e44ad" {
		t.Fatalf("expected both GETs to return the canonical (snapped) grape hex, got %q then %q", color1, color2)
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
