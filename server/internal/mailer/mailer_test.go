package mailer

import (
	"bytes"
	"encoding/base64"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

// parseInvitation builds the raw message SendInvitation would hand to
// smtp.SendMail (headers plus a multipart/alternative body) without
// actually dialing SMTP, splitting it back into its plain-text and
// text/calendar parts so a test can assert on each.
func parseInvitation(t *testing.T, m *SMTPMailer, to, fromName, replyTo, subject string, ics []byte) (headers mail.Header, plainBody string, calBody []byte, calHeader map[string][]string) {
	t.Helper()

	// SendInvitation builds the full message then hands it to
	// smtp.SendMail, which this test can't intercept without a real (or
	// fake) SMTP server. Rebuilding the same message here directly would
	// duplicate SendInvitation's own logic and test nothing; instead this
	// calls buildInvitationMessage, the pure builder SendInvitation itself
	// calls before dialing SMTP.
	raw, err := buildInvitationMessage(m, to, fromName, replyTo, subject, ics)
	if err != nil {
		t.Fatalf("build invitation message: %v", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("expected a multipart body, got %q", mediaType)
	}

	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(part); err != nil {
			t.Fatalf("read part: %v", err)
		}

		ct := part.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "text/plain"):
			plainBody = body.String()
		case strings.HasPrefix(ct, "text/calendar"):
			decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body.String(), "\r\n", ""))
			if err != nil {
				t.Fatalf("decode calendar part: %v", err)
			}
			calBody = decoded
			calHeader = map[string][]string(part.Header)
		}
	}

	return msg.Header, plainBody, calBody, calHeader
}

func TestSendInvitation_CalendarPartCarriesMethodRequestAndOriginalBytes(t *testing.T) {
	m := &SMTPMailer{Host: "smtp.example.com", Port: "587", From: "calendar@example.com"}
	ics := []byte("BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nEND:VCALENDAR\r\n")

	_, _, calBody, calHeader := parseInvitation(t, m, "bob@example.com", "Alice Example", "alice@example.com", "Invitation: Standup", ics)

	if !bytes.Equal(calBody, ics) {
		t.Fatalf("expected the calendar part to round-trip the original ics bytes, got:\n%s", calBody)
	}
	ct := strings.Join(calHeader["Content-Type"], "")
	if !strings.Contains(ct, "text/calendar") || !strings.Contains(ct, "method=REQUEST") {
		t.Fatalf("expected text/calendar;method=REQUEST, got %q", ct)
	}
}

func TestSendInvitation_FromStaysTheInstanceMailbox_ReplyToIsTheOrganizer(t *testing.T) {
	m := &SMTPMailer{Host: "smtp.example.com", Port: "587", From: "calendar@example.com"}
	ics := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")

	headers, _, _, _ := parseInvitation(t, m, "bob@example.com", "Alice Example", "alice@example.com", "Invitation: Standup", ics)

	from, err := headers.AddressList("From")
	if err != nil || len(from) != 1 {
		t.Fatalf("parse From: %v (%+v)", err, from)
	}
	if from[0].Address != "calendar@example.com" {
		t.Fatalf("expected From to stay the instance mailbox, got %q", from[0].Address)
	}
	if from[0].Name != "Alice Example" {
		t.Fatalf("expected the organizer's Name as From's display name, got %q", from[0].Name)
	}

	replyTo, err := headers.AddressList("Reply-To")
	if err != nil || len(replyTo) != 1 {
		t.Fatalf("parse Reply-To: %v (%+v)", err, replyTo)
	}
	if replyTo[0].Address != "alice@example.com" {
		t.Fatalf("expected Reply-To to be the organizer's own address, got %q", replyTo[0].Address)
	}
}

func TestSendInvitation_NoReplyToWhenOrganizerIsUnknown(t *testing.T) {
	m := &SMTPMailer{Host: "smtp.example.com", Port: "587", From: "calendar@example.com"}
	ics := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")

	headers, _, _, _ := parseInvitation(t, m, "bob@example.com", "", "", "Invitation: Standup", ics)

	if headers.Get("Reply-To") != "" {
		t.Fatalf("expected no Reply-To when the organizer is unknown, got %q", headers.Get("Reply-To"))
	}
}

func TestSendInvitation_PlainTextFallbackMentionsSubject(t *testing.T) {
	m := &SMTPMailer{Host: "smtp.example.com", Port: "587", From: "calendar@example.com"}
	ics := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")

	_, plainBody, _, _ := parseInvitation(t, m, "bob@example.com", "Alice Example", "alice@example.com", "Invitation: Standup", ics)

	if !strings.Contains(plainBody, "Invitation: Standup") {
		t.Fatalf("expected the plain-text fallback to mention the subject, got:\n%s", plainBody)
	}
}

// parseCancellation is parseInvitation's METHOD:CANCEL counterpart (#201):
// it calls buildCancellationMessage, the pure builder SendCancellation
// itself calls before dialing SMTP, same reason parseInvitation avoids
// dialing SMTP for SendInvitation.
func parseCancellation(t *testing.T, m *SMTPMailer, to, fromName, replyTo, subject string, ics []byte) (headers mail.Header, plainBody string, calBody []byte, calHeader map[string][]string) {
	t.Helper()

	raw, err := buildCancellationMessage(m, to, fromName, replyTo, subject, ics)
	if err != nil {
		t.Fatalf("build cancellation message: %v", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("expected a multipart body, got %q", mediaType)
	}

	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(part); err != nil {
			t.Fatalf("read part: %v", err)
		}

		ct := part.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "text/plain"):
			plainBody = body.String()
		case strings.HasPrefix(ct, "text/calendar"):
			decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body.String(), "\r\n", ""))
			if err != nil {
				t.Fatalf("decode calendar part: %v", err)
			}
			calBody = decoded
			calHeader = map[string][]string(part.Header)
		}
	}

	return msg.Header, plainBody, calBody, calHeader
}

func TestSendCancellation_CalendarPartCarriesMethodCancelAndOriginalBytes(t *testing.T) {
	m := &SMTPMailer{Host: "smtp.example.com", Port: "587", From: "calendar@example.com"}
	ics := []byte("BEGIN:VCALENDAR\r\nMETHOD:CANCEL\r\nEND:VCALENDAR\r\n")

	_, _, calBody, calHeader := parseCancellation(t, m, "bob@example.com", "Alice Example", "alice@example.com", "Cancelled: Standup", ics)

	if !bytes.Equal(calBody, ics) {
		t.Fatalf("expected the calendar part to round-trip the original ics bytes, got:\n%s", calBody)
	}
	ct := strings.Join(calHeader["Content-Type"], "")
	if !strings.Contains(ct, "text/calendar") || !strings.Contains(ct, "method=CANCEL") {
		t.Fatalf("expected text/calendar;method=CANCEL, got %q", ct)
	}
}

func TestSendCancellation_FromStaysTheInstanceMailbox_ReplyToIsTheOrganizer(t *testing.T) {
	m := &SMTPMailer{Host: "smtp.example.com", Port: "587", From: "calendar@example.com"}
	ics := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")

	headers, _, _, _ := parseCancellation(t, m, "bob@example.com", "Alice Example", "alice@example.com", "Cancelled: Standup", ics)

	from, err := headers.AddressList("From")
	if err != nil || len(from) != 1 {
		t.Fatalf("parse From: %v (%+v)", err, from)
	}
	if from[0].Address != "calendar@example.com" {
		t.Fatalf("expected From to stay the instance mailbox, got %q", from[0].Address)
	}

	replyTo, err := headers.AddressList("Reply-To")
	if err != nil || len(replyTo) != 1 {
		t.Fatalf("parse Reply-To: %v (%+v)", err, replyTo)
	}
	if replyTo[0].Address != "alice@example.com" {
		t.Fatalf("expected Reply-To to be the organizer's own address, got %q", replyTo[0].Address)
	}
}

func TestSendCancellation_PlainTextFallbackMentionsSubject(t *testing.T) {
	m := &SMTPMailer{Host: "smtp.example.com", Port: "587", From: "calendar@example.com"}
	ics := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")

	_, plainBody, _, _ := parseCancellation(t, m, "bob@example.com", "Alice Example", "alice@example.com", "Cancelled: Standup", ics)

	if !strings.Contains(plainBody, "Cancelled: Standup") {
		t.Fatalf("expected the plain-text fallback to mention the subject, got:\n%s", plainBody)
	}
}
