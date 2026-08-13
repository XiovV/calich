// attendee_email_test.go covers #200/ADR-0058: inviting a bare email
// address as an Attendee, resolved against the Event's own Workspace at
// write time.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// emailAttendeeFixture wires an EventService (with a real, non-nil outbox
// unless noted otherwise) plus every actor #200's acceptance criteria name:
// an owner (the Organizer), an enabled Workspace Member, a Disabled one, and
// a User who holds an account on this instance but as a Member of a
// *different* Workspace — ADR-0058's "treated as an outsider" case, which
// newTestAttendeeService's own outsider (no Workspace at all) doesn't cover.
type emailAttendeeFixture struct {
	events           *EventService
	outboxRepo       *repository.OutboxRepository
	ownerID          int64
	memberID         int64
	disabledMemberID int64
	otherWorkspaceID int64
	calendarID       string
}

func newEmailAttendeeFixture(t *testing.T, withOutbox bool) emailAttendeeFixture {
	t.Helper()
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	disabledMember, err := users.Create(ctx, "disabled", "disabled@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create disabled member: %v", err)
	}
	if _, err := users.SetDisabled(ctx, disabledMember.ID, true); err != nil {
		t.Fatalf("disable member: %v", err)
	}
	crossWorkspaceUser, err := users.Create(ctx, "cross", "cross@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create cross-workspace user: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	for _, id := range []int64{owner.ID, member.ID, disabledMember.ID} {
		if err := workspaceRepo.AddMember(ctx, workspace.ID, id, repository.WorkspaceRoleMember); err != nil {
			t.Fatalf("add workspace member: %v", err)
		}
	}

	otherWorkspace, err := workspaceRepo.Create(ctx, "Other Workspace", crossWorkspaceUser.ID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, otherWorkspace.ID, crossWorkspaceUser.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add cross-workspace user to other workspace: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	cal, err := calendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	var outboxRepo *repository.OutboxRepository
	if withOutbox {
		outboxRepo = repository.NewOutboxRepository(sqlDB)
	}

	calendarService := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewEventReminderRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	eventService := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB), repository.NewNotificationRepository(sqlDB), outboxRepo, 1000)

	return emailAttendeeFixture{
		events: eventService, outboxRepo: outboxRepo,
		ownerID: owner.ID, memberID: member.ID, disabledMemberID: disabledMember.ID,
		otherWorkspaceID: otherWorkspace.ID, calendarID: cal.ID,
	}
}

func createEmailAttendeeTestEvent(t *testing.T, f emailAttendeeFixture) repository.Event {
	t.Helper()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	event, err := f.events.Create(context.Background(), f.ownerID, "evt-1", EventWrite{CalendarID: f.calendarID, Title: "Discuss tech stack", Start: start, End: end})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	return event
}

// TestEventService_AddAttendeeByEmail_UnknownAddress_WritesEmailShapedAttendeeAndQueuesInvitation
// covers the AC's core bullet: typing an outside address invites them and
// sends the invitation.
func TestEventService_AddAttendeeByEmail_UnknownAddress_WritesEmailShapedAttendeeAndQueuesInvitation(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	attendee, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com")
	if err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}
	if attendee.UserID != nil {
		t.Fatalf("expected a nil UserID for an unresolved address, got %v", *attendee.UserID)
	}
	if attendee.Email != "guest@example.com" {
		t.Fatalf("expected Email %q, got %q", "guest@example.com", attendee.Email)
	}

	pending, err := f.outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 queued invitation, got %+v", pending)
	}
	if pending[0].RecipientEmail == nil || *pending[0].RecipientEmail != "guest@example.com" {
		t.Fatalf("expected the invitation queued for guest@example.com, got %+v", pending[0])
	}
	if pending[0].RecipientUserID != nil {
		t.Fatalf("expected a nil RecipientUserID, got %v", *pending[0].RecipientUserID)
	}
}

// TestEventService_AddAttendeeByEmail_MatchingWorkspaceMember_WritesUserBackedAttendee
// covers the AC bullet: a typed address matching a Member of the Event's
// Workspace produces a User-backed Attendee, not an email one — including
// the invite Notification ADR-0061 requires for a User-backed row, which an
// email-shaped one never gets.
func TestEventService_AddAttendeeByEmail_MatchingWorkspaceMember_WritesUserBackedAttendee(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	attendee, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "MEMBER@Example.com")
	if err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}
	if attendee.UserID == nil || *attendee.UserID != f.memberID {
		t.Fatalf("expected a User-backed Attendee for member %d, got %+v", f.memberID, attendee)
	}

	notifications, err := f.events.notifications.ListRecentByUser(ctx, f.memberID, 10)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("expected 1 invite notification for the resolved member, got %+v", notifications)
	}
}

// TestEventService_AddAttendeeByEmail_MatchingUserInDifferentWorkspace_WritesEmailShapedAttendee
// covers ADR-0058's deliberate "treated as an outsider": an account that
// exists on this instance, just not in the Event's own Workspace, gets an
// email-shaped row rather than reaching across the Workspace boundary.
func TestEventService_AddAttendeeByEmail_MatchingUserInDifferentWorkspace_WritesEmailShapedAttendee(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	attendee, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "cross@example.com")
	if err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}
	if attendee.UserID != nil {
		t.Fatalf("expected an email-shaped Attendee for a cross-Workspace account, got UserID %v", *attendee.UserID)
	}
	if attendee.Email != "cross@example.com" {
		t.Fatalf("expected Email %q, got %q", "cross@example.com", attendee.Email)
	}
}

// TestEventService_AddAttendeeByEmail_SameAddressTwiceUnderEitherShape_Conflicts
// covers case-insensitive matching and "cannot be invited twice under
// either shape": a second, differently-cased invite of the same unresolved
// address conflicts, and so does re-typing a Member's address once they're
// already a User-backed Attendee.
func TestEventService_AddAttendeeByEmail_SameAddressTwiceUnderEitherShape_Conflicts(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "Guest@Example.com"); !errors.Is(err, repository.ErrAlreadyAttendee) {
		t.Fatalf("expected ErrAlreadyAttendee for a case-different repeat of an unresolved address, got %v", err)
	}

	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "member@example.com"); err != nil {
		t.Fatalf("invite member by email: %v", err)
	}
	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "member@example.com"); !errors.Is(err, repository.ErrAlreadyAttendee) {
		t.Fatalf("expected ErrAlreadyAttendee re-inviting an already-resolved member, got %v", err)
	}
}

// TestEventService_AddAttendeeByEmail_MalformedAddress_ReturnsErrInvalidEmail
// covers the AC bullet: a malformed address is the one error the user can
// fix, rejected as such.
func TestEventService_AddAttendeeByEmail_MalformedAddress_ReturnsErrInvalidEmail(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "not-an-email"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}
}

// TestEventService_AddAttendeeByEmail_OrganizersOwnAddress_Rejected covers
// the AC bullet: the Organizer's own address is rejected with an error,
// whether typed by the Organizer or by an Editor — ADR-0055 made the
// Organizer structurally not an Attendee.
func TestEventService_AddAttendeeByEmail_OrganizersOwnAddress_Rejected(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "Owner@Example.com"); !errors.Is(err, ErrAttendeeIsOrganizer) {
		t.Fatalf("expected ErrAttendeeIsOrganizer, got %v", err)
	}
}

// TestEventService_AddAttendeeByEmail_DisabledMembersAddress_RejectedNotDowngraded
// covers the AC bullet: a Disabled Member's address is rejected rather than
// treated as an outside address — verifying no email-shaped row is left
// behind either.
func TestEventService_AddAttendeeByEmail_DisabledMembersAddress_RejectedNotDowngraded(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "disabled@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound (same rejection an explicit disabled target gets), got %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 0 {
		t.Fatalf("expected no Attendee row written at all (not even an email-shaped one), got %+v", attendees)
	}
}

// TestEventService_AddAttendeeByEmail_NoOutboxConfigured_UnknownAddress_Unavailable
// covers ADR-0058's "the affordance is absent entirely" applied to the
// write path: with no SMTP configured, an unresolved address is refused
// rather than written as a row nobody can ever deliver to.
func TestEventService_AddAttendeeByEmail_NoOutboxConfigured_UnknownAddress_Unavailable(t *testing.T) {
	f := newEmailAttendeeFixture(t, false)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); !errors.Is(err, ErrAttendeeEmailInvitesUnavailable) {
		t.Fatalf("expected ErrAttendeeEmailInvitesUnavailable, got %v", err)
	}
}

// TestEventService_AddAttendeeByEmail_NoOutboxConfigured_MatchingMember_StillInvites
// covers that a resolved Member invite is unaffected by SMTP being absent —
// the existing invite-a-Member posture (no email delivery, but the in-app
// Notification and visibility grant still happen) carries over unchanged.
func TestEventService_AddAttendeeByEmail_NoOutboxConfigured_MatchingMember_StillInvites(t *testing.T) {
	f := newEmailAttendeeFixture(t, false)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	attendee, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "member@example.com")
	if err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}
	if attendee.UserID == nil || *attendee.UserID != f.memberID {
		t.Fatalf("expected a User-backed Attendee, got %+v", attendee)
	}
}

// TestEventService_RemoveAttendeeByEmail_RevokesInvite covers the
// email-shaped counterpart to RemoveAttendee.
func TestEventService_RemoveAttendeeByEmail_RevokesInvite(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)

	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}
	if err := f.events.RemoveAttendeeByEmail(ctx, f.ownerID, event.ID, "Guest@Example.com"); err != nil {
		t.Fatalf("remove attendee by email: %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 0 {
		t.Fatalf("expected the attendee removed, got %+v", attendees)
	}
}

// TestEventService_Create_WithAttendeeEmails_StagedInSameTransaction covers
// the AC bullet: addresses can be staged while creating an Event and are
// written in the same transaction as the Event.
func TestEventService_Create_WithAttendeeEmails_StagedInSameTransaction(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	event, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Discuss tech stack", Start: start, End: end,
		AttendeeEmails: []string{"guest@example.com", "member@example.com"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	attendees, err := f.events.ListAttendees(ctx, f.ownerID, event.ID)
	if err != nil {
		t.Fatalf("list attendees: %v", err)
	}
	if len(attendees) != 2 {
		t.Fatalf("expected 2 attendees, got %+v", attendees)
	}
}

// TestEventService_Create_WithAttendeeEmails_MalformedAddressRollsBackWholeCreate
// covers the AC bullet directly: a malformed address rejects the whole
// request, rolling back an in-flight create — no Event persisted.
func TestEventService_Create_WithAttendeeEmails_MalformedAddressRollsBackWholeCreate(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	_, err := f.events.Create(ctx, f.ownerID, "evt-1", EventWrite{
		CalendarID: f.calendarID, Title: "Discuss tech stack", Start: start, End: end,
		AttendeeEmails: []string{"guest@example.com", "not-an-email"},
	})
	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("expected ErrInvalidEmail, got %v", err)
	}

	if _, err := f.events.Get(ctx, f.ownerID, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected no Event to have been created, got %v", err)
	}
	pending, err := f.outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected nothing queued from a rolled-back create, got %+v", pending)
	}
}

// TestEventService_LoadInvitationForEmail_ResolvesAnEmailShapedRecipient
// covers the outbox Worker's Sender path for an email-shaped recipient
// (#200, ADR-0058): the same Event/Organizer hydration LoadInvitation gives
// a User-backed recipient, keyed on Email instead of a user_id.
func TestEventService_LoadInvitationForEmail_ResolvesAnEmailShapedRecipient(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)
	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}

	loaded, _, ok, err := f.events.LoadInvitationForEmail(ctx, event.ID, "guest@example.com")
	if err != nil {
		t.Fatalf("load invitation for email: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if loaded.CreatedByEmail != "owner@example.com" {
		t.Fatalf("expected the Organizer's email hydrated, got %q", loaded.CreatedByEmail)
	}
	found := false
	for _, a := range loaded.Attendees {
		if a.UserID == nil && a.Email == "guest@example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the email-shaped Attendee in the hydrated list, got %+v", loaded.Attendees)
	}
}

// TestEventService_LoadInvitationForEmail_NotFoundAfterRemoval covers the
// Worker's "nothing left to send" contract for an email-shaped recipient,
// mirroring LoadInvitation's own removed-Attendee case.
func TestEventService_LoadInvitationForEmail_NotFoundAfterRemoval(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)
	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}
	if err := f.events.RemoveAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("remove attendee by email: %v", err)
	}

	_, _, ok, err := f.events.LoadInvitationForEmail(ctx, event.ID, "guest@example.com")
	if err != nil {
		t.Fatalf("load invitation for email: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false once the email-shaped Attendee was removed")
	}
}
