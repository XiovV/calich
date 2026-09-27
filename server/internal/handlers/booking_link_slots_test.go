package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/service"
)

// slots GETs accessToken's derived slots for linkID within (year, month),
// failing the test on anything but 200.
func (s *bookingLinkHandlerTestServer) slots(t *testing.T, accessToken string, workspaceID, linkID int64, year, month int) []time.Time {
	t.Helper()

	path := "/api/booking-links/" + strconv.FormatInt(linkID, 10) + "/slots?year=" + strconv.Itoa(year) + "&month=" + strconv.Itoa(month)
	resp := s.do(t, http.MethodGet, path, accessToken, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 fetching slots, got %d", resp.StatusCode)
	}
	var body slotsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode slots response: %v", err)
	}
	return body.Slots
}

func containsSlot(slots []time.Time, at time.Time) bool {
	for _, s := range slots {
		if s.Equal(at) {
			return true
		}
	}
	return false
}

// createMonFriSchedule creates a Schedule named name with an explicit
// Mon-Fri 09:00-17:00 weekly pattern in Etc/UTC — createScheduleForLink
// (booking_link_test.go) creates one with no Ranges at all, which the
// slots tests below need to actually have some.
func (s *bookingLinkHandlerTestServer) createMonFriSchedule(t *testing.T, accessToken, name string) int64 {
	t.Helper()

	ranges := make([]availabilityRangeDTO, 5)
	for i, weekday := range []int{1, 2, 3, 4, 5} {
		ranges[i] = availabilityRangeDTO{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}
	}

	resp := s.doNoWorkspace(t, http.MethodPost, "/api/availability-schedules/", accessToken, createAvailabilityScheduleRequest{Name: name, Tzid: "Etc/UTC", Ranges: ranges})
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

// setUpSlotsFixture registers alice, gives her a book-into Calendar, a
// Mon-Fri 09:00-17:00 Schedule in Etc/UTC, and a Booking Link with a
// 60-minute Duration, no minimum notice and a generous horizon — every
// slots test below builds on this shared shape.
func setUpSlotsFixture(t *testing.T) (s *bookingLinkHandlerTestServer, token string, userID, workspaceID, linkID int64, calendarID string) {
	t.Helper()

	s = newBookingLinkHandlerTestServer(t)
	token, userID, workspaceID = s.register(t, "alice")
	calendarID = s.createCalendarForLink(t, token, workspaceID, "11111111-1111-1111-1111-111111111111", "Work")
	scheduleID := s.createMonFriSchedule(t, token, "Default")

	req := defaultLinkRequest(scheduleID, calendarID)
	req.MinimumNoticeMinutes = 0
	req.BookingHorizonDays = 90
	link := s.createLink(t, token, workspaceID, req)

	return s, token, userID, workspaceID, link.ID, calendarID
}

// TestBookingLinkHandler_Slots_BusyClosesSlotFreeDoesNot covers the AC
// directly: "Busy Occurrences in the Conflict set's Calendars close slots;
// Free Events do not".
func TestBookingLinkHandler_Slots_BusyClosesSlotFreeDoesNot(t *testing.T) {
	s, token, userID, workspaceID, linkID, calendarID := setUpSlotsFixture(t)
	ctx := context.Background()

	// A Monday deep enough into the horizon that "now" (the test's own
	// wall-clock moment) can never race past it.
	monday := nextWeekday(time.Now().UTC().AddDate(0, 0, 14), time.Monday)

	if _, err := s.graph.Events.Create(ctx, userID, "evt-busy", service.EventWrite{
		CalendarID: calendarID, Title: "Busy block", Busy: true,
		Start: monday.Add(10 * time.Hour), End: monday.Add(11 * time.Hour),
	}); err != nil {
		t.Fatalf("create busy event: %v", err)
	}
	if _, err := s.graph.Events.Create(ctx, userID, "evt-free", service.EventWrite{
		CalendarID: calendarID, Title: "Free block", Busy: false,
		Start: monday.Add(13 * time.Hour), End: monday.Add(14 * time.Hour),
	}); err != nil {
		t.Fatalf("create free event: %v", err)
	}

	slots := s.slots(t, token, workspaceID, linkID, monday.Year(), int(monday.Month()))
	if containsSlot(slots, monday.Add(10*time.Hour)) {
		t.Fatalf("expected the Busy event to close its 10:00 slot, got %v", slots)
	}
	if !containsSlot(slots, monday.Add(13*time.Hour)) {
		t.Fatalf("expected the Free event to leave its 13:00 slot open, got %v", slots)
	}
}

// TestBookingLinkHandler_Slots_AllDayClosesOnlyWhenExplicitlyBusy covers
// the AC directly.
func TestBookingLinkHandler_Slots_AllDayClosesOnlyWhenExplicitlyBusy(t *testing.T) {
	s, token, userID, workspaceID, linkID, calendarID := setUpSlotsFixture(t)
	ctx := context.Background()

	monday := nextWeekday(time.Now().UTC().AddDate(0, 0, 14), time.Monday)
	dayStart := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)

	if _, err := s.graph.Events.Create(ctx, userID, "evt-allday-free", service.EventWrite{
		CalendarID: calendarID, Title: "Holiday", AllDay: true, Busy: false,
		Start: dayStart, End: dayStart.AddDate(0, 0, 1),
	}); err != nil {
		t.Fatalf("create free all-day event: %v", err)
	}

	freeSlots := s.slots(t, token, workspaceID, linkID, monday.Year(), int(monday.Month()))
	if !containsSlot(freeSlots, monday.Add(10*time.Hour)) {
		t.Fatalf("expected a Free all-day event to leave every slot that day open, got %v", freeSlots)
	}

	if _, err := s.graph.Events.Create(ctx, userID, "evt-allday-busy", service.EventWrite{
		CalendarID: calendarID, Title: "Offsite", AllDay: true, Busy: true,
		Start: dayStart, End: dayStart.AddDate(0, 0, 1),
	}); err != nil {
		t.Fatalf("create busy all-day event: %v", err)
	}

	busySlots := s.slots(t, token, workspaceID, linkID, monday.Year(), int(monday.Month()))
	if containsSlot(busySlots, monday.Add(10*time.Hour)) {
		t.Fatalf("expected an explicitly Busy all-day event to close every slot that day, got %v", busySlots)
	}
}

// TestBookingLinkHandler_Slots_RecurringBusyEventClosesEachOccurrence
// covers the recurrence-expansion half of "Busy Occurrences ... close
// slots" — a weekly recurring Event must close its slot on every Monday it
// falls in the requested month, not just its own first Occurrence.
func TestBookingLinkHandler_Slots_RecurringBusyEventClosesEachOccurrence(t *testing.T) {
	s, token, userID, workspaceID, linkID, calendarID := setUpSlotsFixture(t)
	ctx := context.Background()

	firstMonday := nextWeekday(time.Now().UTC().AddDate(0, 0, 14), time.Monday)
	secondMonday := firstMonday.AddDate(0, 0, 7)
	// Stay inside one calendar month so both Occurrences are checked
	// against the same slots response.
	if firstMonday.Month() != secondMonday.Month() {
		firstMonday = firstMonday.AddDate(0, 0, 7)
		secondMonday = firstMonday.AddDate(0, 0, 7)
	}

	if _, err := s.graph.Events.Create(ctx, userID, "evt-recurring-busy", service.EventWrite{
		CalendarID: calendarID, Title: "Standing meeting", Busy: true,
		Start: firstMonday.Add(10 * time.Hour), End: firstMonday.Add(11 * time.Hour),
		Rrule: "FREQ=WEEKLY", Tzid: strPtr("Etc/UTC"),
	}); err != nil {
		t.Fatalf("create recurring busy event: %v", err)
	}

	slots := s.slots(t, token, workspaceID, linkID, firstMonday.Year(), int(firstMonday.Month()))
	if containsSlot(slots, firstMonday.Add(10*time.Hour)) {
		t.Fatalf("expected the first Occurrence's 10:00 slot closed, got %v", slots)
	}
	if containsSlot(slots, secondMonday.Add(10*time.Hour)) {
		t.Fatalf("expected the second Occurrence's 10:00 slot closed too, got %v", slots)
	}
}

// TestBookingLinkHandler_Slots_OverrideReplacesItsOwnOccurrencesBusyness
// covers ADR-0086's "set on a Master and independently on an Override, so
// one Occurrence of a series may differ from the rest without becoming an
// Exception" — a recurring Busy series with one Occurrence overridden to
// Free must leave that Occurrence's own slot open while every other
// Occurrence stays closed.
func TestBookingLinkHandler_Slots_OverrideReplacesItsOwnOccurrencesBusyness(t *testing.T) {
	s, token, userID, workspaceID, linkID, calendarID := setUpSlotsFixture(t)
	ctx := context.Background()

	firstMonday := nextWeekday(time.Now().UTC().AddDate(0, 0, 14), time.Monday)
	secondMonday := firstMonday.AddDate(0, 0, 7)
	if firstMonday.Month() != secondMonday.Month() {
		firstMonday = firstMonday.AddDate(0, 0, 7)
		secondMonday = firstMonday.AddDate(0, 0, 7)
	}
	recurrenceID := secondMonday.Add(10 * time.Hour)

	master, err := s.graph.Events.Create(ctx, userID, "evt-series-master", service.EventWrite{
		CalendarID: calendarID, Title: "Standing meeting", Busy: true,
		Start: firstMonday.Add(10 * time.Hour), End: firstMonday.Add(11 * time.Hour),
		Rrule: "FREQ=WEEKLY", Tzid: strPtr("Etc/UTC"),
	})
	if err != nil {
		t.Fatalf("create series master: %v", err)
	}
	if _, err := s.graph.Events.Create(ctx, userID, "evt-series-override", service.EventWrite{
		CalendarID: calendarID, Title: "Standing meeting (moved to free)", Busy: false,
		Start: recurrenceID, End: recurrenceID.Add(time.Hour),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	slots := s.slots(t, token, workspaceID, linkID, firstMonday.Year(), int(firstMonday.Month()))
	if containsSlot(slots, firstMonday.Add(10*time.Hour)) {
		t.Fatalf("expected the un-overridden first Occurrence's slot to stay closed, got %v", slots)
	}
	if !containsSlot(slots, secondMonday.Add(10*time.Hour)) {
		t.Fatalf("expected the Free Override's own Occurrence slot to open up, got %v", slots)
	}
}

// TestBookingLinkHandler_Slots_TasksIncludedWhenFlagSetExcludedOtherwise
// covers the AC directly: an incomplete Task's Time block closes a slot
// only when the Tasks row is set.
func TestBookingLinkHandler_Slots_TasksIncludedWhenFlagSetExcludedOtherwise(t *testing.T) {
	s, token, userID, workspaceID, linkID, calendarID := setUpSlotsFixture(t)
	ctx := context.Background()

	monday := nextWeekday(time.Now().UTC().AddDate(0, 0, 14), time.Monday)

	taskLists, err := s.graph.TaskLists.ListForUser(ctx, userID, workspaceID)
	if err != nil || len(taskLists) == 0 {
		t.Fatalf("list task lists: %v (%d)", err, len(taskLists))
	}
	task, err := s.graph.Tasks.Create(ctx, userID, workspaceID, taskLists[0].ID, "Write the report")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := s.graph.Tasks.SetTimeBlock(ctx, userID, workspaceID, task.ID, monday.Add(10*time.Hour), 60); err != nil {
		t.Fatalf("set time block: %v", err)
	}

	withTasks := s.slots(t, token, workspaceID, linkID, monday.Year(), int(monday.Month()))
	if containsSlot(withTasks, monday.Add(10*time.Hour)) {
		t.Fatalf("expected the incomplete Task's Time block to close its slot, got %v", withTasks)
	}

	// Flip the Tasks row off.
	scheduleID := getScheduleIDFromLink(t, s, token, workspaceID, linkID)
	updateReq := defaultLinkRequest(scheduleID, calendarID)
	updateReq.MinimumNoticeMinutes = 0
	updateReq.BookingHorizonDays = 90
	updateReq.TasksInConflictSet = false
	resp := s.do(t, http.MethodPatch, "/api/booking-links/"+strconv.FormatInt(linkID, 10), token, workspaceID, updateReq)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 updating, got %d", resp.StatusCode)
	}

	withoutTasks := s.slots(t, token, workspaceID, linkID, monday.Year(), int(monday.Month()))
	if !containsSlot(withoutTasks, monday.Add(10*time.Hour)) {
		t.Fatalf("expected the same slot open once the Tasks row is cleared, got %v", withoutTasks)
	}

	// A completed Task's Time block never closes a slot, even with the
	// Tasks row set again.
	reEnableReq := defaultLinkRequest(scheduleID, calendarID)
	reEnableReq.MinimumNoticeMinutes = 0
	reEnableReq.BookingHorizonDays = 90
	reEnableReq.TasksInConflictSet = true
	reEnableResp := s.do(t, http.MethodPatch, "/api/booking-links/"+strconv.FormatInt(linkID, 10), token, workspaceID, reEnableReq)
	defer reEnableResp.Body.Close()
	if reEnableResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 re-enabling Tasks, got %d", reEnableResp.StatusCode)
	}
	if _, err := s.graph.Tasks.Complete(ctx, userID, workspaceID, task.ID); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	afterCompletion := s.slots(t, token, workspaceID, linkID, monday.Year(), int(monday.Month()))
	if !containsSlot(afterCompletion, monday.Add(10*time.Hour)) {
		t.Fatalf("expected a completed Task to never close a slot, got %v", afterCompletion)
	}
}

func TestBookingLinkHandler_Slots_InvalidMonthRejected(t *testing.T) {
	s, token, _, workspaceID, linkID, _ := setUpSlotsFixture(t)

	resp := s.do(t, http.MethodGet, "/api/booking-links/"+strconv.FormatInt(linkID, 10)+"/slots?year=2026&month=13", token, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBookingLinkHandler_Slots_SecondUserCannotReadFirsts(t *testing.T) {
	s, aliceToken, _, workspaceID, linkID, _ := setUpSlotsFixture(t)
	bobToken, bobID, _ := s.register(t, "bob")
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}
	_ = aliceToken

	now := time.Now()
	resp := s.do(t, http.MethodGet, "/api/booking-links/"+strconv.FormatInt(linkID, 10)+"/slots?year="+strconv.Itoa(now.Year())+"&month="+strconv.Itoa(int(now.Month())), bobToken, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// nextWeekday returns the first date on or after from whose weekday is
// weekday.
func nextWeekday(from time.Time, weekday time.Weekday) time.Time {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	for from.Weekday() != weekday {
		from = from.AddDate(0, 0, 1)
	}
	return from
}

// getScheduleIDFromLink re-reads linkID's own AvailabilityScheduleID via
// List, so a test updating other fields doesn't have to thread the
// schedule id through separately.
func getScheduleIDFromLink(t *testing.T, s *bookingLinkHandlerTestServer, accessToken string, workspaceID, linkID int64) int64 {
	t.Helper()
	links := s.listLinks(t, accessToken, workspaceID)
	for _, l := range links {
		if l.ID == linkID {
			return l.AvailabilityScheduleID
		}
	}
	t.Fatalf("booking link %d not found", linkID)
	return 0
}
