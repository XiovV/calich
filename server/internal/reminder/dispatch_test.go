package reminder

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// fakeNotificationInserter records every insert it's asked to make, standing
// in for the real NotificationRepository.
type fakeNotificationInserter struct {
	inserts []repository.Notification
}

func (f *fakeNotificationInserter) Insert(_ context.Context, userID int64, eventID string, occurrenceStart time.Time, title string, firedAt time.Time) (repository.Notification, error) {
	n := repository.Notification{UserID: userID, EventID: eventID, OccurrenceStart: &occurrenceStart, Title: title, FiredAt: firedAt}
	f.inserts = append(f.inserts, n)
	return n, nil
}

func TestNotificationDispatcher_NotificationChannelInsertsANotification(t *testing.T) {
	inserter := &fakeNotificationInserter{}
	fallback := &fakeDispatcher{}
	fixedNow := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	dispatcher := NotificationDispatcher{Notifications: inserter, Fallback: fallback, Now: func() time.Time { return fixedNow }}

	due := DueReminder{
		EventID:         "evt-1",
		UserID:          7,
		Title:           "Standup",
		ReminderID:      100,
		OccurrenceStart: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
		OffsetMinutes:   30,
		Channel:         "notification",
	}

	if err := dispatcher.Dispatch(context.Background(), due, repository.User{ID: 7}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(inserter.inserts) != 1 {
		t.Fatalf("expected 1 notification insert, got %+v", inserter.inserts)
	}
	got := inserter.inserts[0]
	if got.UserID != 7 || got.EventID != "evt-1" || got.Title != "Standup" || !got.FiredAt.Equal(fixedNow) {
		t.Fatalf("unexpected inserted notification: %+v", got)
	}
	if len(fallback.dispatched) != 0 {
		t.Fatalf("expected fallback not to be called, got %+v", fallback.dispatched)
	}
}

func TestNotificationDispatcher_NonNotificationChannelGoesToFallback(t *testing.T) {
	inserter := &fakeNotificationInserter{}
	fallback := &fakeDispatcher{}
	dispatcher := NotificationDispatcher{Notifications: inserter, Fallback: fallback, Now: time.Now}

	due := DueReminder{EventID: "evt-1", UserID: 7, ReminderID: 100, Channel: "email"}

	if err := dispatcher.Dispatch(context.Background(), due, repository.User{ID: 7}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(inserter.inserts) != 0 {
		t.Fatalf("expected no notification insert for an email Reminder, got %+v", inserter.inserts)
	}
	if len(fallback.dispatched) != 1 || fallback.dispatched[0].EventID != "evt-1" {
		t.Fatalf("expected the email Reminder to reach the fallback, got %+v", fallback.dispatched)
	}
}

// TestNotificationDispatcher_SkipsNotificationChannelWhenUserOptedIntoSyncedDeviceReminders
// covers ADR-0027: a user who has opted a synced device into showing its own
// reminder pop-ups must not also get a server-side Notification for the
// same Reminder.
func TestNotificationDispatcher_SkipsNotificationChannelWhenUserOptedIntoSyncedDeviceReminders(t *testing.T) {
	inserter := &fakeNotificationInserter{}
	fallback := &fakeDispatcher{}
	dispatcher := NotificationDispatcher{Notifications: inserter, Fallback: fallback, Now: time.Now}

	due := DueReminder{EventID: "evt-1", UserID: 7, ReminderID: 100, Channel: "notification"}
	recipient := repository.User{ID: 7, SyncedDeviceRemindersEnabled: true}

	if err := dispatcher.Dispatch(context.Background(), due, recipient); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(inserter.inserts) != 0 {
		t.Fatalf("expected no notification insert when the user opted into synced device reminders, got %+v", inserter.inserts)
	}
	if len(fallback.dispatched) != 1 || fallback.dispatched[0].EventID != "evt-1" {
		t.Fatalf("expected the suppressed Notification Reminder to reach the fallback, got %+v", fallback.dispatched)
	}
}

// TestNotificationDispatcher_InsertsWhenSyncedDeviceRemindersDisabled is the
// default-off half of ADR-0027: a user who hasn't opted in still gets the
// in-app Notification exactly as before.
func TestNotificationDispatcher_InsertsWhenSyncedDeviceRemindersDisabled(t *testing.T) {
	inserter := &fakeNotificationInserter{}
	fallback := &fakeDispatcher{}
	dispatcher := NotificationDispatcher{Notifications: inserter, Fallback: fallback, Now: time.Now}

	due := DueReminder{EventID: "evt-1", UserID: 7, ReminderID: 100, Channel: "notification"}
	recipient := repository.User{ID: 7, SyncedDeviceRemindersEnabled: false}

	if err := dispatcher.Dispatch(context.Background(), due, recipient); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(inserter.inserts) != 1 {
		t.Fatalf("expected a notification insert when synced device reminders are disabled, got %+v", inserter.inserts)
	}
	if len(fallback.dispatched) != 0 {
		t.Fatalf("expected fallback not to be called, got %+v", fallback.dispatched)
	}
}

// TestNotificationDispatcher_EmailChannelUnaffectedBySyncedDeviceRemindersToggle
// covers ADR-0027's "Email channel: always server-fired" — the toggle must
// never gate anything but the Notification channel, so a "notification"
// dispatcher never even needs to consult the toggle for an email Reminder.
func TestNotificationDispatcher_EmailChannelUnaffectedBySyncedDeviceRemindersToggle(t *testing.T) {
	inserter := &fakeNotificationInserter{}
	fallback := &fakeDispatcher{}
	dispatcher := NotificationDispatcher{Notifications: inserter, Fallback: fallback, Now: time.Now}

	due := DueReminder{EventID: "evt-1", UserID: 7, ReminderID: 100, Channel: "email"}
	recipient := repository.User{ID: 7, SyncedDeviceRemindersEnabled: true}

	if err := dispatcher.Dispatch(context.Background(), due, recipient); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(fallback.dispatched) != 1 || fallback.dispatched[0].EventID != "evt-1" {
		t.Fatalf("expected the email Reminder to reach the fallback regardless of the toggle, got %+v", fallback.dispatched)
	}
}

// fakeMailer records every email it's asked to send, standing in for a real
// SMTP mailer.
type fakeMailer struct {
	sent []sentEmail
}

type sentEmail struct {
	to, subject, body string
}

func (f *fakeMailer) Send(to, subject, body string) error {
	f.sent = append(f.sent, sentEmail{to: to, subject: subject, body: body})
	return nil
}

func TestEmailDispatcher_EmailChannelSendsToTheUsersAddress(t *testing.T) {
	mailer := &fakeMailer{}
	fallback := &fakeDispatcher{}
	dispatcher := EmailDispatcher{Mailer: mailer, Fallback: fallback}

	due := DueReminder{
		EventID:         "evt-1",
		UserID:          7,
		Title:           "Standup",
		ReminderID:      100,
		OccurrenceStart: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
		Channel:         "email",
	}

	if err := dispatcher.Dispatch(context.Background(), due, repository.User{ID: 7, Email: "alice@example.com"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(mailer.sent) != 1 {
		t.Fatalf("expected 1 email sent, got %+v", mailer.sent)
	}
	if mailer.sent[0].to != "alice@example.com" {
		t.Fatalf("expected email to alice@example.com, got %+v", mailer.sent[0])
	}
	if len(fallback.dispatched) != 0 {
		t.Fatalf("expected fallback not to be called, got %+v", fallback.dispatched)
	}
}

func TestEmailDispatcher_NonEmailChannelGoesToFallback(t *testing.T) {
	mailer := &fakeMailer{}
	fallback := &fakeDispatcher{}
	dispatcher := EmailDispatcher{Mailer: mailer, Fallback: fallback}

	due := DueReminder{EventID: "evt-1", UserID: 7, ReminderID: 100, Channel: "notification"}

	if err := dispatcher.Dispatch(context.Background(), due, repository.User{ID: 7}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(mailer.sent) != 0 {
		t.Fatalf("expected no email sent for a notification Reminder, got %+v", mailer.sent)
	}
	if len(fallback.dispatched) != 1 || fallback.dispatched[0].EventID != "evt-1" {
		t.Fatalf("expected the notification Reminder to reach the fallback, got %+v", fallback.dispatched)
	}
}

// TestEmailDispatcher_RendersEachRecipientsTimeFormat covers ADR-0039: two
// recipients of one Reminder with different Time format Preferences each get
// their own rendering, read off the recipient the tick already looked up — no
// per-Email query for the Preference.
func TestEmailDispatcher_RendersEachRecipientsTimeFormat(t *testing.T) {
	mailer := &fakeMailer{}
	fallback := &fakeDispatcher{}
	dispatcher := EmailDispatcher{Mailer: mailer, Fallback: fallback}

	occurrenceStart := time.Date(2026, 1, 1, 13, 30, 0, 0, time.UTC)

	if err := dispatcher.Dispatch(context.Background(), DueReminder{
		EventID: "evt-1", UserID: 7, Title: "Standup", ReminderID: 100,
		OccurrenceStart: occurrenceStart, Channel: "email",
	}, repository.User{ID: 7, Email: "alice@example.com", TimeFormat: "12h"}); err != nil {
		t.Fatalf("dispatch alice: %v", err)
	}
	if err := dispatcher.Dispatch(context.Background(), DueReminder{
		EventID: "evt-1", UserID: 8, Title: "Standup", ReminderID: 100,
		OccurrenceStart: occurrenceStart, Channel: "email",
	}, repository.User{ID: 8, Email: "bob@example.com", TimeFormat: "24h"}); err != nil {
		t.Fatalf("dispatch bob: %v", err)
	}

	if len(mailer.sent) != 2 {
		t.Fatalf("expected 2 emails sent, got %+v", mailer.sent)
	}
	if !strings.Contains(mailer.sent[0].body, "1:30:00 PM") {
		t.Fatalf("expected alice's 12h email to contain \"1:30:00 PM\", got %q", mailer.sent[0].body)
	}
	if !strings.Contains(mailer.sent[1].body, "13:30:00") {
		t.Fatalf("expected bob's 24h email to contain \"13:30:00\", got %q", mailer.sent[1].body)
	}
}
