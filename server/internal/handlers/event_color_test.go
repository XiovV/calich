// event_color_test.go covers #147/ADR-0043's REST surface for per-Event
// color: create/update round-trip a color, an invalid hex is rejected with
// 400, and a write that fails the Editor Access gate is rejected with 403
// like every other field on the endpoint (ADR-0034).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/XiovV/calendar/server/internal/service"
)

func TestEventHandler_RoundTripsColor(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	createResp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Color:      strPtr("#ff6b35"),
	})
	var created decodedEvent
	json.NewDecoder(createResp.Body).Decode(&created)
	if created.Color == nil || *created.Color != "#FF6B35FF" {
		t.Fatalf("expected created color to round-trip, got %+v", created)
	}

	getResp, err := authenticatedGet(baseURL+"/api/events/"+created.ID, accessToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer getResp.Body.Close()
	var fetched decodedEvent
	json.NewDecoder(getResp.Body).Decode(&fetched)
	if fetched.Color == nil || *fetched.Color != "#FF6B35FF" {
		t.Fatalf("expected fetched color to round-trip, got %+v", fetched)
	}

	// A "Reset to Calendar color" update omits color, which must clear it
	// back to absent rather than leave the previous value in place.
	updateResp := doJSON(t, http.MethodPatch, baseURL+"/api/events/"+created.ID, accessToken, updateEventRequest{
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
	})
	var updated decodedEvent
	json.NewDecoder(updateResp.Body).Decode(&updated)
	if updated.Color != nil {
		t.Fatalf("expected color reset to absent, got %+v", updated)
	}
}

func TestEventHandler_Create_RejectsInvalidColor(t *testing.T) {
	baseURL, accessToken, calendarID := newEventTestServer(t)

	resp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: calendarID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Color:      strPtr("not-a-color"),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestEventHandler_Create_RejectsColorOnReadOnlyCalendarWith403 proves
// setting an Event's color is gated the same way every other write-protected
// field is — a Subscribed Calendar's Owner and Editor alike are clamped to
// read-only (ADR-0032), so this exercises the same ErrCalendarReadOnly ->
// 403 mapping TestEventHandler_Create_RejectsSubscribedCalendarWith403 uses
// for title/time, now carrying a color too.
func TestEventHandler_Create_RejectsColorOnReadOnlyCalendarWith403(t *testing.T) {
	baseURL, accessToken, _, userID, calendars, _ := newEventTestServerWithServices(t)

	sourceURL := "https://example.com/feed.ics"
	subCalendar, err := calendars.Create(context.Background(), userID, "33333333-3333-3333-3333-333333333333", service.CalendarWrite{
		Name: "Feed", Color: "#123456FF", SourceURL: &sourceURL,
	})
	if err != nil {
		t.Fatalf("create subscribed calendar: %v", err)
	}

	resp := postJSON(t, baseURL, accessToken, "/api/events/", createEventRequest{
		ID:         "22222222-2222-2222-2222-222222222222",
		CalendarID: subCalendar.ID,
		Title:      "Standup",
		Start:      "2026-01-01T09:00:00Z",
		End:        "2026-01-01T10:00:00Z",
		Color:      strPtr("#123456"),
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
