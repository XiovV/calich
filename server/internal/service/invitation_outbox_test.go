package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// outboxUserID builds a *int64 test OutboxMessages can assign to
// RecipientUserID — composite literals can't take the address of a
// variable inline.
func outboxUserID(id int64) *int64 {
	return &id
}

// newTestOutboxAttendeeService is newTestAttendeeService plus a real,
// wired-in OutboxRepository — standing in for a deployment with SMTP
// configured — so the outbox-enqueueing tests in this file can assert on
// what actually got queued.
func newTestOutboxAttendeeService(t *testing.T) (svc *EventService, outboxRepo *repository.OutboxRepository, ownerID, memberID, otherMemberID, workspaceID int64, calendarID string) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	ctx := context.Background()

	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	member, err := users.Create(ctx, "member", "member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	otherMember, err := users.Create(ctx, "other-member", "other-member@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other member: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(ctx, "Test Workspace", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, owner.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add owner member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, member.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add plain member: %v", err)
	}
	if err := workspaceRepo.AddMember(ctx, workspace.ID, otherMember.ID, repository.WorkspaceRoleMember); err != nil {
		t.Fatalf("add other member: %v", err)
	}

	groupRepo := repository.NewGroupRepository(sqlDB)
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	cal, err := calendarRepo.Create(ctx, owner.ID, workspace.ID, "cal-1", repository.CalendarFields{Name: "Personal", Color: "peacock"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	calendarService := NewCalendarService(calendarRepo, repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), groupRepo)
	outboxRepo = repository.NewOutboxRepository(sqlDB)
	svc = NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, groupRepo, repository.NewNotificationRepository(sqlDB), outboxRepo)

	return svc, outboxRepo, owner.ID, member.ID, otherMember.ID, workspace.ID, cal.ID
}

// TestEventService_AddAttendee_EnqueuesInvitation covers the ADR-0059/
// ADR-0060 core: inviting a Member queues an Invitation alongside the
// Attendee row.
func TestEventService_AddAttendee_EnqueuesInvitation(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 queued invitation, got %+v", pending)
	}
	if pending[0].EventID != event.ID || pending[0].RecipientUserID == nil || *pending[0].RecipientUserID != memberID {
		t.Fatalf("expected an invitation for (%s, %d), got %+v", event.ID, memberID, pending[0])
	}
	if pending[0].Method != repository.OutboxMethodRequest {
		t.Fatalf("expected method %q, got %q", repository.OutboxMethodRequest, pending[0].Method)
	}
}

// TestEventService_AddAttendee_NoOutboxConfigured_QueuesNothingButStillInvites
// covers the AC bullet directly: with no SMTP configured (EventService.outbox
// nil), inviting a Member still succeeds and nothing is queued.
func TestEventService_AddAttendee_NoOutboxConfigured_QueuesNothingButStillInvites(t *testing.T) {
	// newTestAttendeeService (attendee_test.go) wires EventService with a nil
	// outbox — the "no SMTP configured" deployment shape.
	svc, _, _, ownerID, memberID, _, calendarID := newTestAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	// The invite itself must still have taken effect.
	if _, err := svc.attendees.Get(ctx, event.ID, memberID); err != nil {
		t.Fatalf("expected the attendee row to exist regardless of outbox config: %v", err)
	}

	// Nothing was ever queued: a fresh OutboxRepository against the very
	// same underlying db sees no rows at all.
	outboxRepo := repository.NewOutboxRepository(svc.db)
	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected nothing queued with no SMTP configured, got %+v", pending)
	}
}

func TestEventService_AddGroupAttendee_EnqueuesOneInvitationPerAddedMember(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, workspaceID, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	group, err := svc.groups.Create(ctx, workspaceID, "Team")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := svc.groups.AddMember(ctx, group.ID, memberID); err != nil {
		t.Fatalf("add group member: %v", err)
	}
	if err := svc.groups.AddMember(ctx, group.ID, otherMemberID); err != nil {
		t.Fatalf("add other group member: %v", err)
	}

	if _, err := svc.AddGroupAttendee(ctx, ownerID, event.ID, group.ID); err != nil {
		t.Fatalf("add group attendee: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 queued invitations (one per group member), got %+v", pending)
	}
	recipients := map[int64]bool{*pending[0].RecipientUserID: true, *pending[1].RecipientUserID: true}
	if !recipients[memberID] || !recipients[otherMemberID] {
		t.Fatalf("expected invitations for both %d and %d, got %+v", memberID, otherMemberID, pending)
	}
}

func TestEventService_Create_WithAttendees_EnqueuesInvitations(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	event, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{
		CalendarID:      calendarID,
		Title:           "Kickoff",
		Start:           start,
		End:             end,
		AttendeeUserIDs: []int64{memberID, otherMemberID},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 queued invitations, got %+v", pending)
	}
	for _, msg := range pending {
		if msg.EventID != event.ID {
			t.Fatalf("expected every invitation to name event %q, got %+v", event.ID, msg)
		}
	}
}

// TestEventService_Create_RollbackOnInvalidAttendeeQueuesNothing covers the
// AC bullet: a rolled-back create queues nothing — not even for the
// Attendee(s) that were valid and already processed earlier in the same
// transaction before the invalid one was hit.
func TestEventService_Create_RollbackOnInvalidAttendeeQueuesNothing(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()

	const invalidUserID = int64(999999)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	_, err := svc.Create(ctx, ownerID, "evt-1", EventWrite{
		CalendarID:      calendarID,
		Title:           "Kickoff",
		Start:           start,
		End:             end,
		AttendeeUserIDs: []int64{memberID, invalidUserID},
	})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	if _, err := svc.events.GetByID(ctx, "evt-1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected no event to have been created, got %v", err)
	}
	if _, err := svc.attendees.Get(ctx, "evt-1", memberID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the first, otherwise-valid attendee row to have rolled back too, got %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected a rolled-back create to queue nothing, got %+v", pending)
	}
}

func TestEventService_LoadInvitation_HydratesAttendeesAndOrganizer(t *testing.T) {
	svc, _, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, otherMemberID); err != nil {
		t.Fatalf("add other attendee: %v", err)
	}

	loaded, masterAnchor, ok, err := svc.LoadInvitation(ctx, event.ID, memberID)
	if err != nil {
		t.Fatalf("load invitation: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if masterAnchor != nil {
		t.Fatalf("expected a nil masterAnchor for a Master event, got %+v", masterAnchor)
	}
	if loaded.CreatedByName != "owner" || loaded.CreatedByEmail != "owner@example.com" {
		t.Fatalf("expected organizer name/email hydrated, got %+v", loaded)
	}
	if len(loaded.Attendees) != 2 {
		t.Fatalf("expected both attendees hydrated, got %+v", loaded.Attendees)
	}
	if len(loaded.Reminders) != 0 {
		t.Fatalf("expected no Reminders hydrated (an Invitation carries none), got %+v", loaded.Reminders)
	}
}

func TestEventService_LoadInvitation_NotOKWhenEventDoesNotExist(t *testing.T) {
	svc, _, _, memberID, _, _, _ := newTestOutboxAttendeeService(t)
	ctx := context.Background()

	_, _, ok, err := svc.LoadInvitation(ctx, "no-such-event", memberID)
	if err != nil {
		t.Fatalf("load invitation: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for a nonexistent event")
	}
}

// TestEventService_LoadInvitation_NotOKWhenAttendeeWasRemoved covers the
// race the outbox Worker's Sender must handle gracefully: the Attendee row
// is gone by send time, so there is nothing left to send.
func TestEventService_LoadInvitation_NotOKWhenAttendeeWasRemoved(t *testing.T) {
	svc, _, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if err := svc.RemoveAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("remove attendee: %v", err)
	}

	_, _, ok, err := svc.LoadInvitation(ctx, event.ID, memberID)
	if err != nil {
		t.Fatalf("load invitation: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false once the attendee was removed")
	}
}

// fakeInvitationMailer records every SendInvitation call, standing in for
// *mailer.SMTPMailer in InvitationSender tests.
type fakeInvitationMailer struct {
	calls []fakeInvitationCall
	err   error
}

type fakeInvitationCall struct {
	to, fromName, replyTo, subject string
	ics                            []byte
}

func (f *fakeInvitationMailer) SendInvitation(to, fromName, replyTo, subject string, ics []byte) error {
	f.calls = append(f.calls, fakeInvitationCall{to, fromName, replyTo, subject, ics})
	return f.err
}

func TestInvitationSender_Send_BuildsAndSendsTheInvitation(t *testing.T) {
	svc, _, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}

	mailer := &fakeInvitationMailer{}
	sender := NewInvitationSender(svc, mailer, "calendar@example.com")

	err := sender.Send(ctx, repository.OutboxMessage{EventID: event.ID, RecipientUserID: outboxUserID(memberID)})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("expected 1 mailer call, got %+v", mailer.calls)
	}
	call := mailer.calls[0]
	if call.to != "member@example.com" {
		t.Fatalf("expected recipient member@example.com, got %q", call.to)
	}
	if call.fromName != "owner" {
		t.Fatalf("expected the organizer's Name as fromName, got %q", call.fromName)
	}
	if call.replyTo != "owner@example.com" {
		t.Fatalf("expected the organizer's own address as replyTo, got %q", call.replyTo)
	}
	if len(call.ics) == 0 {
		t.Fatalf("expected non-empty ics bytes")
	}
}

// TestInvitationSender_Send_SkipsGracefullyWhenNothingLeftToSend covers the
// outbox Worker's contract: a message whose Attendee was removed since it
// was queued sends nothing and reports no error.
func TestInvitationSender_Send_SkipsGracefullyWhenNothingLeftToSend(t *testing.T) {
	svc, _, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if err := svc.RemoveAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("remove attendee: %v", err)
	}

	mailer := &fakeInvitationMailer{}
	sender := NewInvitationSender(svc, mailer, "calendar@example.com")

	if err := sender.Send(ctx, repository.OutboxMessage{EventID: event.ID, RecipientUserID: outboxUserID(memberID)}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(mailer.calls) != 0 {
		t.Fatalf("expected no mailer call, got %+v", mailer.calls)
	}
}

// outboxEmail builds a *string test OutboxMessages can assign to
// RecipientEmail.
func outboxEmail(address string) *string {
	return &address
}

// TestInvitationSender_Send_BuildsAndSendsTheInvitation_EmailShapedRecipient
// is TestInvitationSender_Send_BuildsAndSendsTheInvitation's email-shaped
// counterpart (#200, ADR-0058): an email-shaped OutboxMessage resolves
// through LoadInvitationForEmail rather than LoadInvitation and mails the
// typed address directly, with no User row to source it from.
func TestInvitationSender_Send_BuildsAndSendsTheInvitation_EmailShapedRecipient(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)
	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}

	mailer := &fakeInvitationMailer{}
	sender := NewInvitationSender(f.events, mailer, "calendar@example.com")

	err := sender.Send(ctx, repository.OutboxMessage{EventID: event.ID, RecipientEmail: outboxEmail("guest@example.com")})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("expected 1 mailer call, got %+v", mailer.calls)
	}
	call := mailer.calls[0]
	if call.to != "guest@example.com" {
		t.Fatalf("expected recipient guest@example.com, got %q", call.to)
	}
	if call.fromName != "owner" {
		t.Fatalf("expected the organizer's Name as fromName, got %q", call.fromName)
	}
	if call.replyTo != "owner@example.com" {
		t.Fatalf("expected the organizer's own address as replyTo, got %q", call.replyTo)
	}
}
