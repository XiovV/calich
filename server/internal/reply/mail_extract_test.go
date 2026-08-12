package reply

import (
	"encoding/base64"
	"strings"
	"testing"
)

const sampleICS = "BEGIN:VCALENDAR\r\nMETHOD:REPLY\r\nBEGIN:VEVENT\r\nUID:evt-1\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestExtractCalendarPart_TopLevelTextCalendar(t *testing.T) {
	raw := "From: guest@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Subject: Accepted\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		sampleICS

	data, ok, err := ExtractCalendarPart([]byte(raw))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if string(data) != sampleICS {
		t.Fatalf("expected calendar bytes to round-trip, got %q", data)
	}
}

func TestExtractCalendarPart_MultipartMixed_Base64Attachment(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(sampleICS))
	raw := "From: guest@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Subject: Accepted\r\n" +
		"Content-Type: multipart/mixed; boundary=BOUNDARY1\r\n" +
		"\r\n" +
		"--BOUNDARY1\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		"Your response has been sent.\r\n" +
		"--BOUNDARY1\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"Content-Disposition: attachment; filename=\"reply.ics\"\r\n" +
		"\r\n" +
		encoded + "\r\n" +
		"--BOUNDARY1--\r\n"

	data, ok, err := ExtractCalendarPart([]byte(raw))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if string(data) != sampleICS {
		t.Fatalf("expected decoded calendar bytes to round-trip, got %q", data)
	}
}

// TestExtractCalendarPart_NestedMultipart covers the shape Apple Mail and
// Gmail commonly produce: multipart/mixed wrapping a nested
// multipart/alternative (text/plain + text/html) alongside a separate
// text/calendar attachment — the calendar part is two levels deep.
func TestExtractCalendarPart_NestedMultipart(t *testing.T) {
	raw := "From: guest@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Content-Type: multipart/mixed; boundary=OUTER\r\n" +
		"\r\n" +
		"--OUTER\r\n" +
		"Content-Type: multipart/alternative; boundary=INNER\r\n" +
		"\r\n" +
		"--INNER\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Accepted.\r\n" +
		"--INNER\r\n" +
		"Content-Type: text/html\r\n" +
		"\r\n" +
		"<p>Accepted.</p>\r\n" +
		"--INNER--\r\n" +
		"--OUTER\r\n" +
		"Content-Type: text/calendar; method=REPLY\r\n" +
		"\r\n" +
		sampleICS +
		"--OUTER--\r\n"

	data, ok, err := ExtractCalendarPart([]byte(raw))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if strings.TrimRight(string(data), "\r\n") != strings.TrimRight(sampleICS, "\r\n") {
		t.Fatalf("expected calendar bytes to round-trip, got %q", data)
	}
}

func TestExtractCalendarPart_QuotedPrintable(t *testing.T) {
	// "=0D=0A" is a quoted-printable-encoded CRLF; the rest of sampleICS
	// contains no characters quoted-printable would otherwise escape.
	qp := strings.ReplaceAll(sampleICS, "\r\n", "=0D=0A")
	raw := "From: guest@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Content-Type: text/calendar; method=REPLY\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		qp

	data, ok, err := ExtractCalendarPart([]byte(raw))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if string(data) != sampleICS {
		t.Fatalf("expected decoded calendar bytes to round-trip, got %q", data)
	}
}

// TestExtractCalendarPart_OrdinaryMail covers "mail that is not a calendar
// reply is left alone": no text/calendar part anywhere, ok=false, no error.
func TestExtractCalendarPart_OrdinaryMail(t *testing.T) {
	raw := "From: someone@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Subject: Re: Standup\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		"Sounds good, see you there.\r\n"

	data, ok, err := ExtractCalendarPart([]byte(raw))
	if err != nil {
		t.Fatalf("expected no error for ordinary mail, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for ordinary mail, got %q", data)
	}
}

func TestExtractCalendarPart_MultipartWithNoCalendarPart(t *testing.T) {
	raw := "From: someone@example.com\r\n" +
		"To: calendar@example.com\r\n" +
		"Content-Type: multipart/alternative; boundary=BOUNDARY1\r\n" +
		"\r\n" +
		"--BOUNDARY1\r\n" +
		"Content-Type: text/plain\r\n" +
		"\r\n" +
		"Sounds good.\r\n" +
		"--BOUNDARY1\r\n" +
		"Content-Type: text/html\r\n" +
		"\r\n" +
		"<p>Sounds good.</p>\r\n" +
		"--BOUNDARY1--\r\n"

	_, ok, err := ExtractCalendarPart([]byte(raw))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false when no part is text/calendar")
	}
}

func TestExtractCalendarPart_MalformedMessage(t *testing.T) {
	_, _, err := ExtractCalendarPart([]byte("not a valid rfc822 message at all"))
	if err == nil {
		t.Fatalf("expected an error for an unparseable message")
	}
}
