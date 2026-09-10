// calendar_exposure_test.go covers #297's REST surface for ADR-0080's
// Exposure: PUT .../exposure writes only the caller's own row, open to any
// caller with Access (Owner and accessor alike, unlike the colour override's
// Owner-vs-non-Owner split), and GET/List's resolved "exposed" field reflects
// it — plus the source-and-ownership-dependent default when no row exists.
package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

func getCalendarResponse(t *testing.T, url, accessToken string) calendarResponse {
	t.Helper()
	resp, err := authenticatedGet(url, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// TestCalendarHandler_Get_OrdinaryCalendarDefaultsExposedTrue is the sanity
// baseline: an ordinary Calendar's Owner sees it exposed by default, with no
// Exposure row ever written.
func TestCalendarHandler_Get_OrdinaryCalendarDefaultsExposedTrue(t *testing.T) {
	s := newShareTestServer(t)

	got := getCalendarResponse(t, s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken)
	if !got.Exposed {
		t.Fatalf("expected an ordinary calendar to default to exposed, got %+v", got)
	}
}

// TestCalendarHandler_Get_LinkedCalendarOwnerDefaultsUnexposed covers
// ADR-0080's Owner-side default: absent an explicit choice, a Linked
// Calendar's own Owner sees it unexposed.
func TestCalendarHandler_Get_LinkedCalendarOwnerDefaultsUnexposed(t *testing.T) {
	s := newShareTestServer(t)
	s.makeLinkedCalendar(t, repository.SourceModeReadOnly)

	got := getCalendarResponse(t, s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken)
	if got.Exposed {
		t.Fatalf("expected a Linked Calendar to default to unexposed for its own Owner, got %+v", got)
	}
}

// TestCalendarHandler_SetExposure_OwnerSetsOwnRow covers the Owner turning
// their own Linked Calendar back on, overriding the unexposed default.
func TestCalendarHandler_SetExposure_OwnerSetsOwnRow(t *testing.T) {
	s := newShareTestServer(t)
	s.makeLinkedCalendar(t, repository.SourceModeReadOnly)

	resp := doJSON(t, http.MethodPut, s.baseURL+"/api/calendars/"+s.calendarID+"/exposure", s.ownerToken, setExposureRequest{Exposed: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out exposureResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Exposed {
		t.Fatalf("expected the response to echo exposed=true, got %+v", out)
	}

	got := getCalendarResponse(t, s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken)
	if !got.Exposed {
		t.Fatalf("expected the Owner's explicit override to stick, got %+v", got)
	}
}

// TestCalendarHandler_SetExposure_ViewerSetsOwnRowIndependentOfOwner covers
// "setting writes the requesting User's own row only" (#297's acceptance
// criteria): a Viewer's own choice never touches the Owner's, in either
// direction.
func TestCalendarHandler_SetExposure_ViewerSetsOwnRowIndependentOfOwner(t *testing.T) {
	s := newShareTestServer(t)
	s.makeLinkedCalendar(t, repository.SourceModeReadOnly)

	shareResp := doJSON(t, http.MethodPost, s.baseURL+"/api/calendars/"+s.calendarID+"/shares", s.ownerToken, shareRequest{Email: "other@example.com", Role: repository.RoleViewer})
	shareResp.Body.Close()

	// The accessor defaults to exposed (ADR-0080); they turn it off.
	setResp := doJSON(t, http.MethodPut, s.baseURL+"/api/calendars/"+s.calendarID+"/exposure", s.otherToken, setExposureRequest{Exposed: false})
	setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", setResp.StatusCode)
	}

	viewerGot := getCalendarResponse(t, s.baseURL+"/api/calendars/"+s.calendarID, s.otherToken)
	if viewerGot.Exposed {
		t.Fatalf("expected the Viewer's own override to stick, got %+v", viewerGot)
	}

	// The Owner's own (unexposed-by-default) row is untouched by the
	// Viewer's write.
	ownerGot := getCalendarResponse(t, s.baseURL+"/api/calendars/"+s.calendarID, s.ownerToken)
	if ownerGot.Exposed {
		t.Fatalf("expected the Owner's default to stay unexposed, got %+v", ownerGot)
	}
}

// TestCalendarHandler_SetExposure_NoAccessRefused covers a caller with no
// Access at all — refused the same way every other per-Access write on a
// Calendar is (#272), 404 rather than 403 so a stranger can't distinguish
// "exists but isn't mine" from "doesn't exist".
func TestCalendarHandler_SetExposure_NoAccessRefused(t *testing.T) {
	s := newShareTestServer(t)

	resp := doJSON(t, http.MethodPut, s.baseURL+"/api/calendars/"+s.calendarID+"/exposure", s.otherToken, setExposureRequest{Exposed: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
