// calendar_color_update_test.go covers #122's REST surface for ADR-0038's
// per-User colour override: Calendar update accepts a colour from any caller
// with Access, an Owner's write lands on the Calendar itself, anyone else's
// lands as their own override, every other field on the endpoint stays
// Owner-only, and a caller with no Access is refused.
package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

func TestCalendarHandler_Update_NonOwnerSetsPersonalColorOnly(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
	shareResp.Body.Close()

	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: "#654321"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var updated calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Color != "#654321FF" {
		t.Fatalf("expected the caller's resolved colour to be their override, got %q", updated.Color)
	}
	if updated.IsOwner {
		t.Fatalf("expected a non-Owner's response to carry isOwner=false")
	}

	// The Calendar's own colour is untouched — the Owner still sees it.
	ownerGet, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken)
	if err != nil {
		t.Fatalf("owner get: %v", err)
	}
	defer ownerGet.Body.Close()
	var ownerCalendar calendarResponse
	if err := json.NewDecoder(ownerGet.Body).Decode(&ownerCalendar); err != nil {
		t.Fatalf("decode owner get: %v", err)
	}
	if ownerCalendar.Color != "#12809CFF" {
		t.Fatalf("expected the calendar's own colour to stay untouched, got %q", ownerCalendar.Color)
	}
}

// TestCalendarHandler_Update_ViewerSetsPersonalColor covers "Calendar update
// accepts a colour from any caller with Access" for a Viewer specifically —
// SetColorOverride is gated on CanRead, not CanWrite, so a Viewer must be
// able to set their own colour just like an Editor, even though they may
// write nothing else on the Calendar.
func TestCalendarHandler_Update_ViewerSetsPersonalColor(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleViewer})
	shareResp.Body.Close()

	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: "#654321"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var updated calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Color != "#654321FF" || updated.Access != "viewer" {
		t.Fatalf("unexpected viewer update response: %+v", updated)
	}
}

func TestCalendarHandler_Update_OwnerColorWriteUpdatesTheCalendarItself(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken, updateCalendarRequest{Name: "Family", Color: "#654321"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var updated calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Color != "#654321FF" || !updated.IsOwner {
		t.Fatalf("unexpected owner update response: %+v", updated)
	}
}

// Under ADR-0057, clearing a non-Owner's override doesn't leave them
// tracking the Owner's colour — the very next resolution auto-assigns a
// fresh personal one (here, the first free Swatch, since this User can see
// no other coloured Calendar in the Workspace).
func TestCalendarHandler_Update_NonOwnerClearsOverrideReassignsAFreshColor(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleViewer})
	shareResp.Body.Close()

	setResp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: "#654321"})
	setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		t.Fatalf("set override: expected 200, got %d", setResp.StatusCode)
	}

	clearResp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: ""})
	defer clearResp.Body.Close()
	if clearResp.StatusCode != http.StatusOK {
		t.Fatalf("clear override: expected 200, got %d", clearResp.StatusCode)
	}

	var cleared calendarResponse
	if err := json.NewDecoder(clearResp.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cleared.Color != "#E2483DFF" {
		t.Fatalf("expected the cleared override to be freshly auto-assigned (first free Swatch), got %q", cleared.Color)
	}
}

func TestCalendarHandler_Update_NonOwnerCannotRenameCalendar(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
	shareResp.Body.Close()

	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Name: "Hijacked", Color: "#654321"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}

	ownerGet, err := authenticatedGet(s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken)
	if err != nil {
		t.Fatalf("owner get: %v", err)
	}
	defer ownerGet.Body.Close()
	var ownerCalendar calendarResponse
	if err := json.NewDecoder(ownerGet.Body).Decode(&ownerCalendar); err != nil {
		t.Fatalf("decode owner get: %v", err)
	}
	if ownerCalendar.Name != "Family" {
		t.Fatalf("expected the calendar's name to stay untouched, got %q", ownerCalendar.Name)
	}
}

func TestCalendarHandler_Update_NonOwnerCannotChangeKeepAlarms(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
	shareResp.Body.Close()

	keepAlarms := true
	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: "#654321", KeepAlarms: &keepAlarms})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Update_NonOwnerCannotChangeSourceURL(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
	shareResp.Body.Close()

	url := "https://example.com/calendar.ics"
	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: "#654321", URL: &url})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Update_NoAccessRefused(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: "#654321"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Update_NonOwnerRejectsInvalidColor(t *testing.T) {
	s := newShareTestServer(t)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleEditor})
	shareResp.Body.Close()

	resp := doJSON(t, http.MethodPatch, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken, updateCalendarRequest{Color: "not-a-color"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
