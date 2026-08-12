package service

import (
	"context"
	"fmt"

	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/repository"
)

// InvitationMailer sends an Invitation email — the outbox delivery seam
// (ADR-0059, ADR-0060). Satisfied by *mailer.SMTPMailer.
type InvitationMailer interface {
	SendInvitation(to, fromName, replyTo, subject string, ics []byte) error
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

// Send builds msg's Invitation from the Event and Attendee state as it
// stands right now — never a snapshot taken when msg was queued, so an
// Invitation sent late still reflects a title fixed or a guest added in the
// meantime — and sends it. Returns nil without sending anything when
// EventService.LoadInvitation reports there is nothing left to send: the
// Attendee was removed, or the Event deleted, after msg was queued. That is
// success, not failure — the outbox Worker marks msg sent either way.
func (s *InvitationSender) Send(ctx context.Context, msg repository.OutboxMessage) error {
	event, masterAnchor, ok, err := s.events.LoadInvitation(ctx, msg.EventID, msg.RecipientUserID)
	if err != nil {
		return fmt.Errorf("load invitation: %w", err)
	}
	if !ok {
		return nil
	}

	var recipient repository.AttendeeWithName
	for _, a := range event.Attendees {
		if a.UserID == msg.RecipientUserID {
			recipient = a
			break
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
	if err := s.mailer.SendInvitation(recipient.Email, event.CreatedByName, event.CreatedByEmail, subject, ics); err != nil {
		return fmt.Errorf("send invitation: %w", err)
	}
	return nil
}
