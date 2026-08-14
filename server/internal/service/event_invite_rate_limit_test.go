// event_invite_rate_limit_test.go covers #204/ADR-0058's per-User hourly
// ceiling on queued Invitations: inviting is Editor-gated and stays that
// way, but any Editor can make this instance send mail to an address of
// their choosing, so the mitigation is a ceiling on volume rather than a
// narrower role. Exercised through inviteUser/inviteEmail/expandGroupMembers'
// three callers — AddAttendee, AddAttendeeByEmail, AddGroupAttendee, and
// Create's own staged-Attendee write — since chargeInviteRateLimit is
// unexported and only reachable through them.
package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/apptest"
	"github.com/XiovV/calendar/server/internal/repository"
)

// inviteRateLimitFixture is #204's fixture: a Workspace with two
// independent Calendar owners — each an Editor of their own Calendar, so
// each can act as a distinct inviting actor — a pool of other Members to
// invite, and a Group containing every one of them, for the "large Group
// expansion in one Event" acceptance criterion.
type inviteRateLimitFixture struct {
	events      *EventService
	outbox      *repository.OutboxRepository
	ownerAID    int64
	calendarAID string
	ownerBID    int64
	calendarBID string
	memberIDs   []int64
	groupID     int64
}

// newInviteRateLimitFixture wires a real OutboxRepository (standing in for
// a deployment with SMTP configured, same as newTestOutboxAttendeeService)
// with limitPerHour as EventService's own configured ceiling, and
// memberCount other Workspace Members available to invite.
func newInviteRateLimitFixture(t *testing.T, limitPerHour, memberCount int) inviteRateLimitFixture {
	t.Helper()
	ctx := context.Background()

	// An SMTP transport, so the graph carries the OutboxRepo the rate limit
	// is counted against (ADR-0058, ADR-0060), and limitPerHour as
	// INVITE_RATE_LIMIT_PER_HOUR.
	cfg := apptest.SMTPConfig(t)
	cfg.InviteRateLimitPerHour = limitPerHour
	g := newTestGraphWithConfig(t, cfg)

	users := g.UserRepo
	ownerA, err := users.Create(ctx, "owner-a", "owner-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner a: %v", err)
	}
	ownerB, err := users.Create(ctx, "owner-b", "owner-b@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner b: %v", err)
	}

	workspaceRepo := g.WorkspaceRepo
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", ownerA.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, ownerA.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add owner a: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, ownerB.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add owner b: %v", err)
	}

	memberIDs := make([]int64, memberCount)
	groupRepo := g.GroupRepo
	group, err := groupRepo.Create(ctx, workspace.ID, "Everyone")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	for i := range memberIDs {
		m, err := users.Create(ctx, fmt.Sprintf("member-%d", i), fmt.Sprintf("member-%d@example.com", i), "hash", false)
		if err != nil {
			t.Fatalf("create member %d: %v", i, err)
		}
		if err := workspaceRepo.AddMember(ctx, workspace.ID, m.ID, repository.WorkspaceRoleMember); err != nil {
			t.Fatalf("add member %d: %v", i, err)
		}
		if err := groupRepo.AddMember(ctx, group.ID, m.ID); err != nil {
			t.Fatalf("add member %d to group: %v", i, err)
		}
		memberIDs[i] = m.ID
	}

	calendarRepo := g.CalendarRepo
	calA, err := calendarRepo.Create(ctx, ownerA.ID, workspace.ID, "cal-a", repository.CalendarFields{Name: "A", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar a: %v", err)
	}
	calB, err := calendarRepo.Create(ctx, ownerB.ID, workspace.ID, "cal-b", repository.CalendarFields{Name: "B", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar b: %v", err)
	}

	return inviteRateLimitFixture{
		events: g.Events, outbox: g.OutboxRepo,
		ownerAID: ownerA.ID, calendarAID: calA.ID,
		ownerBID: ownerB.ID, calendarBID: calB.ID,
		memberIDs: memberIDs, groupID: group.ID,
	}
}

func createRateLimitTestEvent(t *testing.T, f inviteRateLimitFixture, ownerID int64, calendarID, id string) repository.Event {
	t.Helper()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	event, err := f.events.Create(context.Background(), ownerID, id, EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	return event
}

// TestEventService_AddAttendee_ExceedingRateLimit_ReturnsClearError covers
// the AC "exceeding it returns a clear error naming the limit": the third
// invite past a ceiling of 2 fails with an *InviteRateLimitError naming that
// exact number, rather than a generic failure.
func TestEventService_AddAttendee_ExceedingRateLimit_ReturnsClearError(t *testing.T) {
	f := newInviteRateLimitFixture(t, 2, 3)
	ctx := context.Background()
	event := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-1")

	if _, err := f.events.AddAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("add attendee 1: %v", err)
	}
	if _, err := f.events.AddAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[1]); err != nil {
		t.Fatalf("add attendee 2: %v", err)
	}

	_, err := f.events.AddAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[2])
	if err == nil {
		t.Fatalf("expected the third invite past a ceiling of 2 to fail")
	}
	var rateLimitErr *InviteRateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected an *InviteRateLimitError, got %T: %v", err, err)
	}
	if rateLimitErr.LimitPerHour != 2 {
		t.Fatalf("expected the error to name the configured limit 2, got %d", rateLimitErr.LimitPerHour)
	}
	if !errors.Is(err, ErrInviteRateLimitExceeded) {
		t.Fatalf("expected errors.Is to match ErrInviteRateLimitExceeded, got %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerAID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected exactly the 2 successful invites to have stuck, not a partial third, got %+v", attendees)
	}
}

// TestEventService_Create_ExceedingRateLimitAcrossMultipleTargets_RollsBackWholeCreate
// covers the AC "rather than partially inviting": a Create naming more
// explicit targets than the actor's remaining budget fails the whole
// create, leaving no Event and no Attendee behind at all — not the Event
// with only the affordable prefix of invites.
func TestEventService_Create_ExceedingRateLimitAcrossMultipleTargets_RollsBackWholeCreate(t *testing.T) {
	f := newInviteRateLimitFixture(t, 2, 3)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := f.events.Create(ctx, f.ownerAID, "evt-1", EventWrite{
		CalendarID:      f.calendarAID,
		Title:           "Standup",
		Start:           start,
		End:             end,
		AttendeeUserIDs: []int64{f.memberIDs[0], f.memberIDs[1], f.memberIDs[2]},
	})
	var rateLimitErr *InviteRateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected an *InviteRateLimitError, got %T: %v", err, err)
	}

	if _, err := f.events.Get(ctx, f.ownerAID, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the whole create to roll back, leaving no Event at all, got %v", err)
	}
}

// TestEventService_AddGroupAttendee_ExceedingRateLimitMidExpansion_RollsBackWholeExpansion
// covers the same atomicity guarantee for a Group expansion (#162,
// ADR-0046): hitting the ceiling partway through a Group's members fails
// the whole expansion, not just the members past the ceiling.
func TestEventService_AddGroupAttendee_ExceedingRateLimitMidExpansion_RollsBackWholeExpansion(t *testing.T) {
	f := newInviteRateLimitFixture(t, 2, 3)
	ctx := context.Background()
	event := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-1")

	_, err := f.events.AddGroupAttendee(ctx, f.ownerAID, event.ID, f.groupID)
	if err == nil {
		t.Fatalf("expected a 3-member group expansion against a ceiling of 2 to fail")
	}
	var rateLimitErr *InviteRateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected an *InviteRateLimitError, got %T: %v", err, err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerAID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 0 {
		t.Fatalf("expected no member of the group to have been invited, got %+v", attendees)
	}
}

// TestEventService_AddGroupAttendee_LargeGroupWithinLimit_Succeeds covers
// the AC "ordinary use, including a large Group expansion in one Event,
// does not trip it": a Group at or under the actor's remaining budget is
// invited in full, in one call.
func TestEventService_AddGroupAttendee_LargeGroupWithinLimit_Succeeds(t *testing.T) {
	const memberCount = 50
	f := newInviteRateLimitFixture(t, 100, memberCount)
	ctx := context.Background()
	event := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-1")

	added, err := f.events.AddGroupAttendee(ctx, f.ownerAID, event.ID, f.groupID)
	if err != nil {
		t.Fatalf("expected a %d-member group expansion within a ceiling of 100 to succeed, got %v", memberCount, err)
	}
	if len(added) != memberCount {
		t.Fatalf("expected all %d members invited, got %d", memberCount, len(added))
	}
}

// TestEventService_AddAttendee_RateLimitIsPerActor_NotSharedAcrossWorkspace
// covers the AC "the limit is per User rather than per Event or per
// Workspace": ownerA exhausting their own budget on one Event leaves ownerB
// — a different actor in the same Workspace — with a fresh one on a
// different Event.
func TestEventService_AddAttendee_RateLimitIsPerActor_NotSharedAcrossWorkspace(t *testing.T) {
	f := newInviteRateLimitFixture(t, 1, 2)
	ctx := context.Background()
	eventA := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-a")
	eventB := createRateLimitTestEvent(t, f, f.ownerBID, f.calendarBID, "evt-b")

	if _, err := f.events.AddAttendee(ctx, f.ownerAID, eventA.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("owner a's first invite: %v", err)
	}
	if _, err := f.events.AddAttendee(ctx, f.ownerAID, eventA.ID, f.memberIDs[1]); !errors.Is(err, ErrInviteRateLimitExceeded) {
		t.Fatalf("expected owner a's second invite to exceed their own ceiling of 1, got %v", err)
	}

	if _, err := f.events.AddAttendee(ctx, f.ownerBID, eventB.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("expected owner b's own ceiling to be untouched by owner a's usage, got %v", err)
	}
}

// TestEventService_AddAttendee_RateLimitIsPerActor_NotPerEvent covers the
// same AC from the other direction: the same actor sharing one ceiling
// across two different Events they both organize, not a fresh budget per
// Event.
func TestEventService_AddAttendee_RateLimitIsPerActor_NotPerEvent(t *testing.T) {
	f := newInviteRateLimitFixture(t, 1, 2)
	ctx := context.Background()
	eventA1 := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-a1")
	eventA2 := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-a2")

	if _, err := f.events.AddAttendee(ctx, f.ownerAID, eventA1.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("invite on first event: %v", err)
	}
	if _, err := f.events.AddAttendee(ctx, f.ownerAID, eventA2.ID, f.memberIDs[1]); !errors.Is(err, ErrInviteRateLimitExceeded) {
		t.Fatalf("expected the same actor's ceiling to carry over to a second Event, got %v", err)
	}
}

// TestEventService_Update_MaterialChangeReinvites_EvenWhenRateLimitExhausted
// covers the AC "lifecycle mail for Events already invited — updates ... —
// is not blocked by the cap": a material-change re-issue to an existing
// Attendee still queues, even though the organizer's own invite budget is
// fully spent.
func TestEventService_Update_MaterialChangeReinvites_EvenWhenRateLimitExhausted(t *testing.T) {
	f := newInviteRateLimitFixture(t, 1, 1)
	ctx := context.Background()
	event := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-1")

	if _, err := f.events.AddAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("spend the budget: %v", err)
	}

	newStart := event.Start.Add(time.Hour)
	newEnd := event.End.Add(time.Hour)
	_, err := f.events.Update(ctx, f.ownerAID, event.ID, EventWrite{
		CalendarID: f.calendarAID,
		Title:      event.Title,
		Start:      newStart,
		End:        newEnd,
	})
	if err != nil {
		t.Fatalf("expected a material-change re-invite to succeed despite an exhausted budget, got %v", err)
	}

	pending, err := f.outbox.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected the original invite plus the material-change re-invite, got %+v", pending)
	}
}

// TestEventService_RemoveAttendee_CancelsAndReinvites_EvenWhenRateLimitExhausted
// covers the other half of the same AC: removing an Attendee (a CANCEL to
// them, a re-send to whoever remains) still queues both messages even
// though the actor's own invite budget is fully spent.
func TestEventService_RemoveAttendee_CancelsAndReinvites_EvenWhenRateLimitExhausted(t *testing.T) {
	f := newInviteRateLimitFixture(t, 2, 2)
	ctx := context.Background()
	event := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-1")

	if _, err := f.events.AddAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("invite member 0: %v", err)
	}
	if _, err := f.events.AddAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[1]); err != nil {
		t.Fatalf("invite member 1: %v", err)
	}

	before, err := f.outbox.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending before removal: %v", err)
	}

	if err := f.events.RemoveAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("expected removal to succeed despite an exhausted budget, got %v", err)
	}

	after, err := f.outbox.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending after removal: %v", err)
	}
	// Removal queues a CANCEL to the member removed plus a re-send to the
	// member who remains — both lifecycle mail, uncharged (#204, ADR-0058) —
	// on top of whatever was already queued.
	if len(after) != len(before)+2 {
		t.Fatalf("expected removal to queue exactly 2 more messages (a cancel plus a re-send), had %d before and %d after: %+v", len(before), len(after), after)
	}
	cancels := 0
	for _, msg := range after {
		if msg.Method == repository.OutboxMethodCancel {
			cancels++
			if msg.ActorUserID != nil {
				t.Fatalf("expected the CANCEL to be uncharged (nil ActorUserID), got %+v", msg)
			}
		}
	}
	if cancels != 1 {
		t.Fatalf("expected exactly 1 CANCEL queued for the removed member, got %d: %+v", cancels, after)
	}
}

// TestEventService_AddAttendeeByEmail_ExceedingRateLimit_ReturnsClearError
// covers the typed-address invite path (ADR-0058, #200): a fresh
// email-shaped invite is charged the same as a User-backed one.
func TestEventService_AddAttendeeByEmail_ExceedingRateLimit_ReturnsClearError(t *testing.T) {
	f := newInviteRateLimitFixture(t, 1, 1)
	ctx := context.Background()
	event := createRateLimitTestEvent(t, f, f.ownerAID, f.calendarAID, "evt-1")

	if _, err := f.events.AddAttendee(ctx, f.ownerAID, event.ID, f.memberIDs[0]); err != nil {
		t.Fatalf("spend the budget: %v", err)
	}

	_, err := f.events.AddAttendeeByEmail(ctx, f.ownerAID, event.ID, "guest@example.com")
	var rateLimitErr *InviteRateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected an *InviteRateLimitError, got %T: %v", err, err)
	}
}
