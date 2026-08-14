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
	cal, _, err := SeriesToICal(master, nil, target)
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

	cal, _, err := SeriesToICal(master, nil, target)
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
	cal, _, err := SeriesToICal(master, []repository.Event{override}, target)
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

	cal, _, err := SeriesToICal(master, nil, CalDAVTarget("/dav/attachments/"))
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

	cal, _, err := SeriesToICal(master, nil, CalDAVTarget("/dav/attachments/"))
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

	cal, _, err := SeriesToICal(master, nil, SerializationTarget{})
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

func TestOccurrenceToICal_CalendarFileTarget_InlinesAttachmentBytes(t *testing.T) {
	// The flattened Occurrence a service.GetOccurrence hands the codec: no
	// rule, concrete start/end, carrying its series' Attachments (ADR-0040).
	occurrence := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 9, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attachments: []repository.Attachment{
			{ID: "att-1", Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 11},
		},
	}

	target := CalendarFileTarget(1<<20, staticOpener("att-1", "hello world"))
	cal, omitted, err := OccurrenceToICal("fresh-uid", occurrence, target)
	if err != nil {
		t.Fatalf("OccurrenceToICal: %v", err)
	}
	if len(omitted) != 0 {
		t.Fatalf("expected nothing omitted for an under-cap attachment, got %+v", omitted)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, "ENCODING=BASE64") || !strings.Contains(got, "VALUE=BINARY") {
		t.Fatalf("expected an inline ATTACH on the occurrence, got:\n%s", got)
	}
	// base64("hello world") = "aGVsbG8gd29ybGQ="
	if !strings.Contains(got, "aGVsbG8gd29ybGQ=") {
		t.Fatalf("expected the base64-encoded bytes, got:\n%s", got)
	}
	if strings.Contains(got, "MANAGED-ID") {
		t.Fatalf("expected no managed-attachment reference in a Calendar file, got:\n%s", got)
	}
}

func TestOccurrenceToICal_CalendarFileTarget_ReportsOversizedOmission(t *testing.T) {
	occurrence := repository.Event{
		ID:        "evt-1",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 9, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Attachments: []repository.Attachment{
			{ID: "att-1", Filename: "huge.bin", ContentType: "application/octet-stream", SizeBytes: 100},
		},
	}

	cal, omitted, err := OccurrenceToICal("fresh-uid", occurrence, CalendarFileTarget(10, func(string) (io.ReadCloser, error) {
		t.Fatal("open should not be called for an attachment over the cap")
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("OccurrenceToICal: %v", err)
	}
	body, err := Encode(cal)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(string(body), "ATTACH") {
		t.Fatalf("expected the oversized attachment omitted, got:\n%s", body)
	}

	if len(omitted) != 1 {
		t.Fatalf("expected exactly one reported omission, got %+v", omitted)
	}
	want := OmittedAttachment{
		Filename:   "huge.bin",
		SizeBytes:  100,
		EventID:    "evt-1",
		EventTitle: "Standup",
		Reason:     OmittedOverInlineCap,
	}
	if omitted[0] != want {
		t.Fatalf("omission: got %+v, want %+v", omitted[0], want)
	}
}

func TestSeriesToICal_ReportsEachOversizedAttachmentOnce(t *testing.T) {
	// An Attachment belongs to the series, so appendSeriesVEvents renders it
	// on the Master's VEVENT and on every Override's. The omission is still
	// one omission — the file lost one file, not one per VEVENT.
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
			{ID: "att-1", Filename: "huge.bin", SizeBytes: 100},
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

	_, omitted, err := SeriesToICal(master, []repository.Event{override}, CalendarFileTarget(10, staticOpener("att-1", "x")))
	if err != nil {
		t.Fatalf("SeriesToICal: %v", err)
	}
	if len(omitted) != 1 {
		t.Fatalf("expected one omission for one oversized Attachment, got %+v", omitted)
	}
	if omitted[0].EventID != master.ID || omitted[0].EventTitle != master.Title {
		t.Fatalf("expected the omission attributed to the Master it hangs off, got %+v", omitted[0])
	}
}

func TestCalendarToICal_ReportsOmissionsFromEverySeries(t *testing.T) {
	masterFor := func(id, title, attachmentID, filename string) repository.Event {
		return repository.Event{
			ID:        id,
			Title:     title,
			Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
			End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
			CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Attachments: []repository.Attachment{
				{ID: attachmentID, Filename: filename, SizeBytes: 100},
			},
		}
	}
	masters := []repository.Event{
		masterFor("evt-1", "Standup", "att-1", "one.bin"),
		masterFor("evt-2", "Retro", "att-2", "two.bin"),
	}

	_, omitted, err := CalendarToICal("Work", "", masters, nil, CalendarFileTarget(10, staticOpener("none", "")))
	if err != nil {
		t.Fatalf("CalendarToICal: %v", err)
	}
	if len(omitted) != 2 {
		t.Fatalf("expected one omission per series, got %+v", omitted)
	}
	if omitted[0].EventID != "evt-1" || omitted[1].EventID != "evt-2" {
		t.Fatalf("expected omissions in the same series order the file is written in, got %+v", omitted)
	}
}
