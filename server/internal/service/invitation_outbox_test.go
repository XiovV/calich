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

// fakeInvitationMailer records every SendInvitation/SendCancellation call,
// standing in for *mailer.SMTPMailer in InvitationSender tests.
type fakeInvitationMailer struct {
	calls       []fakeInvitationCall
	cancelCalls []fakeInvitationCall
	err         error
}

type fakeInvitationCall struct {
	to, fromName, replyTo, subject string
	ics                            []byte
}

func (f *fakeInvitationMailer) SendInvitation(to, fromName, replyTo, subject string, ics []byte) error {
	f.calls = append(f.calls, fakeInvitationCall{to, fromName, replyTo, subject, ics})
	return f.err
}

func (f *fakeInvitationMailer) SendCancellation(to, fromName, replyTo, subject string, ics []byte) error {
	f.cancelCalls = append(f.cancelCalls, fakeInvitationCall{to, fromName, replyTo, subject, ics})
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

// -----------------------------------------------------------------------
// #201: material vs non-material Update re-sends, Delete/RemoveAttendee
// cancellations, and scoped-edit correctness across a recurring series.
// -----------------------------------------------------------------------

func baseEventWrite(calendarID string) EventWrite {
	return EventWrite{
		CalendarID: calendarID,
		Title:      "Discuss tech stack",
		Start:      time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
	}
}

// drainOutbox marks every currently Pending message sent, so a later
// assertion on ListPending only sees what a subsequent write queued.
func drainOutbox(t *testing.T, outboxRepo *repository.OutboxRepository) {
	t.Helper()
	ctx := context.Background()
	pending, err := outboxRepo.ListPending(ctx, 100)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, msg := range pending {
		if err := outboxRepo.MarkSent(ctx, msg.ID, time.Now()); err != nil {
			t.Fatalf("mark sent: %v", err)
		}
	}
}

// TestEventService_Update_MaterialChange_BumpsSequenceAndResendsToAllAttendees
// covers the AC bullet directly: changing start/end re-sends the Invitation
// with the bumped sequence to every current Attendee.
func TestEventService_Update_MaterialChange_BumpsSequenceAndResendsToAllAttendees(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, otherMemberID); err != nil {
		t.Fatalf("add other attendee: %v", err)
	}
	drainOutbox(t, outboxRepo)

	write := baseEventWrite(calendarID)
	write.Start = write.Start.Add(time.Hour)
	write.End = write.End.Add(time.Hour)
	updated, err := svc.Update(ctx, ownerID, event.ID, write)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Sequence != 1 {
		t.Fatalf("expected sequence to bump to 1 on a material change, got %d", updated.Sequence)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 re-sent invitations, got %+v", pending)
	}
	recipients := map[int64]bool{}
	for _, msg := range pending {
		if msg.Method != repository.OutboxMethodRequest {
			t.Fatalf("expected method %q, got %q", repository.OutboxMethodRequest, msg.Method)
		}
		if msg.RecipientUserID != nil {
			recipients[*msg.RecipientUserID] = true
		}
	}
	if !recipients[memberID] || !recipients[otherMemberID] {
		t.Fatalf("expected a re-sent invitation for both attendees, got %+v", pending)
	}
}

// TestEventService_Update_NonMaterialChange_ResendsWithoutBumpingSequence
// covers the AC bullet directly: changing title/description/colour re-sends
// the Invitation but leaves Responses untouched — no sequence bump.
func TestEventService_Update_NonMaterialChange_ResendsWithoutBumpingSequence(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	drainOutbox(t, outboxRepo)

	write := baseEventWrite(calendarID)
	write.Title = "Discuss tech stack (updated)"
	updated, err := svc.Update(ctx, ownerID, event.ID, write)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Sequence != 0 {
		t.Fatalf("expected sequence to stay 0 on a non-material change, got %d", updated.Sequence)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected the title change to still re-send once, got %+v", pending)
	}
	if pending[0].RecipientUserID == nil || *pending[0].RecipientUserID != memberID {
		t.Fatalf("expected the re-send addressed to the attendee, got %+v", pending[0])
	}
}

// TestEventService_Update_UnrelatedFieldChange_QueuesNothing covers the
// negative space: changing something an Invitation never renders (here,
// Reminders — ADR-0059 explicitly excludes VALARM) queues no re-send at all.
func TestEventService_Update_UnrelatedFieldChange_QueuesNothing(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	drainOutbox(t, outboxRepo)

	write := baseEventWrite(calendarID)
	write.Reminders = []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}
	if _, err := svc.Update(ctx, ownerID, event.ID, write); err != nil {
		t.Fatalf("update: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected a Reminders-only change to queue nothing, got %+v", pending)
	}
}

// TestEventService_Update_NoAttendees_QueuesNothing covers the AC bullet
// directly: editing an Event with no Attendees queues nothing, even for a
// material change.
func TestEventService_Update_NoAttendees_QueuesNothing(t *testing.T) {
	svc, outboxRepo, ownerID, _, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")

	write := baseEventWrite(calendarID)
	write.Start = write.Start.Add(time.Hour)
	write.End = write.End.Add(time.Hour)
	if _, err := svc.Update(ctx, ownerID, event.ID, write); err != nil {
		t.Fatalf("update: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected an Event with no Attendees to queue nothing, got %+v", pending)
	}
}

// TestEventService_Update_Override_OnlyResendsToTheOverrideOwnAttendees
// covers the "scoped edits" AC bullet on the edit side: updating an Override
// re-sends only to its own Attendees, never to the Master's — a series is
// never treated as a single Event.
func TestEventService_Update_Override_OnlyResendsToTheOverrideOwnAttendees(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	master, err := svc.Create(ctx, ownerID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY", AttendeeUserIDs: []int64{memberID}})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}

	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, ownerID, "override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)",
		Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
		AttendeeUserIDs: []int64{otherMemberID},
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	drainOutbox(t, outboxRepo)

	write := EventWrite{CalendarID: calendarID, Title: "Standup (moved again)", Start: override.Start.Add(time.Hour), End: override.End.Add(time.Hour)}
	if _, err := svc.Update(ctx, ownerID, override.ID, write); err != nil {
		t.Fatalf("update override: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 re-send (the override's own attendee), got %+v", pending)
	}
	if pending[0].EventID != override.ID {
		t.Fatalf("expected the re-send scoped to the override's own row %q, got %+v", override.ID, pending[0])
	}
	if pending[0].RecipientUserID == nil || *pending[0].RecipientUserID != otherMemberID {
		t.Fatalf("expected the re-send addressed to the override's own attendee, got %+v", pending[0])
	}
}

// TestEventService_Update_RulePatternChange_CancelsDiscardedOverridesOwnAttendees
// covers a rule-pattern change (ADR-0016): it forces "All events" and
// discards the Master's existing Overrides, and each discarded Override is a
// deleted Event exactly as much as one removed through Delete — its own
// Attendees, if any, must still get a METHOD:CANCEL rather than silently
// losing their invite.
func TestEventService_Update_RulePatternChange_CancelsDiscardedOverridesOwnAttendees(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	master, err := svc.Create(ctx, ownerID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY", AttendeeUserIDs: []int64{memberID}})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, ownerID, "override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)",
		Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
		AttendeeUserIDs: []int64{otherMemberID},
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}
	drainOutbox(t, outboxRepo)

	// A weekly rule is a different pattern than daily (samePattern), so this
	// forces "All events" and discards the Override created above.
	write := EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=WEEKLY"}
	if _, err := svc.Update(ctx, ownerID, master.ID, write); err != nil {
		t.Fatalf("update master: %v", err)
	}

	if _, err := svc.events.GetByID(ctx, "override"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the override to actually be discarded, got %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	var sawMasterResend, sawOverrideCancel bool
	for _, msg := range pending {
		switch {
		case msg.EventID == master.ID && msg.Method == repository.OutboxMethodRequest:
			sawMasterResend = true
			if msg.RecipientUserID == nil || *msg.RecipientUserID != memberID {
				t.Fatalf("expected the master's re-send addressed to %d, got %+v", memberID, msg)
			}
		case msg.EventID == "override" && msg.Method == repository.OutboxMethodCancel:
			sawOverrideCancel = true
			if msg.RecipientUserID == nil || *msg.RecipientUserID != otherMemberID {
				t.Fatalf("expected the discarded override's cancellation addressed to %d, got %+v", otherMemberID, msg)
			}
		default:
			t.Fatalf("unexpected queued message: %+v", msg)
		}
	}
	if !sawMasterResend {
		t.Fatalf("expected the master's own material change to re-send to its attendee, got %+v", pending)
	}
	if !sawOverrideCancel {
		t.Fatalf("expected the discarded override's own attendee to get a cancellation, got %+v", pending)
	}
}

// TestEventService_ReparentFrom_MovedOverrideCancelsOldAndReinvitesNew
// covers a "this and following" split (ADR-0016) where the moved Override
// carries its own Attendees: reparenting changes the Override's iTIP UID
// (InvitationToICal keys it off ParentID), so its own Attendees must be
// withdrawn under the old UID and re-invited fresh — a client can only ever
// be told a UID changed via cancel-old/invite-new, never an in-place bump
// (ADR-0059, #201).
func TestEventService_ReparentFrom_MovedOverrideCancelsOldAndReinvitesNew(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, _, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()

	oldMaster, err := svc.Create(ctx, ownerID, "old-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create old master: %v", err)
	}
	newMaster, err := svc.Create(ctx, ownerID, "new-master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC), Rrule: "FREQ=DAILY"})
	if err != nil {
		t.Fatalf("create new master: %v", err)
	}
	recurrenceID := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	override, err := svc.Create(ctx, ownerID, "override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)",
		Start: time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC),
		ParentID: &oldMaster.ID, RecurrenceID: &recurrenceID,
		AttendeeUserIDs: []int64{memberID},
	})
	if err != nil {
		t.Fatalf("create override: %v", err)
	}
	drainOutbox(t, outboxRepo)

	if err := svc.ReparentFrom(ctx, ownerID, oldMaster.ID, newMaster.ID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("reparent: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	var sawCancel, sawResend bool
	for _, msg := range pending {
		if msg.EventID != override.ID {
			continue
		}
		switch msg.Method {
		case repository.OutboxMethodCancel:
			sawCancel = true
			if msg.Snapshot == nil || msg.Snapshot.UID != oldMaster.ID {
				t.Fatalf("expected the cancellation to name the old master's UID %q, got %+v", oldMaster.ID, msg.Snapshot)
			}
			if msg.RecipientUserID == nil || *msg.RecipientUserID != memberID {
				t.Fatalf("expected the cancellation addressed to %d, got %+v", memberID, msg)
			}
		case repository.OutboxMethodRequest:
			sawResend = true
			if msg.RecipientUserID == nil || *msg.RecipientUserID != memberID {
				t.Fatalf("expected the re-invitation addressed to %d, got %+v", memberID, msg)
			}
		}
	}
	if !sawCancel {
		t.Fatalf("expected the moved override's own attendee to be cancelled under the old UID, got %+v", pending)
	}
	if !sawResend {
		t.Fatalf("expected the moved override's own attendee to be re-invited, got %+v", pending)
	}

	// By the time the re-invitation actually sends, the override's own
	// ParentID has already moved — so it renders under the new UID.
	loaded, masterAnchor, ok, err := svc.LoadInvitation(ctx, override.ID, memberID)
	if err != nil {
		t.Fatalf("load invitation: %v", err)
	}
	if !ok {
		t.Fatalf("expected the reparented override to still resolve an invitation")
	}
	if loaded.ParentID == nil || *loaded.ParentID != newMaster.ID {
		t.Fatalf("expected the override's ParentID to now be the new master %q, got %+v", newMaster.ID, loaded.ParentID)
	}
	if masterAnchor == nil || masterAnchor.ID != newMaster.ID {
		t.Fatalf("expected the master anchor to be the new master, got %+v", masterAnchor)
	}
}

// TestEventService_Delete_Master_CancelsEveryAttendee covers the AC bullet
// directly: deleting an Event sends every Attendee a cancellation.
func TestEventService_Delete_Master_CancelsEveryAttendee(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, otherMemberID); err != nil {
		t.Fatalf("add other attendee: %v", err)
	}
	drainOutbox(t, outboxRepo)

	if err := svc.Delete(ctx, ownerID, event.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 cancellations, got %+v", pending)
	}
	recipients := map[int64]bool{}
	for _, msg := range pending {
		if msg.Method != repository.OutboxMethodCancel {
			t.Fatalf("expected method %q, got %q", repository.OutboxMethodCancel, msg.Method)
		}
		if msg.Snapshot == nil || msg.Snapshot.UID != event.ID {
			t.Fatalf("expected a snapshot naming UID %q, got %+v", event.ID, msg.Snapshot)
		}
		if msg.RecipientUserID != nil {
			recipients[*msg.RecipientUserID] = true
		}
	}
	if !recipients[memberID] || !recipients[otherMemberID] {
		t.Fatalf("expected a cancellation for both attendees, got %+v", pending)
	}
}

// TestEventService_Delete_Series_CancelsEachRowsOwnAttendeesOnly covers the
// "scoped edits" AC bullet on the delete side: deleting a whole series
// cancels the Master's own Attendees and, separately, each Override's own
// Attendees with that row's own RECURRENCE-ID — never conflated into one
// undifferentiated series-wide cancellation.
func TestEventService_Delete_Series_CancelsEachRowsOwnAttendeesOnly(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	master, err := svc.Create(ctx, ownerID, "master", EventWrite{CalendarID: calendarID, Title: "Standup", Start: start, End: end, Rrule: "FREQ=DAILY", AttendeeUserIDs: []int64{memberID}})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, ownerID, "override", EventWrite{
		CalendarID: calendarID, Title: "Standup (moved)",
		Start: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC),
		ParentID: &master.ID, RecurrenceID: &recurrenceID,
		AttendeeUserIDs: []int64{otherMemberID},
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}
	drainOutbox(t, outboxRepo)

	if err := svc.Delete(ctx, ownerID, master.ID); err != nil {
		t.Fatalf("delete master: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 cancellations (one per row's own attendee), got %+v", pending)
	}
	var masterCancel, overrideCancel *repository.OutboxMessage
	for i := range pending {
		msg := pending[i]
		if msg.Snapshot == nil {
			t.Fatalf("expected every cancellation to carry a snapshot, got %+v", msg)
		}
		if msg.Snapshot.UID != master.ID {
			t.Fatalf("expected every cancellation to name the master's UID %q, got %+v", master.ID, msg.Snapshot)
		}
		if msg.Snapshot.RecurrenceID == nil {
			masterCancel = &pending[i]
		} else {
			overrideCancel = &pending[i]
		}
	}
	if masterCancel == nil || overrideCancel == nil {
		t.Fatalf("expected one master-scoped and one override-scoped cancellation, got %+v", pending)
	}
	if masterCancel.RecipientUserID == nil || *masterCancel.RecipientUserID != memberID {
		t.Fatalf("expected the master's own cancellation addressed to %d, got %+v", memberID, masterCancel)
	}
	if overrideCancel.RecipientUserID == nil || *overrideCancel.RecipientUserID != otherMemberID {
		t.Fatalf("expected the override's own cancellation addressed to %d, got %+v", otherMemberID, overrideCancel)
	}
	if !overrideCancel.Snapshot.RecurrenceID.Equal(recurrenceID) {
		t.Fatalf("expected the override's cancellation RecurrenceID %v, got %v", recurrenceID, overrideCancel.Snapshot.RecurrenceID)
	}
}

// TestEventService_RemoveAttendee_CancelsOnlyThatPerson covers the AC
// bullet directly: removing an Attendee sends that person a cancellation
// and nobody else gets a cancellation. The Attendee list changing is itself
// a non-material change (ADR-0059), so a remaining Attendee does still get
// an unbumped re-send of the ordinary Invitation — a different message
// entirely from the CANCEL the removed person gets — which this also
// asserts, rather than just counting messages.
func TestEventService_RemoveAttendee_CancelsOnlyThatPerson(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, otherMemberID); err != nil {
		t.Fatalf("add other attendee: %v", err)
	}
	drainOutbox(t, outboxRepo)

	if err := svc.RemoveAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("remove attendee: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 1 cancellation plus 1 re-send to the remaining attendee, got %+v", pending)
	}
	var cancels, requests int
	for _, msg := range pending {
		switch msg.Method {
		case repository.OutboxMethodCancel:
			cancels++
			if msg.RecipientUserID == nil || *msg.RecipientUserID != memberID {
				t.Fatalf("expected the cancellation addressed to the removed attendee %d, got %+v", memberID, msg)
			}
		case repository.OutboxMethodRequest:
			requests++
			if msg.RecipientUserID == nil || *msg.RecipientUserID != otherMemberID {
				t.Fatalf("expected the re-send addressed to the remaining attendee %d, got %+v", otherMemberID, msg)
			}
		}
	}
	if cancels != 1 || requests != 1 {
		t.Fatalf("expected exactly 1 cancellation and 1 re-send, got %d cancels and %d requests: %+v", cancels, requests, pending)
	}
}

// TestEventService_AddAttendee_ResendsToOtherCurrentAttendees covers the
// ADR-0059 text directly: the Attendee list changing is a non-material
// change that still re-sends — the newly invited Attendee gets their own
// fresh invite (already covered by other tests), while every other current
// Attendee gets an unbumped re-send rather than being left with a stale
// guest list on their copy.
func TestEventService_AddAttendee_ResendsToOtherCurrentAttendees(t *testing.T) {
	svc, outboxRepo, ownerID, memberID, otherMemberID, _, calendarID := newTestOutboxAttendeeService(t)
	ctx := context.Background()
	event := createTestEvent(t, svc, ownerID, calendarID, "evt-1")
	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, memberID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	drainOutbox(t, outboxRepo)

	if _, err := svc.AddAttendee(ctx, ownerID, event.ID, otherMemberID); err != nil {
		t.Fatalf("add other attendee: %v", err)
	}

	pending, err := outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 1 fresh invite plus 1 re-send to the existing attendee, got %+v", pending)
	}
	recipients := map[int64]bool{}
	for _, msg := range pending {
		if msg.Method != repository.OutboxMethodRequest {
			t.Fatalf("expected both messages to be REQUESTs, got %+v", msg)
		}
		if msg.RecipientUserID != nil {
			recipients[*msg.RecipientUserID] = true
		}
	}
	if !recipients[memberID] || !recipients[otherMemberID] {
		t.Fatalf("expected a message for both the existing and the newly invited attendee, got %+v", pending)
	}
}

// TestEventService_RemoveAttendeeByEmail_CancelsThatAddress is
// TestEventService_RemoveAttendee_CancelsOnlyThatPerson's email-shaped
// counterpart (#200, ADR-0058).
func TestEventService_RemoveAttendeeByEmail_CancelsThatAddress(t *testing.T) {
	f := newEmailAttendeeFixture(t, true)
	ctx := context.Background()
	event := createEmailAttendeeTestEvent(t, f)
	if _, err := f.events.AddAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("add attendee by email: %v", err)
	}
	drainOutbox(t, f.outboxRepo)

	if err := f.events.RemoveAttendeeByEmail(ctx, f.ownerID, event.ID, "guest@example.com"); err != nil {
		t.Fatalf("remove attendee by email: %v", err)
	}

	pending, err := f.outboxRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 cancellation, got %+v", pending)
	}
	msg := pending[0]
	if msg.Method != repository.OutboxMethodCancel {
		t.Fatalf("expected method %q, got %q", repository.OutboxMethodCancel, msg.Method)
	}
	if msg.RecipientEmail == nil || *msg.RecipientEmail != "guest@example.com" {
		t.Fatalf("expected the cancellation addressed to guest@example.com, got %+v", msg)
	}
}

// TestInvitationSender_Send_Cancellation_BuildsAndSendsFromSnapshot covers
// sendCancellation's own contract: it renders and sends entirely from the
// message's Snapshot, with no Event/Attendee lookup.
func TestInvitationSender_Send_Cancellation_BuildsAndSendsFromSnapshot(t *testing.T) {
	svc, _, _, _, _, _, _ := newTestOutboxAttendeeService(t)
	mailer := &fakeInvitationMailer{}
	sender := NewInvitationSender(svc, mailer, "calendar@example.com")

	msg := repository.OutboxMessage{
		Method:          repository.OutboxMethodCancel,
		RecipientUserID: outboxUserID(1),
		Snapshot: &repository.OutboxCancelSnapshot{
			UID:            "evt-1",
			Start:          time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
			End:            time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			Title:          "Standup",
			OrganizerName:  "Alice Example",
			OrganizerEmail: "alice@example.com",
			RecipientEmail: "bob@example.com",
			RecipientName:  "Bob Guest",
		},
	}

	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(mailer.calls) != 0 {
		t.Fatalf("expected no SendInvitation call, got %+v", mailer.calls)
	}
	if len(mailer.cancelCalls) != 1 {
		t.Fatalf("expected 1 SendCancellation call, got %+v", mailer.cancelCalls)
	}
	call := mailer.cancelCalls[0]
	if call.to != "bob@example.com" {
		t.Fatalf("expected recipient bob@example.com, got %q", call.to)
	}
	if call.fromName != "Alice Example" {
		t.Fatalf("expected the organizer's Name as fromName, got %q", call.fromName)
	}
	if call.replyTo != "alice@example.com" {
		t.Fatalf("expected the organizer's own address as replyTo, got %q", call.replyTo)
	}
	if call.subject != "Cancelled: Standup" {
		t.Fatalf("expected subject %q, got %q", "Cancelled: Standup", call.subject)
	}
	if len(call.ics) == 0 {
		t.Fatalf("expected non-empty ics bytes")
	}
}

// TestInvitationSender_Send_Cancellation_ErrorsWithoutSnapshot covers the
// defensive guard: a CANCEL message with no Snapshot (which never happens
// through EventService's own enqueue paths) fails loudly rather than
// building a bogus Cancellation.
func TestInvitationSender_Send_Cancellation_ErrorsWithoutSnapshot(t *testing.T) {
	svc, _, _, _, _, _, _ := newTestOutboxAttendeeService(t)
	mailer := &fakeInvitationMailer{}
	sender := NewInvitationSender(svc, mailer, "calendar@example.com")

	err := sender.Send(context.Background(), repository.OutboxMessage{Method: repository.OutboxMethodCancel, RecipientUserID: outboxUserID(1)})
	if err == nil {
		t.Fatalf("expected an error for a CANCEL message with no snapshot")
	}
}
