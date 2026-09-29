package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/repository"
)

// bookForCancelTest books firstBookableSlot and returns the created Event,
// the fixture's own graph/host/workspace, and the visitor's email — the
// shared setup every Cancel test starts from.
func bookForCancelTest(t *testing.T) (g *Graph, hostID, workspaceID int64, event repository.Event) {
	t.Helper()
	ctx := context.Background()

	g, hostID, workspaceID, linkID, _, handle := setUpPublicBookingFixture(t, apptest.SMTPConfig(t))
	links, err := g.BookingLinks.ListForUser(ctx, hostID, workspaceID)
	if err != nil || len(links) != 1 {
		t.Fatalf("list booking links: %v", err)
	}
	slot := firstBookableSlot(t, g, hostID, workspaceID, linkID)

	event, err = g.PublicBookings.Book(ctx, handle, links[0].Slug, BookingRequest{
		Start: slot, VisitorName: "Bob Visitor", VisitorEmail: "bob@example.com",
	}, time.Now(), "https://cal.example.com")
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	return g, hostID, workspaceID, event
}

func TestPublicBookingService_Book_QueuesConfirmationWithCancelLink(t *testing.T) {
	g, _, _, event := bookForCancelTest(t)
	ctx := context.Background()

	messages, err := g.OutboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending outbox messages: %v", err)
	}

	var notice *repository.OutboxMessage
	for i, m := range messages {
		if m.EventID == event.ID && m.Method == repository.OutboxMethodBookingNotice {
			notice = &messages[i]
		}
	}
	if notice == nil {
		t.Fatalf("expected a queued BOOKING_NOTICE confirmation, got %+v", messages)
	}
	if notice.RecipientEmail == nil || *notice.RecipientEmail != "bob@example.com" {
		t.Fatalf("expected the confirmation addressed to the visitor, got %+v", notice.RecipientEmail)
	}
	if notice.BookingNotice == nil {
		t.Fatalf("expected a BookingNotice snapshot")
	}
	if !strings.Contains(notice.BookingNotice.Body, "https://cal.example.com/cancel-booking?token=") {
		t.Fatalf("expected the confirmation body to carry a signed cancel link, got %q", notice.BookingNotice.Body)
	}
}

func TestPublicBookingService_Cancel_DeletesEventAndFreesSlot(t *testing.T) {
	g, hostID, _, event := bookForCancelTest(t)
	ctx := context.Background()

	token, err := g.Auth.IssueBookingCancelToken(event.ID)
	if err != nil {
		t.Fatalf("issue cancel token: %v", err)
	}

	if err := g.PublicBookings.Cancel(ctx, token, time.Now()); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := g.Events.Get(ctx, hostID, event.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the cancelled event to be gone, got %v", err)
	}
}

func TestPublicBookingService_Cancel_EmitsAttendeeCancelAndHostNotice(t *testing.T) {
	g, _, _, event := bookForCancelTest(t)
	ctx := context.Background()

	token, err := g.Auth.IssueBookingCancelToken(event.ID)
	if err != nil {
		t.Fatalf("issue cancel token: %v", err)
	}
	if err := g.PublicBookings.Cancel(ctx, token, time.Now()); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	messages, err := g.OutboxRepo.ListPending(ctx, 20)
	if err != nil {
		t.Fatalf("list pending outbox messages: %v", err)
	}

	foundAttendeeCancel := false
	foundHostNotice := false
	for _, m := range messages {
		if m.EventID != event.ID {
			continue
		}
		if m.Method == repository.OutboxMethodCancel && m.RecipientEmail != nil && *m.RecipientEmail == "bob@example.com" {
			foundAttendeeCancel = true
		}
		if m.Method == repository.OutboxMethodBookingNotice && m.RecipientEmail != nil && *m.RecipientEmail == "admin@example.com" {
			foundHostNotice = true
		}
	}
	if !foundAttendeeCancel {
		t.Fatalf("expected a METHOD:CANCEL queued to the visitor, got %+v", messages)
	}
	if !foundHostNotice {
		t.Fatalf("expected a cancellation notice queued to the host, got %+v", messages)
	}
}

func TestPublicBookingService_Cancel_RefusesBadSignature(t *testing.T) {
	g, _, _, event := bookForCancelTest(t)
	ctx := context.Background()

	err := g.PublicBookings.Cancel(ctx, "not-a-real-token", time.Now())
	if !errors.Is(err, ErrInvalidCancelToken) {
		t.Fatalf("expected ErrInvalidCancelToken for a garbage token, got %v", err)
	}

	// A token signed for a different purpose (or under a different key)
	// must be refused the same way — only a real cancel token names an
	// Event this method is willing to delete.
	otherToken, err := g.Auth.IssueConnectState(1)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	if err := g.PublicBookings.Cancel(ctx, otherToken, time.Now()); !errors.Is(err, ErrInvalidCancelToken) {
		t.Fatalf("expected ErrInvalidCancelToken for a token signed for a different purpose, got %v", err)
	}

	// Sanity: the real token for this fixture's own event still works.
	token, err := g.Auth.IssueBookingCancelToken(event.ID)
	if err != nil {
		t.Fatalf("issue cancel token: %v", err)
	}
	if err := g.PublicBookings.Cancel(ctx, token, time.Now()); err != nil {
		t.Fatalf("expected the real token to cancel cleanly, got %v", err)
	}
}

func TestPublicBookingService_Cancel_RefusesAfterEventStarted(t *testing.T) {
	g, hostID, _, event := bookForCancelTest(t)
	ctx := context.Background()

	token, err := g.Auth.IssueBookingCancelToken(event.ID)
	if err != nil {
		t.Fatalf("issue cancel token: %v", err)
	}

	// now is after the booked slot's own start — as if the visitor waited
	// past the meeting before following the link.
	after := event.Start.Add(time.Hour)
	if err := g.PublicBookings.Cancel(ctx, token, after); !errors.Is(err, ErrBookingAlreadyStarted) {
		t.Fatalf("expected ErrBookingAlreadyStarted, got %v", err)
	}

	if _, err := g.Events.Get(ctx, hostID, event.ID); err != nil {
		t.Fatalf("expected the event to survive a refused cancellation, got %v", err)
	}
}

func TestPublicBookingService_Cancel_IsIdempotent(t *testing.T) {
	g, _, _, event := bookForCancelTest(t)
	ctx := context.Background()

	token, err := g.Auth.IssueBookingCancelToken(event.ID)
	if err != nil {
		t.Fatalf("issue cancel token: %v", err)
	}

	if err := g.PublicBookings.Cancel(ctx, token, time.Now()); err != nil {
		t.Fatalf("first cancel: %v", err)
	}
	if err := g.PublicBookings.Cancel(ctx, token, time.Now()); err != nil {
		t.Fatalf("expected the second cancel to be a no-op, got %v", err)
	}
}

func TestPublicBookingService_Cancel_LinkedCalendarQueuesDeleteWriteBack(t *testing.T) {
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
	}, time.Now(), "https://cal.example.com")
	if err != nil {
		t.Fatalf("book: %v", err)
	}

	token, err := g.Auth.IssueBookingCancelToken(event.ID)
	if err != nil {
		t.Fatalf("issue cancel token: %v", err)
	}
	if err := g.PublicBookings.Cancel(ctx, token, time.Now()); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	messages, err := g.OutboxRepo.ListPending(ctx, 20)
	if err != nil {
		t.Fatalf("list pending outbox messages: %v", err)
	}
	foundDelete := false
	for _, m := range messages {
		if m.EventID == event.ID && m.Kind == repository.OutboxKindWriteBack && m.Method == repository.OutboxMethodDelete {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Fatalf("expected a queued delete Write-back for the cancelled booking, got %+v", messages)
	}
}

func TestPublicBookingService_Cancel_UnknownTokenEventIsANoop(t *testing.T) {
	g, _, _, _ := bookForCancelTest(t)
	ctx := context.Background()

	token, err := g.Auth.IssueBookingCancelToken("00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("issue cancel token: %v", err)
	}
	if err := g.PublicBookings.Cancel(ctx, token, time.Now()); err != nil {
		t.Fatalf("expected a token naming a nonexistent event to be a no-op, got %v", err)
	}
}
