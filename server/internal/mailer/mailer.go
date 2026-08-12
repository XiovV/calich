// Package mailer sends Email-Channel Reminders (ADR-0021) and Invitations
// (ADR-0059) over SMTP.
package mailer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/smtp"
	"net/textproto"
)

// SMTPMailer sends mail through a configured SMTP transport.
type SMTPMailer struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

func NewSMTPMailer(host, port, user, pass, from string) *SMTPMailer {
	return &SMTPMailer{Host: host, Port: port, User: user, Pass: pass, From: from}
}

// Send delivers a plain-text email to a single recipient.
func (m *SMTPMailer) Send(to, subject, body string) error {
	addr := m.Host + ":" + m.Port
	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	msg := fmt.Appendf(nil, "From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n", m.From, to, subject, body)

	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}

// base64LineLength is RFC 2045's line-length cap on a base64-encoded MIME
// body part.
const base64LineLength = 76

// SendInvitation delivers an Invitation (ADR-0059): a multipart/alternative
// message — a plain-text fallback plus a text/calendar;method=REQUEST part —
// which is what makes Gmail, Outlook and Apple Mail render an invite card
// with their own Accept/Decline/Tentative rather than showing a plain
// attachment. The message's From stays m.From (this instance's own
// mailbox), carrying fromName as its display name; replyTo, when set, is
// the Organizer's own address — where a human reply actually reaches them,
// since this instance cannot read their mailbox (ADR-0059's ORGANIZER
// split).
func (m *SMTPMailer) SendInvitation(to, fromName, replyTo, subject string, ics []byte) error {
	msg, err := buildInvitationMessage(m, to, fromName, replyTo, subject, ics)
	if err != nil {
		return err
	}

	addr := m.Host + ":" + m.Port
	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}

// SendCancellation delivers a Cancellation (ADR-0059, #201) — a
// METHOD:CANCEL counterpart to SendInvitation, same message shape and same
// From/Reply-To split, so a mail client withdraws the calendar entry it
// rendered from the original Invitation rather than showing a second,
// unrelated attachment.
func (m *SMTPMailer) SendCancellation(to, fromName, replyTo, subject string, ics []byte) error {
	msg, err := buildCancellationMessage(m, to, fromName, replyTo, subject, ics)
	if err != nil {
		return err
	}

	addr := m.Host + ":" + m.Port
	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}

// buildInvitationMessage renders SendInvitation's raw message — headers plus
// a multipart/alternative body — split out as its own pure function so it
// can be tested without dialing SMTP.
func buildInvitationMessage(m *SMTPMailer, to, fromName, replyTo, subject string, ics []byte) ([]byte, error) {
	return buildCalendarMessage(m, to, fromName, replyTo, subject, ics, "REQUEST", "invite.ics",
		"This message contains a calendar invitation. Open it in a calendar app to accept, decline, or reply tentative.")
}

// buildCancellationMessage is buildInvitationMessage's METHOD:CANCEL
// counterpart, split out the same way for the same reason (#201).
func buildCancellationMessage(m *SMTPMailer, to, fromName, replyTo, subject string, ics []byte) ([]byte, error) {
	return buildCalendarMessage(m, to, fromName, replyTo, subject, ics, "CANCEL", "cancel.ics",
		"This message contains a calendar cancellation notice. Open it in a calendar app to remove the event.")
}

// buildCalendarMessage is buildInvitationMessage's and
// buildCancellationMessage's shared builder — headers plus a
// multipart/alternative body carrying ics as a text/calendar;method=<method>
// part, differing only in that method, the calendar part's filename, and the
// plain-text fallback's wording.
func buildCalendarMessage(m *SMTPMailer, to, fromName, replyTo, subject string, ics []byte, method, filename, plainText string) ([]byte, error) {
	var buf bytes.Buffer

	from := mail.Address{Name: fromName, Address: m.From}
	buf.WriteString("From: " + from.String() + "\r\n")
	buf.WriteString("To: " + (&mail.Address{Address: to}).String() + "\r\n")
	if replyTo != "" {
		buf.WriteString("Reply-To: " + (&mail.Address{Address: replyTo}).String() + "\r\n")
	}
	buf.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	mw := multipart.NewWriter(&buf)
	buf.WriteString("Content-Type: multipart/alternative; boundary=" + mw.Boundary() + "\r\n\r\n")

	plainPart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/plain; charset=UTF-8"},
	})
	if err != nil {
		return nil, fmt.Errorf("create plain-text part: %w", err)
	}
	if _, err := fmt.Fprintf(plainPart, "%s\r\n\r\n%s\r\n", subject, plainText); err != nil {
		return nil, fmt.Errorf("write plain-text part: %w", err)
	}

	calPart, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {`text/calendar; method=` + method + `; charset=UTF-8`},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {`inline; filename="` + filename + `"`},
	})
	if err != nil {
		return nil, fmt.Errorf("create calendar part: %w", err)
	}
	if _, err := calPart.Write(base64Wrapped(ics)); err != nil {
		return nil, fmt.Errorf("write calendar part: %w", err)
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	return buf.Bytes(), nil
}

// base64Wrapped base64-encodes data and hard-wraps it at base64LineLength
// with CRLF, the line-folding RFC 2045 requires for a base64 MIME body.
func base64Wrapped(data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(data)

	var out bytes.Buffer
	for i := 0; i < len(encoded); i += base64LineLength {
		end := min(i+base64LineLength, len(encoded))
		out.WriteString(encoded[i:end])
		out.WriteString("\r\n")
	}
	return out.Bytes()
}
