package icalendar

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
)

func mustEncodeInvitation(t *testing.T, event repository.Event, masterAnchor *repository.Event, fromAddress string) string {
	t.Helper()
	cal, err := InvitationToICal(event, masterAnchor, fromAddress)
	if err != nil {
		t.Fatalf("InvitationToICal: %v", err)
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}

func TestInvitationToICal_CarriesMethodRequest(t *testing.T) {
	event := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncodeInvitation(t, event, nil, "calendar@example.com")

	if !strings.Contains(body, "METHOD:REQUEST") {
		t.Fatalf("expected METHOD:REQUEST, got:\n%s", body)
	}
	if !strings.Contains(body, "UID:evt-1") {
		t.Fatalf("expected UID:evt-1, got:\n%s", body)
	}
	if strings.Contains(body, "RECURRENCE-ID") {
		t.Fatalf("expected no RECURRENCE-ID for a Master, got:\n%s", body)
	}
}

// TestInvitationToICal_NoVALARM_EvenWhenEventCarriesReminders covers the
// AC bullet directly: whatever Reminders the Event has, the Invitation
// carries none, so the recipient's own client applies its own default
// alarm rather than the Organizer's habits (ADR-0059).
func TestInvitationToICal_NoVALARM_EvenWhenEventCarriesReminders(t *testing.T) {
	event := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Reminders: []repository.Reminder{
			{OffsetMinutes: 10, Channel: "notification"},
			{OffsetMinutes: 30, Channel: "email"},
		},
	}

	body := mustEncodeInvitation(t, event, nil, "calendar@example.com")

	if strings.Contains(body, "BEGIN:VALARM") {
		t.Fatalf("expected no VALARM in an Invitation, got:\n%s", body)
	}
}

// TestInvitationToICal_OrganizerNamesTheInstanceMailbox_NotTheOrganizersOwn
// covers ADR-0059's ORGANIZER split: the Invitation's ORGANIZER carries the
// Organizer's Name as CN, but mailto's the instance's own SMTP_FROM address
// — never the Organizer's real email, which travels as Reply-To on the
// email itself instead (mailer.SendInvitation's concern, not the codec's).
func TestInvitationToICal_OrganizerNamesTheInstanceMailbox_NotTheOrganizersOwn(t *testing.T) {
	organizerID := int64(7)
	event := repository.Event{
		ID:             "evt-1",
		Title:          "Meeting",
		Start:          time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:            time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:      &organizerID,
		CreatedByName:  "Alice Example",
		CreatedByEmail: "alice@example.com",
	}

	body := mustEncodeInvitation(t, event, nil, "calendar@example.com")

	if !strings.Contains(body, "ORGANIZER;CN=Alice Example:mailto:calendar@example.com") {
		t.Fatalf("expected ORGANIZER naming the instance mailbox with the organizer's CN, got:\n%s", body)
	}
	if strings.Contains(body, "mailto:alice@example.com") {
		t.Fatalf("expected the organizer's own address to not appear anywhere in the Invitation, got:\n%s", body)
	}
}

func TestInvitationToICal_NoOrganizer_WhenCreatedByNil(t *testing.T) {
	event := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncodeInvitation(t, event, nil, "calendar@example.com")

	if strings.Contains(body, "ORGANIZER") {
		t.Fatalf("expected no ORGANIZER when CreatedBy is nil, got:\n%s", body)
	}
}

// TestInvitationToICal_AttendeesEmittedWithPartstatAndMailto covers
// ADR-0062: the Invitation carries the same ATTENDEE lines CalDAV and a
// Calendar file do.
func TestInvitationToICal_AttendeesEmittedWithPartstatAndMailto(t *testing.T) {
	event := repository.Event{
		ID:        "evt-1",
		Title:     "Meeting",
		Start:     time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Attendees: []repository.AttendeeWithName{
			{Attendee: repository.Attendee{UserID: 1, Response: repository.ResponseAccepted}, Name: "Bob Guest", Email: "bob@example.com"},
			{Attendee: repository.Attendee{UserID: 2, Response: repository.ResponseNeedsAction}, Name: "Carol Guest", Email: "carol@example.com"},
		},
	}

	body := mustEncodeInvitation(t, event, nil, "calendar@example.com")

	if !strings.Contains(body, "ATTENDEE;CN=Bob Guest;PARTSTAT=ACCEPTED:mailto:bob@example.com") {
		t.Fatalf("expected an ATTENDEE for Bob with PARTSTAT=ACCEPTED, got:\n%s", body)
	}
	if !strings.Contains(body, "ATTENDEE;CN=Carol Guest;PARTSTAT=NEEDS-ACTION:mailto:carol@example.com") {
		t.Fatalf("expected an ATTENDEE for Carol with PARTSTAT=NEEDS-ACTION, got:\n%s", body)
	}
}

// TestInvitationToICal_Override_UsesMasterUIDAndRecurrenceID covers an
// Attendee invited on an Override specifically: the Invitation must name
// the series' own UID (the Master's id) with a RECURRENCE-ID, the same
// shape appendSeriesVEvents produces for CalDAV, formatted against the
// Master's own AllDay/Tzid (masterAnchor) rather than the Override's.
func TestInvitationToICal_Override_UsesMasterUIDAndRecurrenceID(t *testing.T) {
	masterID := "evt-master"
	recurrenceID := time.Date(2026, 7, 8, 15, 0, 0, 0, time.UTC)
	master := repository.Event{
		ID:    masterID,
		Title: "Weekly sync",
		Start: time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		Rrule: "FREQ=WEEKLY",
	}
	override := repository.Event{
		ID:           "evt-override",
		ParentID:     &masterID,
		RecurrenceID: &recurrenceID,
		Title:        "Weekly sync (moved)",
		Start:        time.Date(2026, 7, 8, 16, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 7, 8, 17, 0, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := mustEncodeInvitation(t, override, &master, "calendar@example.com")

	if !strings.Contains(body, "UID:"+masterID) {
		t.Fatalf("expected the Master's own UID, got:\n%s", body)
	}
	if !strings.Contains(body, "RECURRENCE-ID") {
		t.Fatalf("expected a RECURRENCE-ID for an Override Invitation, got:\n%s", body)
	}
}
