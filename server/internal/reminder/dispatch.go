package reminder

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// Dispatcher delivers a DueReminder that the scheduler has just newly marked
// fired in the ledger. It is the seam real delivery (Notification insert,
// SMTP send) plugs into without touching the engine — this ticket wires it
// to a no-op/logging sink only (ADR-0021).
type Dispatcher interface {
	Dispatch(ctx context.Context, due DueReminder) error
}

// LogDispatcher logs each dispatched Reminder instead of delivering it — the
// placeholder sink for this ticket; real Notification/Email delivery lands
// in the delivery tickets (#56, #57) behind the same Dispatcher seam.
type LogDispatcher struct{}

func (LogDispatcher) Dispatch(_ context.Context, due DueReminder) error {
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
// A user who has opted a synced device into showing its own reminder
// pop-ups (SyncedDeviceRemindersEnabled, ADR-0027) also goes to Fallback for
// the Notification channel: the device already fires its own alarm from the
// synced VALARM, so inserting a second, server-side Notification would
// double-fire. The Email channel is never gated by this — no device
// competes for it.
type NotificationDispatcher struct {
	Notifications NotificationInserter
	Users         UserLookup
	Fallback      Dispatcher
	Now           func() time.Time
}

func (d NotificationDispatcher) Dispatch(ctx context.Context, due DueReminder) error {
	if due.Channel != "notification" {
		return d.Fallback.Dispatch(ctx, due)
	}

	user, err := d.Users.GetByID(ctx, due.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	// A Disabled User receives no Reminders on any Channel (ADR-0037) — not
	// even the LogDispatcher fallback, since that's still a delivery in
	// spirit for a device that would otherwise show it.
	if user.IsDisabled {
		return nil
	}
	if user.SyncedDeviceRemindersEnabled {
		return d.Fallback.Dispatch(ctx, due)
	}

	_, err = d.Notifications.Insert(ctx, due.UserID, due.EventID, due.OccurrenceStart, due.Title, d.Now())
	return err
}

// UserLookup resolves a Reminder's owning user — the Email-channel
// dispatch seam needs it to find the recipient address (ADR-0021), and the
// Notification-channel seam needs it to check the synced-device-reminders
// toggle (ADR-0027). Satisfied by *repository.UserRepository.
type UserLookup interface {
	GetByID(ctx context.Context, id int64) (repository.User, error)
}

// Mailer sends a plain-text email — the Email-Channel delivery seam
// (ADR-0021, #57). Satisfied by *mailer.SMTPMailer.
type Mailer interface {
	Send(to, subject, body string) error
}

// EmailDispatcher delivers an Email-Channel Reminder by sending an email
// naming the Event and its start time to the user's account address
// (ADR-0021, #57); any other Channel (Notification) isn't this dispatcher's
// concern and goes to Fallback.
type EmailDispatcher struct {
	Users    UserLookup
	Mailer   Mailer
	Fallback Dispatcher
}

func (d EmailDispatcher) Dispatch(ctx context.Context, due DueReminder) error {
	if due.Channel != "email" {
		return d.Fallback.Dispatch(ctx, due)
	}

	user, err := d.Users.GetByID(ctx, due.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	// A Disabled User receives no Reminders on any Channel (ADR-0037).
	if user.IsDisabled {
		return nil
	}
	if user.Email == nil {
		// No destination configured (e.g. the user cleared their account
		// email after creating this Email Reminder) — nothing to send.
		return nil
	}

	subject := fmt.Sprintf("Reminder: %s", due.Title)
	body := fmt.Sprintf("%s starts at %s.", due.Title, due.OccurrenceStart.Format(time.RFC1123))
	return d.Mailer.Send(*user.Email, subject, body)
}
