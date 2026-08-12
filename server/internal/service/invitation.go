package service

import (
	"context"
	"fmt"

	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/repository"
)

// InvitationMailer sends an Invitation or Cancellation email — the outbox
// delivery seam (ADR-0059, ADR-0060, #201). Satisfied by *mailer.SMTPMailer.
type InvitationMailer interface {
	SendInvitation(to, fromName, replyTo, subject string, ics []byte) error
	SendCancellation(to, fromName, replyTo, subject string, ics []byte) error
}

// InvitationSender turns one queued outbox.Message into an Invitation and
// sends it — the outbox.Sender the outbox Worker drains through. from is
// this instance's own mailbox (SMTP_FROM), the address ORGANIZER carries on
// every Invitation regardless of who organized it (ADR-0059).
type InvitationSender struct {
	events *EventService
	mailer InvitationMailer
	from   string
}

func NewInvitationSender(events *EventService, mailer InvitationMailer, from string) *InvitationSender {
	return &InvitationSender{events: events, mailer: mailer, from: from}
}

// Send dispatches msg to sendInvitation or sendCancellation by its Method
// (ADR-0059, #201).
func (s *InvitationSender) Send(ctx context.Context, msg repository.OutboxMessage) error {
	if msg.Method == repository.OutboxMethodCancel {
		return s.sendCancellation(msg)
	}
	return s.sendInvitation(ctx, msg)
}

// sendInvitation builds msg's Invitation from the Event and Attendee state
// as it stands right now — never a snapshot taken when msg was queued, so an
// Invitation sent late still reflects a title fixed or a guest added in the
// meantime — and sends it. Dispatches to LoadInvitation or
// LoadInvitationForEmail depending on which shape msg's recipient is
// (#200, ADR-0058). Returns nil without sending anything when that reports
// there is nothing left to send: the Attendee was removed, or the Event
// deleted, after msg was queued. That is success, not failure — the outbox
// Worker marks msg sent either way.
func (s *InvitationSender) sendInvitation(ctx context.Context, msg repository.OutboxMessage) error {
	var event repository.Event
	var masterAnchor *repository.Event
	var ok bool
	var err error
	var recipientEmail string
	if msg.RecipientUserID != nil {
		event, masterAnchor, ok, err = s.events.LoadInvitation(ctx, msg.EventID, *msg.RecipientUserID)
	} else {
		recipientEmail = *msg.RecipientEmail
		event, masterAnchor, ok, err = s.events.LoadInvitationForEmail(ctx, msg.EventID, recipientEmail)
	}
	if err != nil {
		return fmt.Errorf("load invitation: %w", err)
	}
	if !ok {
		return nil
	}

	// A User-backed recipient's Email rides along on the Attendee row
	// itself (repository.AttendeeWithName.Email); an email-shaped
	// recipient's is already known from msg and doesn't need looking up.
	if msg.RecipientUserID != nil {
		for _, a := range event.Attendees {
			if a.UserID != nil && *a.UserID == *msg.RecipientUserID {
				recipientEmail = a.Email
				break
			}
		}
	}

	cal, err := icalendar.InvitationToICal(event, masterAnchor, s.from)
	if err != nil {
		return fmt.Errorf("build invitation ical: %w", err)
	}
	ics, err := icalendar.Encode(cal)
	if err != nil {
		return fmt.Errorf("encode invitation ical: %w", err)
	}

	subject := fmt.Sprintf("Invitation: %s", event.Title)
	if err := s.mailer.SendInvitation(recipientEmail, event.CreatedByName, event.CreatedByEmail, subject, ics); err != nil {
		return fmt.Errorf("send invitation: %w", err)
	}
	return nil
}

// sendCancellation builds and sends msg's Cancellation entirely from its own
// Snapshot (ADR-0059, ADR-0060, #201) — no Event or Attendee lookup, unlike
// sendInvitation, since a Cancellation's purpose is to still render
// correctly after the row (or Attendee row) it withdraws is gone.
func (s *InvitationSender) sendCancellation(msg repository.OutboxMessage) error {
	if msg.Snapshot == nil {
		return fmt.Errorf("cancel outbox message %d carries no snapshot", msg.ID)
	}

	cal, err := icalendar.CancellationToICal(*msg.Snapshot, s.from)
	if err != nil {
		return fmt.Errorf("build cancellation ical: %w", err)
	}
	ics, err := icalendar.Encode(cal)
	if err != nil {
		return fmt.Errorf("encode cancellation ical: %w", err)
	}

	subject := fmt.Sprintf("Cancelled: %s", msg.Snapshot.Title)
	if err := s.mailer.SendCancellation(msg.Snapshot.RecipientEmail, msg.Snapshot.OrganizerName, msg.Snapshot.OrganizerEmail, subject, ics); err != nil {
		return fmt.Errorf("send cancellation: %w", err)
	}
	return nil
}
