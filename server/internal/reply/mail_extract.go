// Package reply drains inbound Attendee Responses (ADR-0059): a background
// poller, the same shape as the reminder Scheduler and the outbox Worker,
// that fetches unseen mail from the ORGANIZER mailbox (SMTP_FROM/IMAP_USER),
// parses each text/calendar METHOD:REPLY it finds, and writes the Response
// it names.
package reply

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// mimeHeader is the subset of net/mail.Header and net/textproto.MIMEHeader
// extractCalendarPart needs — satisfied by both, so it recurses into a
// multipart part's own textproto.MIMEHeader the same way it reads the
// top-level message's net/mail.Header.
type mimeHeader interface {
	Get(key string) string
}

// ExtractCalendarPart walks raw — a full RFC 822 message, whatever shape a
// mainstream mail client's Accept/Decline/Tentative button produced —
// looking for its text/calendar part (a top-level body, or nested inside
// one or more multipart/* parts, however deep). ok is false when raw simply
// isn't a calendar reply at all — ordinary mail carries no such part — which
// the caller leaves alone rather than treating as an error (#202, "mail
// that is not a calendar reply is left alone").
func ExtractCalendarPart(raw []byte) ([]byte, bool, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, false, fmt.Errorf("read message: %w", err)
	}
	return extractCalendarFromPart(msg.Header, msg.Body)
}

// extractCalendarFromPart is ExtractCalendarPart's recursive body: header
// names body's Content-Type, and body is read once — either decoded and
// returned (a text/calendar part) or handed to multipart.Reader to look
// for one further in.
func extractCalendarFromPart(header mimeHeader, body io.Reader) ([]byte, bool, error) {
	contentType := header.Get("Content-Type")
	if contentType == "" {
		return nil, false, nil
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// An unparseable Content-Type is not this app's calendar part —
		// left alone same as any other content this app doesn't recognize,
		// rather than failing the whole message over one malformed header.
		return nil, false, nil
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		return extractCalendarFromMultipart(body, params["boundary"])
	}

	if mediaType != "text/calendar" {
		return nil, false, nil
	}

	data, err := decodeBody(body, header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return nil, false, fmt.Errorf("decode calendar part: %w", err)
	}
	return data, true, nil
}

// extractCalendarFromMultipart walks a multipart/* body's parts in order,
// recursing into each (a multipart/alternative or an attachment can itself
// nest another multipart, e.g. Apple Mail's multipart/mixed wrapping a
// multipart/alternative plus a separate text/calendar attachment) and
// returning the first text/calendar part found.
func extractCalendarFromMultipart(body io.Reader, boundary string) ([]byte, bool, error) {
	if boundary == "" {
		return nil, false, fmt.Errorf("multipart body with no boundary")
	}

	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("read multipart part: %w", err)
		}

		data, ok, err := extractCalendarFromPart(part.Header, part)
		if err != nil {
			return nil, false, err
		}
		if ok {
			return data, true, nil
		}
	}
}

// decodeBody reverses whatever Content-Transfer-Encoding the sending
// client applied — base64 (the encoding this app's own mailer.SMTPMailer
// uses to send an Invitation, so its own re-sent REQUEST bounced back would
// take this path) or quoted-printable; any other value (or none) means the
// body is already the raw octets.
func decodeBody(r io.Reader, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, r))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(r))
	default:
		return io.ReadAll(r)
	}
}
