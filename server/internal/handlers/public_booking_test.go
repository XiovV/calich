package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/apptest"
)

// newPublicBookingTestServer is newBookingLinkHandlerTestServerWithConfig
// over an SMTP-configured graph — the public Booking Link page (#324,
// ADR-0087) clamps every link to Paused with no SMTP transport configured,
// so a test asserting on an otherwise-bookable link needs one.
func newPublicBookingTestServer(t *testing.T) *bookingLinkHandlerTestServer {
	t.Helper()

	cfg := apptest.SMTPConfig(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	return newBookingLinkHandlerTestServerWithConfig(t, cfg)
}

// publicHandle returns userID's own Handle, claimed automatically by their
// first Booking Link (ADR-0084).
func (s *bookingLinkHandlerTestServer) publicHandle(t *testing.T, userID int64) string {
	t.Helper()

	user, err := s.graph.UserRepo.GetByID(t.Context(), userID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.Handle == nil {
		t.Fatalf("expected user %d to have claimed a handle", userID)
	}
	return *user.Handle
}

func (s *bookingLinkHandlerTestServer) getPublicLink(t *testing.T, handle, slug string) *http.Response {
	t.Helper()
	return s.doNoWorkspace(t, http.MethodGet, "/api/public/"+url.PathEscape(handle)+"/"+url.PathEscape(slug), "", nil)
}

func (s *bookingLinkHandlerTestServer) getPublicSlots(t *testing.T, handle, slug string, year, month int) *http.Response {
	t.Helper()
	path := "/api/public/" + url.PathEscape(handle) + "/" + url.PathEscape(slug) + "/slots?year=" + strconv.Itoa(year) + "&month=" + strconv.Itoa(month)
	return s.doNoWorkspace(t, http.MethodGet, path, "", nil)
}

func (s *bookingLinkHandlerTestServer) getPublicIndex(t *testing.T, handle string) *http.Response {
	t.Helper()
	return s.doNoWorkspace(t, http.MethodGet, "/api/public/"+url.PathEscape(handle), "", nil)
}

// setUpPublicLinkFixture registers alice on an SMTP-configured graph, gives
// her a book-into Calendar, a Mon-Fri 09:00-17:00 Schedule in Etc/UTC, and a
// Public Booking Link, returning her claimed Handle alongside the link.
func setUpPublicLinkFixture(t *testing.T) (s *bookingLinkHandlerTestServer, token, handle string, userID, workspaceID int64, link bookingLinkResponse) {
	t.Helper()

	s = newPublicBookingTestServer(t)
	token, userID, workspaceID = s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createMonFriSchedule(t, token, "Default")

	req := defaultLinkRequest(scheduleID, calendarID)
	req.MinimumNoticeMinutes = 0
	req.BookingHorizonDays = 90
	link = s.createLink(t, token, workspaceID, req)

	handle = s.publicHandle(t, userID)
	return s, token, handle, userID, workspaceID, link
}

func TestPublicBookingHandler_Get_ReturnsHostAndLinkDetails(t *testing.T) {
	s, _, handle, _, _, link := setUpPublicLinkFixture(t)

	resp := s.getPublicLink(t, handle, link.Slug)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body publicBookingLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.HostName != "alice" {
		t.Fatalf("expected host name %q, got %q", "alice", body.HostName)
	}
	if body.HostTimezone != "Etc/UTC" {
		t.Fatalf("expected host timezone %q, got %q", "Etc/UTC", body.HostTimezone)
	}
	if body.Title != link.Title || body.DurationMinutes != link.DurationMinutes || body.BookingHorizonDays != link.BookingHorizonDays {
		t.Fatalf("expected link fields to match the created link, got %+v", body)
	}
	if body.Paused {
		t.Fatalf("expected an SMTP-configured, Public, fully-accessible link to not be paused")
	}
}

func TestPublicBookingHandler_Get_PrivateLinkReachableDirectly(t *testing.T) {
	s, token, handle, _, workspaceID, link := setUpPublicLinkFixture(t)

	updateReq := bookingLinkWriteRequest{
		Title: link.Title, Slug: link.Slug, DurationMinutes: link.DurationMinutes,
		Visibility: "private", AvailabilityScheduleID: link.AvailabilityScheduleID,
		BookIntoCalendarID: link.BookIntoCalendarID, MinimumNoticeMinutes: 0, BookingHorizonDays: 90,
		TasksInConflictSet: link.TasksInConflictSet,
	}
	resp := s.do(t, http.MethodPatch, "/api/booking-links/"+strconv.FormatInt(link.ID, 10), token, workspaceID, updateReq)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 setting private, got %d", resp.StatusCode)
	}

	getResp := s.getPublicLink(t, handle, link.Slug)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected a Private link to still be reachable by its direct URL, got %d", getResp.StatusCode)
	}

	var body publicBookingLinkResponse
	if err := json.NewDecoder(getResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Paused {
		t.Fatalf("expected a Private (not Paused) link to render as not paused")
	}
}

func TestPublicBookingHandler_Get_UnknownHandleReturns404(t *testing.T) {
	s, _, _, _, _, link := setUpPublicLinkFixture(t)

	resp := s.getPublicLink(t, "no-such-handle", link.Slug)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPublicBookingHandler_Get_UnknownSlugReturns404(t *testing.T) {
	s, _, handle, _, _, _ := setUpPublicLinkFixture(t)

	resp := s.getPublicLink(t, handle, "no-such-slug")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// TestPublicBookingHandler_UnknownHandleAndUnknownSlugAreIndistinguishable
// covers the AC directly: an unknown Handle and an unknown Slug under a
// known Handle must render the identical response, so the namespace cannot
// enumerate Users (ADR-0084).
func TestPublicBookingHandler_UnknownHandleAndUnknownSlugAreIndistinguishable(t *testing.T) {
	s, _, handle, _, _, _ := setUpPublicLinkFixture(t)

	unknownHandleResp := s.getPublicLink(t, "no-such-handle", "whatever")
	defer unknownHandleResp.Body.Close()
	unknownSlugResp := s.getPublicLink(t, handle, "no-such-slug")
	defer unknownSlugResp.Body.Close()

	if unknownHandleResp.StatusCode != unknownSlugResp.StatusCode {
		t.Fatalf("expected identical status codes, got %d and %d", unknownHandleResp.StatusCode, unknownSlugResp.StatusCode)
	}

	var unknownHandleBody, unknownSlugBody map[string]any
	if err := json.NewDecoder(unknownHandleResp.Body).Decode(&unknownHandleBody); err != nil {
		t.Fatalf("decode unknown-handle response: %v", err)
	}
	if err := json.NewDecoder(unknownSlugResp.Body).Decode(&unknownSlugBody); err != nil {
		t.Fatalf("decode unknown-slug response: %v", err)
	}
	unknownHandleError, _ := unknownHandleBody["error"].(map[string]any)
	unknownSlugError, _ := unknownSlugBody["error"].(map[string]any)
	if unknownHandleError["code"] != unknownSlugError["code"] || unknownHandleError["message"] != unknownSlugError["message"] {
		t.Fatalf("expected identical error bodies, got %v and %v", unknownHandleBody, unknownSlugBody)
	}
}

func TestPublicBookingHandler_Get_ReservedHandleReturns404(t *testing.T) {
	s, _, _, _, _, link := setUpPublicLinkFixture(t)

	resp := s.getPublicLink(t, "settings", link.Slug)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected a reserved Handle to resolve as 404, got %d", resp.StatusCode)
	}
}

func TestPublicBookingHandler_Get_PausedWhenVisibilityExplicitlyPaused(t *testing.T) {
	s, token, handle, _, workspaceID, link := setUpPublicLinkFixture(t)

	updateReq := bookingLinkWriteRequest{
		Title: link.Title, Slug: link.Slug, DurationMinutes: link.DurationMinutes,
		Visibility: "paused", AvailabilityScheduleID: link.AvailabilityScheduleID,
		BookIntoCalendarID: link.BookIntoCalendarID, MinimumNoticeMinutes: 0, BookingHorizonDays: 90,
		TasksInConflictSet: link.TasksInConflictSet,
	}
	resp := s.do(t, http.MethodPatch, "/api/booking-links/"+strconv.FormatInt(link.ID, 10), token, workspaceID, updateReq)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 pausing, got %d", resp.StatusCode)
	}

	getResp := s.getPublicLink(t, handle, link.Slug)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected a Paused link to still render, got %d", getResp.StatusCode)
	}
	var body publicBookingLinkResponse
	if err := json.NewDecoder(getResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Paused {
		t.Fatalf("expected paused=true")
	}
}

func TestPublicBookingHandler_Get_PausedWhenNoSMTPConfigured(t *testing.T) {
	// newBookingLinkHandlerTestServer (unlike newPublicBookingTestServer)
	// builds over apptest.Config's default, which has no SMTP configured.
	s := newBookingLinkHandlerTestServer(t)
	token, userID, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	link := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))
	handle := s.publicHandle(t, userID)

	resp := s.getPublicLink(t, handle, link.Slug)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body publicBookingLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Paused {
		t.Fatalf("expected a link on an instance with no SMTP configured to render as paused")
	}
}

// TestPublicBookingHandler_Get_PausedWhenBookIntoCalendarInAnotherWorkspace
// simulates the host losing Access to their own Book-into Calendar
// (ADR-0087's "the host losing Owner or Editor Access to the Book-into
// Calendar") by pointing the stored link at a Workspace the Calendar isn't
// actually in — the same mismatch BookingLinkService.validateWrite itself
// treats as "not found", here re-checked at read time rather than trusted
// from create time.
func TestPublicBookingHandler_Get_PausedWhenBookIntoCalendarInAnotherWorkspace(t *testing.T) {
	s, _, handle, _, _, link := setUpPublicLinkFixture(t)
	_, _, otherWorkspaceID := s.register(t, "bob")

	if _, err := s.graph.DB.Exec("UPDATE booking_links SET workspace_id = ? WHERE id = ?", otherWorkspaceID, link.ID); err != nil {
		t.Fatalf("mismatch booking link's workspace: %v", err)
	}

	resp := s.getPublicLink(t, handle, link.Slug)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body publicBookingLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Paused {
		t.Fatalf("expected a link whose Book-into Calendar sits outside its own Workspace to render as paused")
	}
}

func TestPublicBookingHandler_Slots_ReturnsDerivedSlots(t *testing.T) {
	s, _, handle, _, _, link := setUpPublicLinkFixture(t)

	// A Monday deep enough into the horizon that "now" can never race past
	// it, mirroring booking_link_slots_test.go's own fixture — guards
	// against a flake where "this calendar month" has no weekday left in it.
	monday := nextWeekday(time.Now().UTC().AddDate(0, 0, 14), time.Monday)
	resp := s.getPublicSlots(t, handle, link.Slug, monday.Year(), int(monday.Month()))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body publicSlotsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode slots response: %v", err)
	}
	if len(body.Slots) == 0 {
		t.Fatalf("expected at least one derived slot from a Mon-Fri 09:00-17:00 schedule with no conflicts")
	}
}

func TestPublicBookingHandler_Slots_EmptyWhenPaused(t *testing.T) {
	s, token, handle, _, workspaceID, link := setUpPublicLinkFixture(t)

	updateReq := bookingLinkWriteRequest{
		Title: link.Title, Slug: link.Slug, DurationMinutes: link.DurationMinutes,
		Visibility: "paused", AvailabilityScheduleID: link.AvailabilityScheduleID,
		BookIntoCalendarID: link.BookIntoCalendarID, MinimumNoticeMinutes: 0, BookingHorizonDays: 90,
		TasksInConflictSet: link.TasksInConflictSet,
	}
	resp := s.do(t, http.MethodPatch, "/api/booking-links/"+strconv.FormatInt(link.ID, 10), token, workspaceID, updateReq)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 pausing, got %d", resp.StatusCode)
	}

	now := time.Now().UTC()
	slotsResp := s.getPublicSlots(t, handle, link.Slug, now.Year(), int(now.Month()))
	defer slotsResp.Body.Close()
	if slotsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", slotsResp.StatusCode)
	}
	var body publicSlotsResponse
	if err := json.NewDecoder(slotsResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode slots response: %v", err)
	}
	if len(body.Slots) != 0 {
		t.Fatalf("expected no slots for a Paused link, got %v", body.Slots)
	}
}

func TestPublicBookingHandler_Slots_InvalidMonthRejected(t *testing.T) {
	s, _, handle, _, _, link := setUpPublicLinkFixture(t)

	resp := s.getPublicSlots(t, handle, link.Slug, 2026, 13)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPublicBookingHandler_RateLimited(t *testing.T) {
	cfg := apptest.SMTPConfig(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	cfg.PublicBookingRateLimitPerIP = 2
	s := newBookingLinkHandlerTestServerWithConfig(t, cfg)
	token, userID, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	link := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))
	handle := s.publicHandle(t, userID)

	for i := 0; i < 2; i++ {
		resp := s.getPublicLink(t, handle, link.Slug)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request %d to succeed, got %d", i, resp.StatusCode)
		}
	}

	resp := s.getPublicLink(t, handle, link.Slug)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the request past the ceiling to be rate limited, got %d", resp.StatusCode)
	}
}
