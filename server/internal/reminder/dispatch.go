package reminder

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// Dispatcher delivers a DueReminder that the scheduler has just newly marked
// fired in the ledger, to recipient — the User it belongs to, read once for
// the whole tick by the scheduler rather than looked up here (#219), and
// already known not to be Disabled (ADR-0037). It is the seam real delivery
// (Notification insert, SMTP send) plugs into without touching the engine —
// this ticket wires it to a no-op/logging sink only (ADR-0021).
type Dispatcher interface {
	Dispatch(ctx context.Context, due DueReminder, recipient repository.User) error
}

// LogDispatcher logs each dispatched Reminder instead of delivering it — the
// placeholder sink for this ticket; real Notification/Email delivery lands
// in the delivery tickets (#56, #57) behind the same Dispatcher seam.
type LogDispatcher struct{}

func (LogDispatcher) Dispatch(_ context.Context, due DueReminder, _ repository.User) error {
	log.Printf(
		"reminder fired: event=%s user=%d channel=%s offsetMinutes=%d occurrenceStart=%s",
		due.EventID, due.UserID, due.Channel, due.OffsetMinutes, due.OccurrenceStart.Format("2006-01-02T15:04:05Z07:00"),
	)
	return nil
}

// NotificationInserter is the Notification-channel dispatch seam: satisfied
// by *repository.NotificationRepository.
type NotificationInserter interface {
	Insert(ctx context.Context, userID int64, eventID string, occurrenceStart time.Time, title string, firedAt time.Time) (repository.Notification, error)
}

// NotificationDispatcher delivers a Notification-Channel Reminder by
// inserting a persistent Notification record (ADR-0021, #56); any other
// Channel (Email) isn't this dispatcher's concern and goes to Fallback —
// LogDispatcher until the Email delivery ticket (#57) lands its own seam.
//
// A recipient who has opted a synced device into showing its own reminder
// pop-ups (SyncedDeviceRemindersEnabled, ADR-0027) also goes to Fallback for
// the Notification channel: the device already fires its own alarm from the
// synced VALARM, so inserting a second, server-side Notification would
// double-fire. The Email channel is never gated by this — no device
// competes for it.
type NotificationDispatcher struct {
	Notifications NotificationInserter
	Fallback      Dispatcher
	Now           func() time.Time
}

func (d NotificationDispatcher) Dispatch(ctx context.Context, due DueReminder, recipient repository.User) error {
	if due.Channel != "notification" {
		return d.Fallback.Dispatch(ctx, due, recipient)
	}
	if recipient.SyncedDeviceRemindersEnabled {
		return d.Fallback.Dispatch(ctx, due, recipient)
	}

	_, err := d.Notifications.Insert(ctx, due.UserID, due.EventID, due.OccurrenceStart, due.Title, d.Now())
	return err
}

// Mailer sends a plain-text email — the Email-Channel delivery seam
// (ADR-0021, #57). Satisfied by *mailer.SMTPMailer.
type Mailer interface {
	Send(to, subject, body string) error
}

// EmailDispatcher delivers an Email-Channel Reminder by sending an email
// naming the Event and its start time to the recipient's account address
// (ADR-0021, #57); any other Channel (Notification) isn't this dispatcher's
// concern and goes to Fallback.
type EmailDispatcher struct {
	Mailer   Mailer
	Fallback Dispatcher
}

func (d EmailDispatcher) Dispatch(ctx context.Context, due DueReminder, recipient repository.User) error {
	if due.Channel != "email" {
		return d.Fallback.Dispatch(ctx, due, recipient)
	}

	subject := fmt.Sprintf("Reminder: %s", due.Title)
	body := fmt.Sprintf("%s starts at %s.", due.Title, due.OccurrenceStart.Format(emailTimeLayout(recipient.TimeFormat)))
	return d.Mailer.Send(recipient.Email, subject, body)
}

// emailTimeLayout is time.RFC1123 with its hour segment swapped for the
// recipient's Time format Preference (ADR-0039) — everything else (date,
// zone) is unaffected; which zone the Email renders in is a separate,
// pre-existing question this doesn't touch. The recipient is already on hand
// from the tick's own batched lookup, so this reads their Preference for free
// rather than issuing a per-Email query.
func emailTimeLayout(timeFormat string) string {
	if timeFormat == "12h" {
		return "Mon, 02 Jan 2006 3:04:05 PM MST"
	}
	return "Mon, 02 Jan 2006 15:04:05 MST"
}
