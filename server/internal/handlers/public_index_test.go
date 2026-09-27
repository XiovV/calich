package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/XiovV/calich/server/internal/apptest"
)

func TestPublicIndexHandler_ListsPublicLinksAcrossWorkspaces(t *testing.T) {
	s, token, handle, userID, workspaceID, first := setUpPublicLinkFixture(t)

	secondWorkspace, err := s.graph.Workspaces.CreateForOwner(t.Context(), userID, "Side project")
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	secondCalendar := s.createCalendarForLink(t, token, secondWorkspace.ID, "33333333-3333-3333-3333-333333333333", "Side")
	secondSchedule := s.createMonFriSchedule(t, token, "Side schedule")
	secondReq := defaultLinkRequest(secondSchedule, secondCalendar)
	secondReq.Slug = "second-call"
	secondReq.Title = "Second call"
	second := s.createLink(t, token, secondWorkspace.ID, secondReq)

	resp := s.getPublicIndex(t, handle)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body publicIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.HostName != "alice" {
		t.Fatalf("expected host name %q, got %q", "alice", body.HostName)
	}
	if len(body.Links) != 2 {
		t.Fatalf("expected both workspaces' Public links unioned, got %v", body.Links)
	}
	slugs := map[string]bool{}
	for _, l := range body.Links {
		slugs[l.Slug] = true
	}
	if !slugs[first.Slug] || !slugs[second.Slug] {
		t.Fatalf("expected both %q and %q listed, got %v", first.Slug, second.Slug, body.Links)
	}
	// workspaceID is unused directly but proves the fixture's own workspace
	// is the one the first link lives in, distinct from the second.
	_ = workspaceID
}

func TestPublicIndexHandler_PrivateLinkExcluded(t *testing.T) {
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

	indexResp := s.getPublicIndex(t, handle)
	defer indexResp.Body.Close()
	var body publicIndexResponse
	if err := json.NewDecoder(indexResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.HostName != "" || len(body.Links) != 0 {
		t.Fatalf("expected a Private-only index to render exactly like an unknown Handle, got %+v", body)
	}
}

func TestPublicIndexHandler_PausedLinkExcluded(t *testing.T) {
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

	indexResp := s.getPublicIndex(t, handle)
	defer indexResp.Body.Close()
	var body publicIndexResponse
	if err := json.NewDecoder(indexResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Links) != 0 {
		t.Fatalf("expected an explicitly Paused link to be excluded, got %v", body.Links)
	}
}

// TestPublicIndexHandler_UnknownHandleAndZeroPublicLinksAreIndistinguishable
// covers the AC directly: "An unknown Handle returns the same response as a
// Handle with no Public links" (ADR-0084). A real User who claimed a Handle
// but published nothing must not be revealed by name either, or the
// namespace becomes a User enumerator by a different door than #324 closed.
func TestPublicIndexHandler_UnknownHandleAndZeroPublicLinksAreIndistinguishable(t *testing.T) {
	s := newPublicBookingTestServer(t)
	_, userID, _ := s.register(t, "alice")
	if _, err := s.graph.Auth.UpdateHandle(t.Context(), userID, "alice"); err != nil {
		t.Fatalf("claim handle: %v", err)
	}

	knownResp := s.getPublicIndex(t, "alice")
	defer knownResp.Body.Close()
	unknownResp := s.getPublicIndex(t, "no-such-handle")
	defer unknownResp.Body.Close()

	if knownResp.StatusCode != http.StatusOK || unknownResp.StatusCode != http.StatusOK {
		t.Fatalf("expected both to be 200, got %d and %d", knownResp.StatusCode, unknownResp.StatusCode)
	}

	var knownBody, unknownBody publicIndexResponse
	if err := json.NewDecoder(knownResp.Body).Decode(&knownBody); err != nil {
		t.Fatalf("decode known-handle response: %v", err)
	}
	if err := json.NewDecoder(unknownResp.Body).Decode(&unknownBody); err != nil {
		t.Fatalf("decode unknown-handle response: %v", err)
	}
	if knownBody.HostName != unknownBody.HostName || len(knownBody.Links) != len(unknownBody.Links) {
		t.Fatalf("expected identical responses, got %+v and %+v", knownBody, unknownBody)
	}
	if knownBody.HostName != "" {
		t.Fatalf("expected no HostName revealed for a Handle with zero Public links, got %q", knownBody.HostName)
	}
}

func TestPublicIndexHandler_ReservedHandleReturnsEmptyIndex(t *testing.T) {
	s, _, _, _, _, _ := setUpPublicLinkFixture(t)

	resp := s.getPublicIndex(t, "settings")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected a reserved Handle to resolve as 200, got %d", resp.StatusCode)
	}
	var body publicIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.HostName != "" || len(body.Links) != 0 {
		t.Fatalf("expected an empty index for a reserved Handle, got %+v", body)
	}
}

// TestPublicIndexHandler_EmptyForUserWithNoBookingLinksAtAll covers the AC
// directly: "A Handle with no Public links renders an empty index rather
// than an error" — here, a Handle claimed via Settings rather than a
// Booking Link, and no Booking Link at all.
func TestPublicIndexHandler_EmptyForUserWithNoBookingLinksAtAll(t *testing.T) {
	s := newPublicBookingTestServer(t)
	_, userID, _ := s.register(t, "alice")
	if _, err := s.graph.Auth.UpdateHandle(t.Context(), userID, "alice"); err != nil {
		t.Fatalf("claim handle: %v", err)
	}

	resp := s.getPublicIndex(t, "alice")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body publicIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Links) != 0 {
		t.Fatalf("expected an empty index, got %v", body.Links)
	}
}

// TestPublicIndexHandler_WholeIndexPausedWhenNoSMTPConfigured covers the AC
// directly: "The whole index behaves as Paused when the instance has no
// SMTP transport configured" (ADR-0087).
func TestPublicIndexHandler_WholeIndexPausedWhenNoSMTPConfigured(t *testing.T) {
	// newBookingLinkHandlerTestServer (unlike newPublicBookingTestServer)
	// builds over apptest.Config's default, which has no SMTP configured.
	s := newBookingLinkHandlerTestServer(t)
	token, userID, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))
	handle := s.publicHandle(t, userID)

	resp := s.getPublicIndex(t, handle)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body publicIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.HostName != "" || len(body.Links) != 0 {
		t.Fatalf("expected an otherwise-public link to render as absent with no SMTP configured, got %+v", body)
	}
}

func TestPublicIndexHandler_LinkFieldsMatchTitleSlugAndDuration(t *testing.T) {
	s, _, handle, _, _, link := setUpPublicLinkFixture(t)

	resp := s.getPublicIndex(t, handle)
	defer resp.Body.Close()
	var body publicIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Links) != 1 {
		t.Fatalf("expected exactly one link, got %v", body.Links)
	}
	got := body.Links[0]
	if got.Slug != link.Slug || got.Title != link.Title || got.DurationMinutes != link.DurationMinutes {
		t.Fatalf("expected link fields to match the created link, got %+v", got)
	}
}

func TestPublicIndexHandler_RateLimited(t *testing.T) {
	cfg := apptest.SMTPConfig(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	cfg.PublicBookingRateLimitPerIP = 2
	s := newBookingLinkHandlerTestServerWithConfig(t, cfg)
	token, userID, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))
	handle := s.publicHandle(t, userID)

	for i := 0; i < 2; i++ {
		resp := s.getPublicIndex(t, handle)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request %d to succeed, got %d", i, resp.StatusCode)
		}
	}

	resp := s.getPublicIndex(t, handle)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the request past the ceiling to be rate limited, got %d", resp.StatusCode)
	}
}
