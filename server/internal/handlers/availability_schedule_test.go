package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/service"
)

// availabilityScheduleTestServer bundles the HTTP surface Availability
// Schedules need (#320, ADR-0085): Register (to mint real Users, since a
// Schedule is private to one User with no Workspace of its own), the
// Preferences PATCH (to exercise Working hours seeding and the
// never-synchronised rule), and the /api/availability-schedules routes,
// gated the same way router.New wires them — RequireAuth/RequireEnabledUser
// alone, with no RequireWorkspace.
type availabilityScheduleTestServer struct {
	srv   *httptest.Server
	graph *service.Graph
}

func newAvailabilityScheduleTestServer(t *testing.T) *availabilityScheduleTestServer {
	t.Helper()

	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	g := newTestGraphWithConfig(t, cfg)

	auth := g.Auth
	authHandler := NewAuthHandler(auth, g.RateLimiter, false, false, false, true)
	scheduleHandler := NewAvailabilityScheduleHandler(g.AvailabilitySchedules)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)
	r.With(httpauth.RequireAuth(auth)).Patch("/api/auth/preferences", authHandler.UpdatePreferences)

	r.Route("/api/availability-schedules", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))

		r.Get("/", scheduleHandler.List)
		r.Post("/", scheduleHandler.Create)
		r.Patch("/{id}", scheduleHandler.Update)
		r.Delete("/{id}", scheduleHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &availabilityScheduleTestServer{srv: srv, graph: g}
}

func (s *availabilityScheduleTestServer) register(t *testing.T, username string) (accessToken string, userID int64) {
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

	return logged.AccessToken, user.ID
}

func (s *availabilityScheduleTestServer) setWorkingHours(t *testing.T, accessToken string, start, end int) {
	t.Helper()

	resp := s.do(t, http.MethodPatch, "/api/auth/preferences", accessToken, updatePreferencesRequest{WorkingHoursStart: &start, WorkingHoursEnd: &end})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 setting working hours, got %d", resp.StatusCode)
	}
}

func (s *availabilityScheduleTestServer) list(t *testing.T, accessToken, tz string) []availabilityScheduleResponse {
	t.Helper()

	path := "/api/availability-schedules/"
	if tz != "" {
		path += "?tz=" + tz
	}
	resp := s.do(t, http.MethodGet, path, accessToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing availability schedules, got %d", resp.StatusCode)
	}
	var list []availabilityScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return list
}

func (s *availabilityScheduleTestServer) create(t *testing.T, accessToken string, req createAvailabilityScheduleRequest) *http.Response {
	t.Helper()
	return s.do(t, http.MethodPost, "/api/availability-schedules/", accessToken, req)
}

func (s *availabilityScheduleTestServer) do(t *testing.T, method, path, accessToken string, body any) *http.Response {
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestAvailabilityScheduleHandler_List_SeedsDefaultFromFallbackWhenNoWorkingHours(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	schedules := s.list(t, token, "")
	if len(schedules) != 1 {
		t.Fatalf("expected exactly one auto-seeded schedule, got %v", schedules)
	}
	def := schedules[0]
	if def.Name != "Default" {
		t.Fatalf("expected name %q, got %q", "Default", def.Name)
	}
	if def.Tzid != "Etc/UTC" {
		t.Fatalf("expected fallback tzid %q, got %q", "Etc/UTC", def.Tzid)
	}
	if len(def.Ranges) != 5 {
		t.Fatalf("expected Mon-Fri (5 ranges), got %v", def.Ranges)
	}
	for _, rng := range def.Ranges {
		if rng.Weekday < 1 || rng.Weekday > 5 {
			t.Fatalf("expected only Mon-Fri weekdays, got weekday %d", rng.Weekday)
		}
		if rng.StartMinute != 9*60 || rng.EndMinute != 17*60 {
			t.Fatalf("expected the 09:00-17:00 fallback, got %d-%d", rng.StartMinute, rng.EndMinute)
		}
	}
}

func TestAvailabilityScheduleHandler_List_SeedsDefaultFromWorkingHoursWhenSet(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	s.setWorkingHours(t, token, 8*60, 16*60)

	schedules := s.list(t, token, "")
	if len(schedules) != 1 {
		t.Fatalf("expected exactly one auto-seeded schedule, got %v", schedules)
	}
	for _, rng := range schedules[0].Ranges {
		if rng.StartMinute != 8*60 || rng.EndMinute != 16*60 {
			t.Fatalf("expected the seeded 08:00-16:00 range copied from Working hours, got %d-%d", rng.StartMinute, rng.EndMinute)
		}
	}
}

func TestAvailabilityScheduleHandler_List_SeedsTzidFromHint(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	schedules := s.list(t, token, "Europe/Sarajevo")
	if schedules[0].Tzid != "Europe/Sarajevo" {
		t.Fatalf("expected the seeded schedule to take the browser-detected tz hint, got %q", schedules[0].Tzid)
	}
}

func TestAvailabilityScheduleHandler_List_InvalidTzHintFallsBackToUTC(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	schedules := s.list(t, token, "Not/AZone")
	if schedules[0].Tzid != "Etc/UTC" {
		t.Fatalf("expected an unresolvable tz hint to fall back to Etc/UTC, got %q", schedules[0].Tzid)
	}
}

// TestAvailabilityScheduleHandler_EditingWorkingHoursAfterwardsNeverReachesTheSchedule
// is ADR-0085's central rule: the Default Schedule is seeded once, and
// changing Working hours afterwards must never touch it, ever.
func TestAvailabilityScheduleHandler_EditingWorkingHoursAfterwardsNeverReachesTheSchedule(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	seeded := s.list(t, token, "")
	if seeded[0].Ranges[0].StartMinute != 9*60 || seeded[0].Ranges[0].EndMinute != 17*60 {
		t.Fatalf("expected the initial fallback seed, got %v", seeded[0].Ranges)
	}

	s.setWorkingHours(t, token, 6*60, 14*60)

	after := s.list(t, token, "")
	if len(after) != 1 {
		t.Fatalf("expected still exactly one schedule, got %v", after)
	}
	for _, rng := range after[0].Ranges {
		if rng.StartMinute != 9*60 || rng.EndMinute != 17*60 {
			t.Fatalf("expected the Default schedule to stay at its originally seeded 09:00-17:00 despite the Working hours change, got %d-%d", rng.StartMinute, rng.EndMinute)
		}
	}
}

func TestAvailabilityScheduleHandler_Create_SplitDayAllowsSeveralRangesOnOneWeekday(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	req := createAvailabilityScheduleRequest{
		Name: "Mornings and evenings",
		Tzid: "Europe/Berlin",
		Ranges: []availabilityRangeDTO{
			{Weekday: 1, StartMinute: 9 * 60, EndMinute: 12 * 60},
			{Weekday: 1, StartMinute: 14 * 60, EndMinute: 17 * 60},
		},
	}
	resp := s.create(t, token, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var created availabilityScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if len(created.Ranges) != 2 {
		t.Fatalf("expected both ranges on the split day to survive, got %v", created.Ranges)
	}
}

func TestAvailabilityScheduleHandler_Create_EmptyScheduleIsLegal(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	resp := s.create(t, token, createAvailabilityScheduleRequest{Name: "Unavailable", Tzid: "Etc/UTC", Ranges: nil})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating an empty schedule, got %d", resp.StatusCode)
	}
	var created availabilityScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if len(created.Ranges) != 0 {
		t.Fatalf("expected no ranges, got %v", created.Ranges)
	}
}

func TestAvailabilityScheduleHandler_Create_EmptyNameRejected(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	resp := s.create(t, token, createAvailabilityScheduleRequest{Name: "   ", Tzid: "Etc/UTC"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAvailabilityScheduleHandler_Create_InvalidTimezoneRejected(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	resp := s.create(t, token, createAvailabilityScheduleRequest{Name: "Work", Tzid: "Not/AZone"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAvailabilityScheduleHandler_Create_InvalidRangeRejected(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	cases := []availabilityRangeDTO{
		{Weekday: 7, StartMinute: 0, EndMinute: 60},
		{Weekday: -1, StartMinute: 0, EndMinute: 60},
		{Weekday: 1, StartMinute: 60, EndMinute: 60},
		{Weekday: 1, StartMinute: 120, EndMinute: 60},
		{Weekday: 1, StartMinute: 0, EndMinute: 24*60 + 1},
	}
	for _, rng := range cases {
		resp := s.create(t, token, createAvailabilityScheduleRequest{Name: "Work", Tzid: "Etc/UTC", Ranges: []availabilityRangeDTO{rng}})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for range %+v, got %d", rng, resp.StatusCode)
		}
	}
}

func TestAvailabilityScheduleHandler_Update_ReplacesNameTimezoneAndRanges(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	createResp := s.create(t, token, createAvailabilityScheduleRequest{
		Name:   "Work",
		Tzid:   "Etc/UTC",
		Ranges: []availabilityRangeDTO{{Weekday: 1, StartMinute: 9 * 60, EndMinute: 17 * 60}},
	})
	defer createResp.Body.Close()
	var created availabilityScheduleResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	updateReq := updateAvailabilityScheduleRequest{
		Name: "Weekend calls",
		Tzid: "Europe/Sarajevo",
		Ranges: []availabilityRangeDTO{
			{Weekday: 6, StartMinute: 10 * 60, EndMinute: 12 * 60},
		},
	}
	updateResp := s.do(t, http.MethodPatch, "/api/availability-schedules/"+strconv.FormatInt(created.ID, 10), token, updateReq)
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 updating, got %d", updateResp.StatusCode)
	}
	var updated availabilityScheduleResponse
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Name != "Weekend calls" || updated.Tzid != "Europe/Sarajevo" {
		t.Fatalf("expected the name and timezone to be replaced, got %+v", updated)
	}
	if len(updated.Ranges) != 1 || updated.Ranges[0].Weekday != 6 {
		t.Fatalf("expected the old range replaced wholesale by the new one, got %v", updated.Ranges)
	}
}

func TestAvailabilityScheduleHandler_SecondUserCannotReadUpdateOrDeleteFirsts(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	aliceToken, _ := s.register(t, "alice")
	bobToken, _ := s.register(t, "bob")

	createResp := s.create(t, aliceToken, createAvailabilityScheduleRequest{Name: "Work", Tzid: "Etc/UTC"})
	defer createResp.Body.Close()
	var alicesSchedule availabilityScheduleResponse
	if err := json.NewDecoder(createResp.Body).Decode(&alicesSchedule); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	idPath := "/api/availability-schedules/" + strconv.FormatInt(alicesSchedule.ID, 10)

	updateResp := s.do(t, http.MethodPatch, idPath, bobToken, updateAvailabilityScheduleRequest{Name: "Hijacked", Tzid: "Etc/UTC"})
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 updating, got %d", updateResp.StatusCode)
	}

	deleteResp := s.do(t, http.MethodDelete, idPath, bobToken, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 deleting, got %d", deleteResp.StatusCode)
	}

	// alice's own list still shows it, untouched, under its original name.
	aliceSchedules := s.list(t, aliceToken, "")
	found := false
	for _, sched := range aliceSchedules {
		if sched.ID == alicesSchedule.ID && sched.Name == "Work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected alice's schedule to survive bob's attempts untouched, got %v", aliceSchedules)
	}

	// bob's own list is independent — he never sees alice's schedule at all.
	bobSchedules := s.list(t, bobToken, "")
	for _, sched := range bobSchedules {
		if sched.ID == alicesSchedule.ID {
			t.Fatalf("expected bob's own list to never include alice's schedule, got %v", bobSchedules)
		}
	}
}

func TestAvailabilityScheduleHandler_Delete_RemovesIt(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	// Seed the Default first so deleting the one we create below doesn't
	// drop the caller back to zero schedules, which would re-seed a fresh
	// Default on the very next List and make the assertion below ambiguous.
	s.list(t, token, "")

	createResp := s.create(t, token, createAvailabilityScheduleRequest{Name: "Temp", Tzid: "Etc/UTC"})
	defer createResp.Body.Close()
	var created availabilityScheduleResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/availability-schedules/"+strconv.FormatInt(created.ID, 10), token, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 deleting, got %d", deleteResp.StatusCode)
	}

	remaining := s.list(t, token, "")
	if len(remaining) != 1 || remaining[0].Name != "Default" {
		t.Fatalf("expected only the Default schedule to remain, got %v", remaining)
	}
}

// TestAvailabilityScheduleHandler_RangeSurvivesADSTTransitionAtItsStatedWallClockHours
// covers ADR-0085's "a range spanning a DST transition yields the wall-clock
// hours stated": a Schedule stores a bare weekday + start/end minute pair
// and a tzid, never a frozen UTC offset, so reconstructing the same
// wall-clock range against two dates either side of a DST changeover (US
// spring-forward, 2026-03-08) must read back the same local hours even
// though the UTC instant underneath shifts by an hour.
func TestAvailabilityScheduleHandler_RangeSurvivesADSTTransitionAtItsStatedWallClockHours(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, _ := s.register(t, "alice")

	resp := s.create(t, token, createAvailabilityScheduleRequest{
		Name: "US hours",
		Tzid: "America/New_York",
		Ranges: []availabilityRangeDTO{
			{Weekday: 1, StartMinute: 9 * 60, EndMinute: 17 * 60},
		},
	})
	defer resp.Body.Close()
	var created availabilityScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	loc, err := time.LoadLocation(created.Tzid)
	if err != nil {
		t.Fatalf("load location %q: %v", created.Tzid, err)
	}
	rng := created.Ranges[0]
	startHour, startMin := rng.StartMinute/60, rng.StartMinute%60
	endHour, endMin := rng.EndMinute/60, rng.EndMinute%60

	// 2026-03-02 is the Monday before the US DST transition (2026-03-08);
	// 2026-03-09 is the Monday after.
	before := time.Date(2026, 3, 2, startHour, startMin, 0, 0, loc)
	beforeEnd := time.Date(2026, 3, 2, endHour, endMin, 0, 0, loc)
	after := time.Date(2026, 3, 9, startHour, startMin, 0, 0, loc)
	afterEnd := time.Date(2026, 3, 9, endHour, endMin, 0, 0, loc)

	if before.Hour() != 9 || after.Hour() != 9 {
		t.Fatalf("expected the stated 09:00 wall-clock hour on both sides of the DST transition, got %d and %d", before.Hour(), after.Hour())
	}
	if beforeEnd.Hour() != 17 || afterEnd.Hour() != 17 {
		t.Fatalf("expected the stated 17:00 wall-clock hour on both sides of the DST transition, got %d and %d", beforeEnd.Hour(), afterEnd.Hour())
	}

	_, beforeOffset := before.Zone()
	_, afterOffset := after.Zone()
	if beforeOffset == afterOffset {
		t.Fatalf("expected the UTC offset to actually differ across the DST transition, got %d both sides", beforeOffset)
	}
}

// TestAvailabilityScheduleHandler_CascadesOnUserDeletion covers
// availability_schedules.user_id's ON DELETE CASCADE, which also takes
// availability_schedule_ranges with it via schedule_id's own cascade.
func TestAvailabilityScheduleHandler_CascadesOnUserDeletion(t *testing.T) {
	s := newAvailabilityScheduleTestServer(t)
	token, userID := s.register(t, "alice")

	created := s.list(t, token, "")
	if len(created) != 1 || len(created[0].Ranges) == 0 {
		t.Fatalf("expected the auto-seeded Default schedule with ranges, got %v", created)
	}

	impact, err := s.graph.Accounts.DeleteImpact(context.Background(), userID)
	if err != nil {
		t.Fatalf("delete impact: %v", err)
	}
	dispositions := make([]service.CalendarDisposition, len(impact.Calendars))
	for i, c := range impact.Calendars {
		dispositions[i] = service.CalendarDisposition{CalendarID: c.ID, Disposition: service.DispositionDelete}
	}
	if err := s.graph.Accounts.Delete(context.Background(), userID, dispositions); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var scheduleCount int
	if err := s.graph.DB.QueryRow("SELECT COUNT(*) FROM availability_schedules WHERE user_id = ?", userID).Scan(&scheduleCount); err != nil {
		t.Fatalf("count availability schedules: %v", err)
	}
	if scheduleCount != 0 {
		t.Fatalf("expected deleting the user to cascade every availability schedule they owned, got %d remaining", scheduleCount)
	}

	var rangeCount int
	if err := s.graph.DB.QueryRow("SELECT COUNT(*) FROM availability_schedule_ranges WHERE schedule_id = ?", created[0].ID).Scan(&rangeCount); err != nil {
		t.Fatalf("count availability schedule ranges: %v", err)
	}
	if rangeCount != 0 {
		t.Fatalf("expected the schedule's own ranges to cascade too, got %d remaining", rangeCount)
	}
}
