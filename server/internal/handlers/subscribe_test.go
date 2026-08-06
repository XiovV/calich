package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const subscribeHandlerTestICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Team Holidays
X-APPLE-CALENDAR-COLOR:#8E44ADFF
BEGIN:VEVENT
UID:feed-uid-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
END:VEVENT
END:VCALENDAR
`

func icsFeedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(strings.ReplaceAll(subscribeHandlerTestICS, "\n", "\r\n")))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postSubscribe(t *testing.T, baseURL, accessToken, dryRun, url, name, color string) *http.Response {
	t.Helper()
	return postSubscribeWithKeepAlarms(t, baseURL, accessToken, dryRun, url, name, color, false)
}

func postSubscribeWithKeepAlarms(t *testing.T, baseURL, accessToken, dryRun, url, name, color string, keepAlarms bool) *http.Response {
	t.Helper()

	body, err := json.Marshal(subscribeRequest{URL: url, Name: name, Color: color, KeepAlarms: keepAlarms})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/calendars/subscribe?dryRun="+dryRun, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

// patchKeepAlarms sends a PATCH carrying name/color alongside keepAlarms —
// the shape the frontend's edit dialog sends for a Subscribed Calendar
// (#87, ADR-0032): the whole form's current state, not a partial diff.
func patchKeepAlarms(t *testing.T, baseURL, accessToken, id, name, color string, keepAlarms bool) *http.Response {
	t.Helper()

	body, err := json.Marshal(updateCalendarRequest{Name: name, Color: color, KeepAlarms: &keepAlarms})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, baseURL+"/api/calendars/"+id, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	return resp
}

func TestCalendarHandler_Subscribe_Preview(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)
	feed := icsFeedServer(t)

	resp := postSubscribe(t, baseURL, accessToken, "1", feed.URL+"/feed.ics", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var preview subscribePreviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if preview.Name != "Team Holidays" || preview.Color != "#8E44ADFF" || preview.EventCount != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	listResp, err := authenticatedGet(baseURL+"/api/calendars/", accessToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()
	var calendars []calendarResponse
	if err := json.NewDecoder(listResp.Body).Decode(&calendars); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(calendars) != 0 {
		t.Fatalf("expected a preview to create no calendar, got %+v", calendars)
	}
}

func TestCalendarHandler_Subscribe_Commit(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)
	feed := icsFeedServer(t)

	resp := postSubscribe(t, baseURL, accessToken, "0", feed.URL+"/feed.ics", "Team Holidays", "#8E44ADFF")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "Team Holidays" || created.Color != "#8E44ADFF" {
		t.Fatalf("unexpected calendar: %+v", created)
	}
	if created.SourceURL == nil || *created.SourceURL != feed.URL+"/feed.ics" {
		t.Fatalf("expected sourceUrl to be set, got %+v", created.SourceURL)
	}
}

func TestCalendarHandler_Subscribe_MasksPasswordInResponse(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)
	feed := icsFeedServer(t)

	feedURL := strings.Replace(feed.URL, "http://", "http://alice:s3cret@", 1) + "/feed.ics"

	resp := postSubscribe(t, baseURL, accessToken, "0", feedURL, "Team Holidays", "#8E44ADFF")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.SourceURL == nil {
		t.Fatalf("expected sourceUrl to be set")
	}
	if strings.Contains(*created.SourceURL, "s3cret") {
		t.Fatalf("expected the password to be masked, got %q", *created.SourceURL)
	}
}

func TestCalendarHandler_Subscribe_InvalidURL(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	resp := postSubscribe(t, baseURL, accessToken, "1", "not a url", "", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Subscribe_RequiresAuth(t *testing.T) {
	baseURL, _ := newCalendarTestServer(t)

	resp, err := http.Post(baseURL+"/api/calendars/subscribe?dryRun=1", "application/json", bytes.NewReader([]byte(`{"url":"http://example.com/feed.ics"}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func postRefresh(t *testing.T, baseURL, accessToken, calendarID string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/calendars/"+calendarID+"/refresh", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestCalendarHandler_Refresh_Success(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)
	feed := icsFeedServer(t)

	subscribeResp := postSubscribe(t, baseURL, accessToken, "0", feed.URL+"/feed.ics", "Team Holidays", "#8E44ADFF")
	var calendar calendarResponse
	if err := json.NewDecoder(subscribeResp.Body).Decode(&calendar); err != nil {
		t.Fatalf("decode subscribe response: %v", err)
	}

	resp := postRefresh(t, baseURL, accessToken, calendar.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var refreshed subscriptionRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&refreshed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if refreshed.NotModified {
		t.Fatalf("expected a forced refresh to actually run, got %+v", refreshed)
	}

	getResp, err := authenticatedGet(baseURL+"/api/calendars/"+calendar.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	var got calendarResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.LastSyncedAt == nil {
		t.Fatalf("expected lastSyncedAt to be set after a refresh")
	}
}

func TestCalendarHandler_Refresh_FailureSurfacesErrorClassAndMessage(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	up := true
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(strings.ReplaceAll(subscribeHandlerTestICS, "\n", "\r\n")))
	}))
	t.Cleanup(feed.Close)

	subscribeResp := postSubscribe(t, baseURL, accessToken, "0", feed.URL+"/feed.ics", "Team Holidays", "#8E44ADFF")
	var calendar calendarResponse
	if err := json.NewDecoder(subscribeResp.Body).Decode(&calendar); err != nil {
		t.Fatalf("decode subscribe response: %v", err)
	}

	up = false
	resp := postRefresh(t, baseURL, accessToken, calendar.ID)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	getResp, err := authenticatedGet(baseURL+"/api/calendars/"+calendar.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	var got calendarResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.ErrorClass == nil || *got.ErrorClass != "needs_attention" {
		t.Fatalf("expected errorClass needs_attention in the calendar response, got %+v", got)
	}
	if got.ErrorMessage == nil || *got.ErrorMessage == "" {
		t.Fatalf("expected a non-empty errorMessage in the calendar response, got %+v", got)
	}
}

func TestCalendarHandler_Refresh_RejectsOrdinaryCalendar(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	createResp := createCalendar(t, baseURL, accessToken, "11111111-1111-1111-1111-111111111111", "Personal", "#12809CFF")
	var created calendarResponse
	json.NewDecoder(createResp.Body).Decode(&created)

	resp := postRefresh(t, baseURL, accessToken, created.ID)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Refresh_NotFound(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	resp := postRefresh(t, baseURL, accessToken, "does-not-exist")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Refresh_RequiresAuth(t *testing.T) {
	baseURL, _ := newCalendarTestServer(t)

	resp, err := http.Post(baseURL+"/api/calendars/some-id/refresh", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Subscribe_KeepAlarmsTrue_IsStoredAndReturned(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)
	feed := icsFeedServer(t)

	resp := postSubscribeWithKeepAlarms(t, baseURL, accessToken, "0", feed.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !created.KeepAlarms {
		t.Fatalf("expected keepAlarms true in the response, got %+v", created)
	}
}

func TestCalendarHandler_UpdateKeepAlarms_TurningOffClearsReminders(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)
	feed := icsFeedServer(t)

	subscribeResp := postSubscribeWithKeepAlarms(t, baseURL, accessToken, "0", feed.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", true)
	var calendar calendarResponse
	if err := json.NewDecoder(subscribeResp.Body).Decode(&calendar); err != nil {
		t.Fatalf("decode subscribe response: %v", err)
	}

	resp := patchKeepAlarms(t, baseURL, accessToken, calendar.ID, calendar.Name, calendar.Color, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var updated calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.KeepAlarms {
		t.Fatalf("expected keepAlarms false in the response, got %+v", updated)
	}
}

func TestCalendarHandler_UpdateKeepAlarms_RejectsOrdinaryCalendar(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)

	createResp := createCalendar(t, baseURL, accessToken, "11111111-1111-1111-1111-111111111111", "Personal", "#12809CFF")
	var created calendarResponse
	json.NewDecoder(createResp.Body).Decode(&created)

	resp := patchKeepAlarms(t, baseURL, accessToken, created.ID, created.Name, created.Color, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestCalendarHandler_Update_OmittingKeepAlarmsLeavesItUnchanged proves an
// ordinary name/color edit — which never sends keepAlarms — can't
// accidentally turn a Subscription's alarms off.
func TestCalendarHandler_Update_OmittingKeepAlarmsLeavesItUnchanged(t *testing.T) {
	baseURL, accessToken := newCalendarTestServer(t)
	feed := icsFeedServer(t)

	subscribeResp := postSubscribeWithKeepAlarms(t, baseURL, accessToken, "0", feed.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", true)
	var calendar calendarResponse
	if err := json.NewDecoder(subscribeResp.Body).Decode(&calendar); err != nil {
		t.Fatalf("decode subscribe response: %v", err)
	}

	resp := patchCalendar(t, baseURL, accessToken, calendar.ID, "Renamed", "#E2483DFF")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var updated calendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !updated.KeepAlarms {
		t.Fatalf("expected keepAlarms to survive an ordinary name/color edit, got %+v", updated)
	}
}
