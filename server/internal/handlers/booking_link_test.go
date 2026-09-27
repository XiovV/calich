package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/config"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/service"
)

// bookingLinkHandlerTestServer bundles the HTTP surface Booking Links need
// (#322, ADR-0084, ADR-0087): Register (to mint real Users, each with their
// own Workspace), enough of /api/calendars to create the Calendars a link
// references, enough of /api/availability-schedules to create the Schedule
// a link references, and the /api/booking-links routes, gated by
// RequireWorkspace exactly like router.New wires them.
type bookingLinkHandlerTestServer struct {
	srv        *httptest.Server
	graph      *service.Graph
	workspaces *service.WorkspaceService
}

func newBookingLinkHandlerTestServer(t *testing.T) *bookingLinkHandlerTestServer {
	t.Helper()

	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	return newBookingLinkHandlerTestServerWithConfig(t, cfg)
}

// newBookingLinkHandlerTestServerWithConfig is newBookingLinkHandlerTestServer
// for a test that needs a setting other than its defaults — the public
// booking handler tests (#324) need an SMTP-configured graph to ever see a
// link that isn't Paused, since ADR-0087 clamps every link to Paused on a
// deployment with no SMTP transport at all.
func newBookingLinkHandlerTestServerWithConfig(t *testing.T, cfg config.Config) *bookingLinkHandlerTestServer {
	t.Helper()

	g := newTestGraphWithConfig(t, cfg)

	workspaces := g.Workspaces
	auth := g.Auth

	authHandler := NewAuthHandler(auth, g.RateLimiter, false, false, false, true)
	calendarHandler := NewCalendarHandler(g.Calendars, g.Events, g.Imports, g.Subscriptions, g.Connections, g.AttachmentStore)
	scheduleHandler := NewAvailabilityScheduleHandler(g.AvailabilitySchedules)
	linkHandler := NewBookingLinkHandler(g.BookingLinks)
	publicHandler := NewPublicBookingHandler(g.PublicBookings, g.PublicBookingRateLimiter)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)

	r.Route("/api/calendars", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))

		r.With(httpauth.RequireWorkspace(workspaces)).Post("/", calendarHandler.Create)
		r.Delete("/{id}", calendarHandler.Delete)
	})

	r.Route("/api/availability-schedules", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))

		r.Get("/", scheduleHandler.List)
		r.Post("/", scheduleHandler.Create)
		r.Delete("/{id}", scheduleHandler.Delete)
	})

	r.Route("/api/booking-links", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))
		r.Use(httpauth.RequireWorkspace(workspaces))

		r.Get("/", linkHandler.List)
		r.Post("/", linkHandler.Create)
		r.Patch("/{id}", linkHandler.Update)
		r.Delete("/{id}", linkHandler.Delete)
		r.Post("/{id}/duplicate", linkHandler.Duplicate)
		r.Get("/{id}/slots", linkHandler.Slots)
		r.Put("/{id}/conflict-set/calendars/{calendarId}", linkHandler.AddConflictCalendar)
		r.Delete("/{id}/conflict-set/calendars/{calendarId}", linkHandler.RemoveConflictCalendar)
	})

	r.Route("/api/public", func(r chi.Router) {
		r.Get("/{handle}/{slug}", publicHandler.Get)
		r.Get("/{handle}/{slug}/slots", publicHandler.Slots)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &bookingLinkHandlerTestServer{srv: srv, graph: g, workspaces: workspaces}
}

func (s *bookingLinkHandlerTestServer) register(t *testing.T, username string) (accessToken string, userID, workspaceID int64) {
	t.Helper()
	ctx := context.Background()

	body, err := json.Marshal(registerRequest{Name: username, Email: username + "@example.com", Password: "hunter22"})
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}
	resp, err := http.Post(s.srv.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 registering %s, got %d", username, resp.StatusCode)
	}
	var logged loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&logged); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	user, err := s.graph.UserRepo.GetByEmail(ctx, username+"@example.com")
	if err != nil {
		t.Fatalf("get %s: %v", username, err)
	}
	workspaceList, err := s.workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(workspaceList) != 1 {
		t.Fatalf("list workspaces for %s: %v (%d)", username, err, len(workspaceList))
	}
	workspaceID = workspaceList[0].ID

	// Registration seeds the fresh Workspace with its own default
	// Calendars — irrelevant noise for these tests, which reason about an
	// exact Conflict set built from Calendars they create themselves.
	s.clearDefaultCalendars(t, user.ID, workspaceID)

	return logged.AccessToken, user.ID, workspaceID
}

// clearDefaultCalendars deletes every Calendar userID already owns in
// workspaceID — Register seeds a fresh Workspace with its own defaults,
// which would otherwise pollute a test's Conflict set assertions with
// Calendars it never created itself.
func (s *bookingLinkHandlerTestServer) clearDefaultCalendars(t *testing.T, userID, workspaceID int64) {
	t.Helper()
	ctx := context.Background()

	calendars, err := s.graph.CalendarRepo.ListByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		t.Fatalf("list default calendars: %v", err)
	}
	for _, c := range calendars {
		if err := s.graph.CalendarRepo.Delete(ctx, userID, c.ID); err != nil {
			t.Fatalf("delete default calendar %s: %v", c.ID, err)
		}
	}
}

// createCalendarForLink creates an owned Calendar named name in workspaceID
// for accessToken, returning its id.
func (s *bookingLinkHandlerTestServer) createCalendarForLink(t *testing.T, accessToken string, workspaceID int64, id, name string) string {
	t.Helper()

	resp := createCalendar(t, s.srv.URL, accessToken, strconv.FormatInt(workspaceID, 10), id, name, "#12809CFF")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating calendar, got %d", resp.StatusCode)
	}
	return id
}

// createScheduleForLink creates an Availability Schedule for accessToken
// (private to the caller, no Workspace of its own) and returns its id.
func (s *bookingLinkHandlerTestServer) createScheduleForLink(t *testing.T, accessToken, name string) int64 {
	t.Helper()

	resp := s.doNoWorkspace(t, http.MethodPost, "/api/availability-schedules/", accessToken, createAvailabilityScheduleRequest{Name: name, Tzid: "Etc/UTC"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating schedule, got %d", resp.StatusCode)
	}
	var schedule availabilityScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&schedule); err != nil {
		t.Fatalf("decode create schedule response: %v", err)
	}
	return schedule.ID
}

func defaultLinkRequest(scheduleID int64, calendarID string) bookingLinkWriteRequest {
	return bookingLinkWriteRequest{
		Title:                  "Intro call",
		Slug:                   "intro-call",
		DurationMinutes:        30,
		Visibility:             "public",
		AvailabilityScheduleID: scheduleID,
		BookIntoCalendarID:     calendarID,
		MinimumNoticeMinutes:   240,
		BookingHorizonDays:     60,
		TasksInConflictSet:     true,
	}
}

func (s *bookingLinkHandlerTestServer) createLink(t *testing.T, accessToken string, workspaceID int64, req bookingLinkWriteRequest) bookingLinkResponse {
	t.Helper()

	resp := s.do(t, http.MethodPost, "/api/booking-links/", accessToken, workspaceID, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating booking link, got %d", resp.StatusCode)
	}
	var link bookingLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatalf("decode create booking link response: %v", err)
	}
	return link
}

func (s *bookingLinkHandlerTestServer) listLinks(t *testing.T, accessToken string, workspaceID int64) []bookingLinkResponse {
	t.Helper()

	resp := s.do(t, http.MethodGet, "/api/booking-links/", accessToken, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing booking links, got %d", resp.StatusCode)
	}
	var list []bookingLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list booking links response: %v", err)
	}
	return list
}

func (s *bookingLinkHandlerTestServer) do(t *testing.T, method, path, accessToken string, workspaceID int64, body any) *http.Response {
	t.Helper()

	req := s.buildRequest(t, method, path, accessToken, body)
	req.Header.Set("X-Workspace-Id", strconv.FormatInt(workspaceID, 10))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// doNoWorkspace is do without an X-Workspace-Id header, for the
// Availability Schedule routes, which carry no Workspace of their own.
func (s *bookingLinkHandlerTestServer) doNoWorkspace(t *testing.T, method, path, accessToken string, body any) *http.Response {
	t.Helper()

	req := s.buildRequest(t, method, path, accessToken, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func (s *bookingLinkHandlerTestServer) buildRequest(t *testing.T, method, path, accessToken string, body any) *http.Request {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, s.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestBookingLinkHandler_Create_SeedsConflictSetFromOwnedCalendarsPlusBookInto(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")

	bookInto := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	other := s.createCalendarForLink(t, token, workspaceID, "22222222-2222-2222-2222-222222222222", "Personal")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	link := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, bookInto))

	if link.BookIntoCalendarID != bookInto {
		t.Fatalf("expected book-into %q, got %q", bookInto, link.BookIntoCalendarID)
	}
	if len(link.ConflictCalendarIDs) != 2 {
		t.Fatalf("expected both owned calendars seeded into the conflict set, got %v", link.ConflictCalendarIDs)
	}
	found := map[string]bool{}
	for _, id := range link.ConflictCalendarIDs {
		found[id] = true
	}
	if !found[bookInto] || !found[other] {
		t.Fatalf("expected both %q and %q in the conflict set, got %v", bookInto, other, link.ConflictCalendarIDs)
	}
	if !link.TasksInConflictSet {
		t.Fatalf("expected the Tasks row set by default, got false")
	}
	if link.Visibility != "public" {
		t.Fatalf("expected visibility %q, got %q", "public", link.Visibility)
	}
}

func TestBookingLinkHandler_Create_EmptyTitleRejected(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	req := defaultLinkRequest(scheduleID, calendarID)
	req.Title = "   "
	resp := s.do(t, http.MethodPost, "/api/booking-links/", token, workspaceID, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBookingLinkHandler_Create_InvalidSlugRejected(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	req := defaultLinkRequest(scheduleID, calendarID)
	req.Slug = "-not valid-"
	resp := s.do(t, http.MethodPost, "/api/booking-links/", token, workspaceID, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBookingLinkHandler_Create_InvalidDurationRejected(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	req := defaultLinkRequest(scheduleID, calendarID)
	req.DurationMinutes = 0
	resp := s.do(t, http.MethodPost, "/api/booking-links/", token, workspaceID, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBookingLinkHandler_Create_InvalidVisibilityRejected(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	req := defaultLinkRequest(scheduleID, calendarID)
	req.Visibility = "hidden"
	resp := s.do(t, http.MethodPost, "/api/booking-links/", token, workspaceID, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBookingLinkHandler_Create_UnknownScheduleRejected(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")

	req := defaultLinkRequest(999999, calendarID)
	resp := s.do(t, http.MethodPost, "/api/booking-links/", token, workspaceID, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestBookingLinkHandler_Create_BookIntoRestrictedToOwnedOrEditableCalendars
// covers the AC directly: "Book-into is restricted to Calendars the User
// owns or can edit in that Workspace" — bob has no Access at all to alice's
// Calendar.
func TestBookingLinkHandler_Create_BookIntoRestrictedToOwnedOrEditableCalendars(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")
	bobToken, _, bobWorkspaceID := s.register(t, "bob")

	aliceCalendar := s.createCalendarForLink(t, aliceToken, aliceWorkspaceID, "11111111-1111-1111-1111-111111111111", "Alice's calendar")
	bobScheduleID := s.createScheduleForLink(t, bobToken, "Default")

	req := defaultLinkRequest(bobScheduleID, aliceCalendar)
	resp := s.do(t, http.MethodPost, "/api/booking-links/", bobToken, bobWorkspaceID, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 booking into a calendar with no access, got %d", resp.StatusCode)
	}
}

func TestBookingLinkHandler_Create_DuplicateSlugForSameUserIsConflict(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	resp := s.do(t, http.MethodPost, "/api/booking-links/", token, workspaceID, defaultLinkRequest(scheduleID, calendarID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

// TestBookingLinkHandler_Create_SlugUniquePerUserNotInstanceWide covers the
// AC directly: "two Users may both publish intro-call".
func TestBookingLinkHandler_Create_SlugUniquePerUserNotInstanceWide(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")
	bobToken, _, bobWorkspaceID := s.register(t, "bob")

	aliceCalendar := s.createCalendarForLink(t, aliceToken, aliceWorkspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	aliceScheduleID := s.createScheduleForLink(t, aliceToken, "Default")
	bobCalendar := s.createCalendarForLink(t, bobToken, bobWorkspaceID, "22222222-2222-2222-2222-222222222222", "Work")
	bobScheduleID := s.createScheduleForLink(t, bobToken, "Default")

	s.createLink(t, aliceToken, aliceWorkspaceID, defaultLinkRequest(aliceScheduleID, aliceCalendar))
	bobsLink := s.createLink(t, bobToken, bobWorkspaceID, defaultLinkRequest(bobScheduleID, bobCalendar))

	if bobsLink.Slug != "intro-call" {
		t.Fatalf("expected bob to also get slug %q, got %q", "intro-call", bobsLink.Slug)
	}
}

// TestBookingLinkHandler_Create_ClaimsHandleOnFirstLink covers ADR-0084:
// creating a first Booking Link claims a Handle when the User has none.
func TestBookingLinkHandler_Create_ClaimsHandleOnFirstLink(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, userID, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	before, err := s.graph.UserRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if before.Handle != nil {
		t.Fatalf("expected no Handle before creating a link, got %v", *before.Handle)
	}

	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	after, err := s.graph.UserRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if after.Handle == nil {
		t.Fatalf("expected a Handle to be claimed after the first booking link")
	}
}

// TestBookingLinkHandler_Create_DoesNotOverwriteAnExistingHandle guards the
// other side of the auto-claim: a User who already has a Handle keeps it.
func TestBookingLinkHandler_Create_DoesNotOverwriteAnExistingHandle(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, userID, workspaceID := s.register(t, "alice")
	if _, err := s.graph.Auth.UpdateHandle(context.Background(), userID, "already-chosen"); err != nil {
		t.Fatalf("claim handle: %v", err)
	}
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	user, err := s.graph.UserRepo.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.Handle == nil || *user.Handle != "already-chosen" {
		t.Fatalf("expected the existing handle to survive, got %v", user.Handle)
	}
}

func TestBookingLinkHandler_Update_ReplacesFieldsAndRepinsNewBookInto(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	firstCalendar := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	secondCalendar := s.createCalendarForLink(t, token, workspaceID, "22222222-2222-2222-2222-222222222222", "Personal")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	link := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, firstCalendar))

	updateReq := defaultLinkRequest(scheduleID, secondCalendar)
	updateReq.Title = "Deep dive"
	updateReq.Slug = "deep-dive"
	updateReq.Visibility = "private"
	resp := s.do(t, http.MethodPatch, "/api/booking-links/"+strconv.FormatInt(link.ID, 10), token, workspaceID, updateReq)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 updating, got %d", resp.StatusCode)
	}
	var updated bookingLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Title != "Deep dive" || updated.Slug != "deep-dive" || updated.Visibility != "private" {
		t.Fatalf("expected fields replaced, got %+v", updated)
	}
	if updated.BookIntoCalendarID != secondCalendar {
		t.Fatalf("expected book-into changed to %q, got %q", secondCalendar, updated.BookIntoCalendarID)
	}
	found := false
	for _, id := range updated.ConflictCalendarIDs {
		if id == secondCalendar {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the new book-into calendar re-pinned into the conflict set, got %v", updated.ConflictCalendarIDs)
	}
}

func TestBookingLinkHandler_Delete_RemovesIt(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	link := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	resp := s.do(t, http.MethodDelete, "/api/booking-links/"+strconv.FormatInt(link.ID, 10), token, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	links := s.listLinks(t, token, workspaceID)
	if len(links) != 0 {
		t.Fatalf("expected no links left, got %v", links)
	}
}

func TestBookingLinkHandler_Duplicate_DerivesFreshSlugAndTitleAndCopiesConflictSet(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	bookInto := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	extra := s.createCalendarForLink(t, token, workspaceID, "22222222-2222-2222-2222-222222222222", "Personal")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	original := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, bookInto))
	// Customize the conflict set before duplicating, to prove Duplicate
	// copies it rather than reseeding from scratch.
	removeResp := s.do(t, http.MethodDelete, "/api/booking-links/"+strconv.FormatInt(original.ID, 10)+"/conflict-set/calendars/"+extra, token, workspaceID, nil)
	defer removeResp.Body.Close()
	if removeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 removing conflict calendar, got %d", removeResp.StatusCode)
	}

	resp := s.do(t, http.MethodPost, "/api/booking-links/"+strconv.FormatInt(original.ID, 10)+"/duplicate", token, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 duplicating, got %d", resp.StatusCode)
	}
	var duplicate bookingLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&duplicate); err != nil {
		t.Fatalf("decode duplicate response: %v", err)
	}

	if duplicate.Slug != "intro-call-copy" {
		t.Fatalf("expected slug %q, got %q", "intro-call-copy", duplicate.Slug)
	}
	if duplicate.Title != "Intro call (copy)" {
		t.Fatalf("expected title %q, got %q", "Intro call (copy)", duplicate.Title)
	}
	if len(duplicate.ConflictCalendarIDs) != 1 || duplicate.ConflictCalendarIDs[0] != bookInto {
		t.Fatalf("expected the duplicate to copy the customized (single-calendar) conflict set, got %v", duplicate.ConflictCalendarIDs)
	}

	// Duplicating again disambiguates further.
	secondResp := s.do(t, http.MethodPost, "/api/booking-links/"+strconv.FormatInt(original.ID, 10)+"/duplicate", token, workspaceID, nil)
	defer secondResp.Body.Close()
	var secondDuplicate bookingLinkResponse
	if err := json.NewDecoder(secondResp.Body).Decode(&secondDuplicate); err != nil {
		t.Fatalf("decode second duplicate response: %v", err)
	}
	if secondDuplicate.Slug != "intro-call-copy-2" {
		t.Fatalf("expected slug %q, got %q", "intro-call-copy-2", secondDuplicate.Slug)
	}
}

func TestBookingLinkHandler_ConflictSet_AddAndRemove(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	bookInto := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	other := s.createCalendarForLink(t, token, workspaceID, "22222222-2222-2222-2222-222222222222", "Personal")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	link := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, bookInto))

	removeResp := s.do(t, http.MethodDelete, "/api/booking-links/"+strconv.FormatInt(link.ID, 10)+"/conflict-set/calendars/"+other, token, workspaceID, nil)
	defer removeResp.Body.Close()
	if removeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 removing, got %d", removeResp.StatusCode)
	}

	addResp := s.do(t, http.MethodPut, "/api/booking-links/"+strconv.FormatInt(link.ID, 10)+"/conflict-set/calendars/"+other, token, workspaceID, nil)
	defer addResp.Body.Close()
	if addResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 adding, got %d", addResp.StatusCode)
	}

	links := s.listLinks(t, token, workspaceID)
	if len(links) != 1 || len(links[0].ConflictCalendarIDs) != 2 {
		t.Fatalf("expected both calendars back in the conflict set, got %v", links)
	}
}

// TestBookingLinkHandler_ConflictSet_CannotRemoveBookIntoCalendar covers the
// AC directly: "the Book-into Calendar present and unremovable".
func TestBookingLinkHandler_ConflictSet_CannotRemoveBookIntoCalendar(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	bookInto := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")

	link := s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, bookInto))

	resp := s.do(t, http.MethodDelete, "/api/booking-links/"+strconv.FormatInt(link.ID, 10)+"/conflict-set/calendars/"+bookInto, token, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	links := s.listLinks(t, token, workspaceID)
	found := false
	for _, id := range links[0].ConflictCalendarIDs {
		if id == bookInto {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the book-into calendar to survive the refused removal, got %v", links[0].ConflictCalendarIDs)
	}
}

func TestBookingLinkHandler_SecondUserCannotReadUpdateOrDeleteFirsts(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	aliceToken, _, workspaceID := s.register(t, "alice")
	bobToken, bobID, _ := s.register(t, "bob")
	// Add bob to alice's workspace to prove privacy holds regardless of
	// shared Workspace membership, the same posture task_list_test.go
	// exercises for Task Lists.
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}

	calendarID := s.createCalendarForLink(t, aliceToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, aliceToken, "Default")
	link := s.createLink(t, aliceToken, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	// bob's update body must reference resources *he* can legally use — a
	// schedule or calendar of alice's would already be refused by
	// validateWrite's own ownership checks (400), which would prove nothing
	// about the link-ownership check this test is actually about.
	bobCalendarID := s.createCalendarForLink(t, bobToken, workspaceID, "99999999-9999-9999-9999-999999999999", "Bob's calendar")
	bobScheduleID := s.createScheduleForLink(t, bobToken, "Bob's schedule")

	updateResp := s.do(t, http.MethodPatch, "/api/booking-links/"+strconv.FormatInt(link.ID, 10), bobToken, workspaceID, defaultLinkRequest(bobScheduleID, bobCalendarID))
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 updating, got %d", updateResp.StatusCode)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/booking-links/"+strconv.FormatInt(link.ID, 10), bobToken, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 deleting, got %d", deleteResp.StatusCode)
	}

	aliceLinks := s.listLinks(t, aliceToken, workspaceID)
	if len(aliceLinks) != 1 || aliceLinks[0].Title != "Intro call" {
		t.Fatalf("expected alice's link to survive bob's attempts untouched, got %v", aliceLinks)
	}
}

// TestBookingLinkHandler_CascadesOnWorkspaceDeletion covers booking_links.
// workspace_id's ON DELETE CASCADE (ADR-0084, mirroring task_lists').
func TestBookingLinkHandler_CascadesOnWorkspaceDeletion(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	if _, err := s.graph.DB.Exec("DELETE FROM workspaces WHERE id = ?", workspaceID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}

	var count int
	if err := s.graph.DB.QueryRow("SELECT COUNT(*) FROM booking_links WHERE workspace_id = ?", workspaceID).Scan(&count); err != nil {
		t.Fatalf("count booking links: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleting the workspace to cascade the booking link, got %d remaining", count)
	}
}

// TestBookingLinkHandler_ReferencedScheduleCannotBeDeleted closes ADR-0085's
// loop, which this ticket is what finally gives it a referencing entity to
// enforce against: "deleting a Schedule still referenced is refused".
func TestBookingLinkHandler_ReferencedScheduleCannotBeDeleted(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	resp := s.doNoWorkspace(t, http.MethodDelete, "/api/availability-schedules/"+strconv.FormatInt(scheduleID, 10), token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 deleting a schedule still referenced by a booking link, got %d", resp.StatusCode)
	}
}

// TestBookingLinkHandler_ReferencedBookIntoCalendarCannotBeDeleted covers
// the migration's other half of the same guarantee ADR-0087 states for the
// Book-into Calendar, mirroring the Schedule test above.
func TestBookingLinkHandler_ReferencedBookIntoCalendarCannotBeDeleted(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	deleteReq, err := http.NewRequest(http.MethodDelete, s.srv.URL+"/api/calendars/"+calendarID, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("DELETE /api/calendars/%s: %v", calendarID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 deleting a calendar still referenced by a booking link, got %d", resp.StatusCode)
	}
}

// TestBookingLinkHandler_CascadesOnUserDeletion covers booking_links.
// user_id's ON DELETE CASCADE (ADR-0084's "cascade on both"), isolated from
// the workspace_id cascade already covered above: alice's own account is
// deleted directly via AccountService, dispositioning her owned Calendar.
func TestBookingLinkHandler_CascadesOnUserDeletion(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	token, userID, workspaceID := s.register(t, "alice")
	calendarID := s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createScheduleForLink(t, token, "Default")
	s.createLink(t, token, workspaceID, defaultLinkRequest(scheduleID, calendarID))

	ctx := context.Background()
	impact, err := s.graph.Accounts.DeleteImpact(ctx, userID)
	if err != nil {
		t.Fatalf("delete impact: %v", err)
	}
	dispositions := make([]service.CalendarDisposition, len(impact.Calendars))
	for i, c := range impact.Calendars {
		dispositions[i] = service.CalendarDisposition{CalendarID: c.ID, Disposition: service.DispositionDelete}
	}
	if err := s.graph.Accounts.Delete(ctx, userID, dispositions); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	var count int
	if err := s.graph.DB.QueryRow("SELECT COUNT(*) FROM booking_links WHERE user_id = ?", userID).Scan(&count); err != nil {
		t.Fatalf("count booking links: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleting the user to cascade their booking link, got %d remaining", count)
	}
}

// TestBookingLinkHandler_Create_BookIntoAcceptsEditorSharedCalendar covers
// the "or can edit" half of "Book-into is restricted to Calendars the User
// owns or can edit in that Workspace": bob is only an Editor on alice's
// Calendar, not its owner, and can still book into it.
func TestBookingLinkHandler_Create_BookIntoAcceptsEditorSharedCalendar(t *testing.T) {
	s := newBookingLinkHandlerTestServer(t)
	aliceToken, aliceID, workspaceID := s.register(t, "alice")
	bobToken, bobID, _ := s.register(t, "bob")
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}

	aliceCalendar := s.createCalendarForLink(t, aliceToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Shared calendar")
	if _, _, err := s.graph.Calendars.Share(context.Background(), aliceID, aliceCalendar, "bob@example.com", "editor"); err != nil {
		t.Fatalf("share calendar with bob: %v", err)
	}
	bobScheduleID := s.createScheduleForLink(t, bobToken, "Default")

	link := s.createLink(t, bobToken, workspaceID, defaultLinkRequest(bobScheduleID, aliceCalendar))
	if link.BookIntoCalendarID != aliceCalendar {
		t.Fatalf("expected bob to book into alice's editor-shared calendar %q, got %q", aliceCalendar, link.BookIntoCalendarID)
	}
}
