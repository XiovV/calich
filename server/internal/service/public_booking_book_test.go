package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/config"
	"github.com/XiovV/calich/server/internal/repository"
)

// setUpPublicBookingFixture is setUpBookingLinkSlotsFixture's own sibling,
// built on an SMTP-configured Graph — Book requires SMTP (ADR-0087), which
// newTestGraph's own default config doesn't carry.
func setUpPublicBookingFixture(t *testing.T, cfg config.Config) (g *Graph, hostID, workspaceID, linkID int64, calendarID, handle string) {
	t.Helper()
	ctx := context.Background()

	g = newTestGraphWithConfig(t, cfg)
	user, _, err := g.Auth.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	workspaces, err := g.Workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list workspaces: %v (%d)", err, len(workspaces))
	}
	workspaceID = workspaces[0].ID

	calendarID = "22222222-2222-2222-2222-222222222222"
	if _, err := g.Calendars.Create(ctx, user.ID, workspaceID, calendarID, CalendarWrite{Name: "Work", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	ranges := make([]repository.AvailabilityRange, 5)
	for i, weekday := range []int{1, 2, 3, 4, 5} {
		ranges[i] = repository.AvailabilityRange{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}
	}
	schedule, err := g.AvailabilitySchedules.Create(ctx, user.ID, "Default", "Etc/UTC", ranges)
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	link, err := g.BookingLinks.Create(ctx, user.ID, workspaceID, BookingLinkWrite{
		Title: "Intro call", Slug: "intro-call", DurationMinutes: 60, Visibility: "public",
		AvailabilityScheduleID: schedule.ID, BookIntoCalendarID: calendarID,
		Location: "Zoom", Description: "Let's chat", MinimumNoticeMinutes: 0, BookingHorizonDays: 30,
	})
	if err != nil {
		t.Fatalf("create booking link: %v", err)
	}

	suggestion, err := g.Auth.SuggestHandle(ctx, user.ID)
	if err != nil {
		t.Fatalf("suggest handle: %v", err)
	}
	if _, err := g.Auth.UpdateHandle(ctx, user.ID, suggestion); err != nil {
		t.Fatalf("claim handle: %v", err)
	}
	updatedUser, err := g.Auth.GetUser(ctx, user.ID)
	if err != nil || updatedUser.Handle == nil {
		t.Fatalf("expected claimed handle: %v", err)
	}

	return g, user.ID, workspaceID, link.ID, calendarID, *updatedUser.Handle
}

// firstBookableSlot picks a slot at least a full week out, so a test's own
// clock skew or slow CI run can never race "now" past it.
func firstBookableSlot(t *testing.T, g *Graph, hostID, workspaceID, linkID int64) time.Time {
	t.Helper()
	ctx := context.Background()

	now := time.Now().UTC()
	monday := nextWeekdayUTC(now.AddDate(0, 0, 7), time.Monday)
	slots, err := g.BookingLinks.DeriveSlotsForMonth(ctx, hostID, workspaceID, linkID, monday.Year(), monday.Month(), now)
	if err != nil {
		t.Fatalf("derive slots: %v", err)
	}
	if len(slots) == 0 {
		t.Fatalf("expected at least one bookable slot")
	}
	return slots[0]
}

func TestPublicBookingService_Book_CreatesBusyEventWithAttendee(t *testing.T) {
	g, hostID, workspaceID, linkID, calendarID, handle := setUpPublicBookingFixture(t, apptest.SMTPConfig(t))
	ctx := context.Background()
	link, err := g.BookingLinks.ListForUser(ctx, hostID, workspaceID)
	if err != nil || len(link) != 1 {
		t.Fatalf("list booking links: %v", err)
	}
	slot := firstBookableSlot(t, g, hostID, workspaceID, linkID)

	event, err := g.PublicBookings.Book(ctx, handle, link[0].Slug, BookingRequest{
		Start: slot, VisitorName: "Bob Visitor", VisitorEmail: "bob@example.com",
	}, time.Now())
	if err != nil {
		t.Fatalf("book: %v", err)
	}

	if event.CalendarID != calendarID {
		t.Fatalf("expected the event on the book-into calendar %q, got %q", calendarID, event.CalendarID)
	}
	if !event.Busy {
		t.Fatalf("expected a Busy event")
	}
	if event.Title != "Intro call — Bob Visitor" {
		t.Fatalf("expected title %q, got %q", "Intro call — Bob Visitor", event.Title)
	}
	if event.Location != "Zoom" || event.Description != "Let's chat" {
		t.Fatalf("expected the link's Location/Description copied, got %q / %q", event.Location, event.Description)
	}
	if event.Tzid == nil || *event.Tzid != "Etc/UTC" {
		t.Fatalf("expected the Schedule's own zone as Anchor zone, got %v", event.Tzid)
	}
	if event.CreatedBy == nil || *event.CreatedBy != hostID {
		t.Fatalf("expected the host as Organizer, got %v", event.CreatedBy)
	}
	if !event.Start.Equal(slot) || !event.End.Equal(slot.Add(time.Hour)) {
		t.Fatalf("expected the event to span the requested slot, got %v-%v", event.Start, event.End)
	}

	attendees, err := g.Events.ListAttendees(ctx, hostID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].Email != "bob@example.com" || attendees[0].UserID != nil {
		t.Fatalf("expected the visitor as a single email-shaped attendee, got %+v", attendees)
	}

	messages, err := g.OutboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending outbox messages: %v", err)
	}
	found := false
	for _, m := range messages {
		if m.EventID == event.ID && m.RecipientEmail != nil && *m.RecipientEmail == "bob@example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the visitor's Invitation queued in the outbox, got %+v", messages)
	}
}

func TestPublicBookingService_Book_RefusesWhenSlotAlreadyTaken(t *testing.T) {
	g, hostID, workspaceID, linkID, _, handle := setUpPublicBookingFixture(t, apptest.SMTPConfig(t))
	ctx := context.Background()
	links, err := g.BookingLinks.ListForUser(ctx, hostID, workspaceID)
	if err != nil || len(links) != 1 {
		t.Fatalf("list booking links: %v", err)
	}
	slot := firstBookableSlot(t, g, hostID, workspaceID, linkID)

	if _, err := g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: slot, VisitorName: "First Visitor", VisitorEmail: "first@example.com",
	}, time.Now()); err != nil {
		t.Fatalf("first booking: %v", err)
	}

	_, err = g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: slot, VisitorName: "Second Visitor", VisitorEmail: "second@example.com",
	}, time.Now())
	if !errors.Is(err, ErrSlotTaken) {
		t.Fatalf("expected ErrSlotTaken for a slot already booked, got %v", err)
	}

	events, err := g.Events.List(ctx, hostID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	count := 0
	for _, e := range events {
		if e.Start.Equal(slot) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one Event for the raced slot, got %d", count)
	}
}

func TestPublicBookingService_Book_RefusesUnavailableSlot(t *testing.T) {
	g, hostID, workspaceID, _, _, handle := setUpPublicBookingFixture(t, apptest.SMTPConfig(t))
	ctx := context.Background()
	links, err := g.BookingLinks.ListForUser(ctx, hostID, workspaceID)
	if err != nil || len(links) != 1 {
		t.Fatalf("list booking links: %v", err)
	}

	// A Sunday at midnight UTC is outside the Mon-Fri 09:00-17:00 Schedule,
	// so it was never a real slot to begin with.
	sunday := nextWeekdayUTC(time.Now().UTC().AddDate(0, 0, 7), time.Sunday)

	_, err = g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: sunday, VisitorName: "Bob", VisitorEmail: "bob@example.com",
	}, time.Now())
	if !errors.Is(err, ErrSlotTaken) {
		t.Fatalf("expected ErrSlotTaken for a slot outside the Schedule, got %v", err)
	}
}

func TestPublicBookingService_Book_RefusesWhenNoSMTPConfigured(t *testing.T) {
	g, hostID, workspaceID, linkID, _, handle := setUpPublicBookingFixture(t, apptest.Config(t))
	ctx := context.Background()
	links, err := g.BookingLinks.ListForUser(ctx, hostID, workspaceID)
	if err != nil || len(links) != 1 {
		t.Fatalf("list booking links: %v", err)
	}
	slot := firstBookableSlot(t, g, hostID, workspaceID, linkID)

	_, err = g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: slot, VisitorName: "Bob", VisitorEmail: "bob@example.com",
	}, time.Now())
	if !errors.Is(err, ErrBookingLinkPaused) {
		t.Fatalf("expected ErrBookingLinkPaused with no SMTP configured, got %v", err)
	}
}

func TestPublicBookingService_Book_RequiresNameAndEmail(t *testing.T) {
	g, hostID, workspaceID, linkID, _, handle := setUpPublicBookingFixture(t, apptest.SMTPConfig(t))
	ctx := context.Background()
	links, err := g.BookingLinks.ListForUser(ctx, hostID, workspaceID)
	if err != nil || len(links) != 1 {
		t.Fatalf("list booking links: %v", err)
	}
	slot := firstBookableSlot(t, g, hostID, workspaceID, linkID)

	if _, err := g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: slot, VisitorName: "  ", VisitorEmail: "bob@example.com",
	}, time.Now()); !errors.Is(err, ErrInvalidVisitorName) {
		t.Fatalf("expected ErrInvalidVisitorName for a blank name, got %v", err)
	}

	if _, err := g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: slot, VisitorName: "Bob", VisitorEmail: "not-an-email",
	}, time.Now()); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail for a malformed email, got %v", err)
	}
}

// TestPublicBookingService_Book_LinkedCalendarQueuesWriteBack covers
// ADR-0087's "A Linked Calendar is a legal Book-into target ... the visitor
// is confirmed before the push lands": booking onto a writable Connection
// Source must still create the Event and its email-shaped Attendee (local
// only — never mirrored to the Provider), and queue an ordinary Write-back
// push for the Event's own fields, exactly as an authenticated Create would.
// This exercises EventWrite.AllowAttendeesOnConnectionSource, the one seam
// that lets a Booking Link's Attendee coexist with Create's usual "no
// Attendees on a Connection Source" refusal (ADR-0052).
func TestPublicBookingService_Book_LinkedCalendarQueuesWriteBack(t *testing.T) {
	g := newTestGraphWithConfig(t, apptest.SMTPConfig(t))
	ctx := context.Background()
	userID, calendarID := newTestLinkedCalendar(t, g, repository.SourceModeWritable)

	workspaces, err := g.Workspaces.ListForUser(ctx, userID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list workspaces: %v (%d)", err, len(workspaces))
	}
	workspaceID := workspaces[0].ID

	ranges := make([]repository.AvailabilityRange, 5)
	for i, weekday := range []int{1, 2, 3, 4, 5} {
		ranges[i] = repository.AvailabilityRange{Weekday: weekday, StartMinute: 9 * 60, EndMinute: 17 * 60}
	}
	schedule, err := g.AvailabilitySchedules.Create(ctx, userID, "Default", "Etc/UTC", ranges)
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	link, err := g.BookingLinks.Create(ctx, userID, workspaceID, BookingLinkWrite{
		Title: "Intro call", Slug: "intro-call", DurationMinutes: 60, Visibility: "public",
		AvailabilityScheduleID: schedule.ID, BookIntoCalendarID: calendarID,
		MinimumNoticeMinutes: 0, BookingHorizonDays: 30,
	})
	if err != nil {
		t.Fatalf("create booking link: %v", err)
	}

	suggestion, err := g.Auth.SuggestHandle(ctx, userID)
	if err != nil {
		t.Fatalf("suggest handle: %v", err)
	}
	if _, err := g.Auth.UpdateHandle(ctx, userID, suggestion); err != nil {
		t.Fatalf("claim handle: %v", err)
	}
	user, err := g.Auth.GetUser(ctx, userID)
	if err != nil || user.Handle == nil {
		t.Fatalf("expected claimed handle: %v", err)
	}

	slot := firstBookableSlot(t, g, userID, workspaceID, link.ID)

	event, err := g.PublicBookings.Book(ctx, *user.Handle, link.Slug, BookingRequest{
		Start: slot, VisitorName: "Bob Visitor", VisitorEmail: "bob@example.com",
	}, time.Now())
	if err != nil {
		t.Fatalf("expected booking onto a writable Linked Calendar to succeed, got: %v", err)
	}

	attendees, err := g.Events.ListAttendees(ctx, userID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 1 || attendees[0].Email != "bob@example.com" {
		t.Fatalf("expected the visitor as a local-only attendee, got %+v", attendees)
	}

	messages, err := g.OutboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending outbox messages: %v", err)
	}
	foundWriteBack := false
	for _, m := range messages {
		if m.EventID == event.ID && m.Kind == repository.OutboxKindWriteBack {
			foundWriteBack = true
		}
	}
	if !foundWriteBack {
		t.Fatalf("expected a queued Write-back push for the booking event, got %+v", messages)
	}
}

func TestPublicBookingService_Book_RefusesWhenBookIntoCalendarAccessLost(t *testing.T) {
	g, hostID, workspaceID, linkID, _, handle := setUpPublicBookingFixture(t, apptest.SMTPConfig(t))
	ctx := context.Background()
	links, err := g.BookingLinks.ListForUser(ctx, hostID, workspaceID)
	if err != nil || len(links) != 1 {
		t.Fatalf("list booking links: %v", err)
	}
	slot := firstBookableSlot(t, g, hostID, workspaceID, linkID)

	// Simulate the host losing Access the same way
	// TestPublicBookingHandler_Get_PausedWhenBookIntoCalendarInAnotherWorkspace
	// does: mismatch the stored link's workspace_id against its own
	// Book-into Calendar's actual Workspace.
	otherWorkspace, err := g.Workspaces.CreateForOwner(ctx, hostID, "Other")
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	if _, err := g.DB.Exec("UPDATE booking_links SET workspace_id = ? WHERE id = ?", otherWorkspace.ID, linkID); err != nil {
		t.Fatalf("mismatch booking link's workspace: %v", err)
	}

	_, err = g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: slot, VisitorName: "Bob", VisitorEmail: "visitor@example.com",
	}, time.Now())
	if !errors.Is(err, ErrBookingLinkPaused) {
		t.Fatalf("expected ErrBookingLinkPaused once the Book-into Calendar sits outside the link's Workspace, got %v", err)
	}
}
