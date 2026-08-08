package icalendar

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// staticOpener is an AttachmentOpener test double: id resolves to content,
// any other id errors — mirroring an attachmentstore.Store miss.
func staticOpener(id, content string) AttachmentOpener {
	return func(gotID string) (io.ReadCloser, error) {
		if gotID != id {
			return nil, errors.New("no such attachment")
		}
		return io.NopCloser(bytes.NewReader([]byte(content))), nil
	}
}

func TestSeriesToICal_CalendarFileTarget_InlinesAttachmentBytes(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attachments: []repository.Attachment{
			{ID: "att-1", Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 11},
		},
	}

	target := CalendarFileTarget(1<<20, staticOpener("att-1", "hello world"))
	cal, err := SeriesToICal(master, nil, target)
	if err != nil {
		t.Fatalf("SeriesToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, "ENCODING=BASE64") || !strings.Contains(got, "VALUE=BINARY") {
		t.Fatalf("expected an inline ATTACH (ENCODING=BASE64;VALUE=BINARY), got:\n%s", got)
	}
	if !strings.Contains(got, "FMTTYPE=text/plain") {
		t.Fatalf("expected FMTTYPE=text/plain, got:\n%s", got)
	}
	if !strings.Contains(got, "FILENAME=notes.txt") {
		t.Fatalf("expected FILENAME=notes.txt, got:\n%s", got)
	}
	if strings.Contains(got, "MANAGED-ID") {
		t.Fatalf("expected no MANAGED-ID on an inline ATTACH (that's the CalDAV shape), got:\n%s", got)
	}
	// base64("hello world") = "aGVsbG8gd29ybGQ="
	if !strings.Contains(got, "aGVsbG8gd29ybGQ=") {
		t.Fatalf("expected the base64-encoded bytes, got:\n%s", got)
	}
}

func TestSeriesToICal_CalendarFileTarget_OmitsAttachmentOverTheCap(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attachments: []repository.Attachment{
			{ID: "att-1", Filename: "huge.bin", ContentType: "application/octet-stream", SizeBytes: 100},
		},
	}

	// The opener would error if called — proving the cap check never reads
	// the oversized attachment's bytes.
	target := CalendarFileTarget(10, func(string) (io.ReadCloser, error) {
		t.Fatal("open should not be called for an attachment over the cap")
		return nil, nil
	})

	cal, err := SeriesToICal(master, nil, target)
	if err != nil {
		t.Fatalf("SeriesToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := string(body)

	if strings.Contains(got, "ATTACH") {
		t.Fatalf("expected the oversized attachment to be omitted, not inlined, got:\n%s", got)
	}
}

func TestSeriesToICal_CalendarFileTarget_InlinesOnOverrideToo(t *testing.T) {
	recurrenceID := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	parentID := "evt-1"
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Rrule:     "FREQ=WEEKLY;BYDAY=TU",
		Start:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attachments: []repository.Attachment{
			{ID: "att-1", Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 5},
		},
	}
	override := repository.Event{
		ID:           "evt-1-override",
		ParentID:     &parentID,
		RecurrenceID: &recurrenceID,
		Title:        "Standup (moved)",
		Start:        time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 6, 9, 11, 30, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
	}

	target := CalendarFileTarget(1<<20, staticOpener("att-1", "hello"))
	cal, err := SeriesToICal(master, []repository.Event{override}, target)
	if err != nil {
		t.Fatalf("SeriesToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := string(body)

	if strings.Count(got, "ATTACH") != 2 {
		t.Fatalf("expected an inline ATTACH on both the master and the override VEVENT, got:\n%s", got)
	}
}

func TestSeriesToICal_CalDAVTarget_UnaffectedByInlineAmendment(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attachments: []repository.Attachment{
			{ID: "att-1", Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 11},
		},
	}

	cal, err := SeriesToICal(master, nil, CalDAVTarget("/dav/attachments/"))
	if err != nil {
		t.Fatalf("SeriesToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, "MANAGED-ID=att-1") {
		t.Fatalf("expected the RFC 8607 managed-attachment reference unchanged, got:\n%s", got)
	}
	if strings.Contains(got, "ENCODING=BASE64") {
		t.Fatalf("expected no inline bytes over CalDAV, got:\n%s", got)
	}
}

// TestSeriesToICal_CalDAVTarget_ETagPinned pins a fixed series' (no
// Attachments) CalDAV ETag to a literal hash: ADR-0041 explicitly promises
// "CalDAV output is unchanged by this ADR — no ETags churn", unlike
// ADR-0031's own VTIMEZONE rollout, which changed every ETag once on
// purpose. If this test ever needs its literal updated, that is this
// promise breaking and should be treated as a bug, not a rebase.
func TestSeriesToICal_CalDAVTarget_ETagPinned(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}

	cal, err := SeriesToICal(master, nil, CalDAVTarget("/dav/attachments/"))
	if err != nil {
		t.Fatalf("SeriesToICal: %v", err)
	}
	etag, err := CalendarETag(cal)
	if err != nil {
		t.Fatalf("CalendarETag: %v", err)
	}

	const wantETag = "e0eeb8bd28478e5cd2bbf3371973a746f3f017ad9b63453f764d2fa19696da82"
	if etag != wantETag {
		t.Fatalf("ETag changed: got %s, want %s (ADR-0041 promises CalDAV output is unchanged)", etag, wantETag)
	}
}

func TestZeroSerializationTarget_NoATTACH(t *testing.T) {
	master := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attachments: []repository.Attachment{
			{ID: "att-1", Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 11},
		},
	}

	cal, err := SeriesToICal(master, nil, SerializationTarget{})
	if err != nil {
		t.Fatalf("SeriesToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(body), "ATTACH") {
		t.Fatalf("expected no ATTACH for the zero SerializationTarget, got:\n%s", body)
	}
}
