// import.go decomposes a whole foreign Calendar file into the series
// ImportSeries writes (ADR-0030). A Calendar file may hold many VEVENT
// series (unlike a single CalDAV calendar object, which ParseCalendarObject
// assumes carries exactly one UID), so this groups VEVENTs by UID first and
// classifies whatever ParseCalendarObject can't express — an unparseable
// group, a missing DTSTART, an orphan RECURRENCE-ID, a cancelled series — as
// skipped rather than failing the whole file. VTODO/VJOURNAL/VFREEBUSY
// components are counted as ignored without being decoded at all. Unlike
// ParseCalendarObject (shared with CalDAV PUT, which never touches
// Attachments per ADR-0040), this also decodes each series' inline ATTACH
// properties into its Master's Attachments, deduplicated across the raw
// master and Override VEVENTs, with a URI ATTACH counted but never fetched
// (#135, ADR-0040).
package icalendar

import (
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calich/server/internal/repository"
)

// Skip reasons an import summary reports (ADR-0030).
const (
	SkipUnparseable      = "unparseable"
	SkipMissingDTStart   = "missing DTSTART"
	SkipOrphanRecurrence = "orphan RECURRENCE-ID"
	SkipCancelled        = "cancelled"
)

// ImportedSeries is one series decoded from a foreign Calendar file, plus
// the counts of what import quietly altered within it: an unresolvable
// TZID degrading to a Floating Event, and an unsupported VALARM dropped
// (ADR-0030).
type ImportedSeries struct {
	ParsedSeries
	FloatingDowngrades int
	DroppedAlarms      int
	// Attachments are the inline ATTACH bytes decoded for this series'
	// Master, deduplicated across every VEVENT (master and Overrides alike)
	// that carried the same one — a file this app exports (#134) repeats the
	// same ATTACH on every VEVENT in the object, and re-importing it must not
	// produce one copy per Occurrence (#135, ADR-0040).
	Attachments []InlineAttachment
	// AttachmentsTooLarge, AttachmentsTooMany and AttachmentsIgnoredURI count
	// what this series' ATTACH properties held that import declined to
	// ingest — a distinct inline attachment over MAX_ATTACHMENT_SIZE, a
	// distinct inline attachment past MAX_ATTACHMENTS_PER_EVENT, and a
	// distinct URI ATTACH (never fetched, ADR-0040) respectively. Each is
	// counted once per distinct ATTACH value, not once per VEVENT it repeats
	// on.
	AttachmentsTooLarge, AttachmentsTooMany, AttachmentsIgnoredURI int
}

// InlineAttachment is one inline ATTACH decoded from a foreign VEVENT,
// ready to become an Attachment on the imported series' Master. FMTTYPE and
// FILENAME are used when present; plenty of real files carry neither, so
// Filename and ContentType already carry the sniffed/generated fallback by
// the time a caller sees this (#135).
type InlineAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// SkippedSeries is one VEVENT UID group import declined to write, and why.
// UID is the group's own iCalendar UID (the map key ParseImportFile grouped
// it under) — a Refresh needs it to tell "present in the feed but
// unparseable" apart from "absent from the feed" for the same ExternalUID
// (#85, ADR-0033); ordinary import ignores it.
type SkippedSeries struct {
	Reason string
	Title  string
	UID    string
}

// ParsedFile is one Calendar file's (or zip entry's) whole decoded content
// (ADR-0030).
type ParsedFile struct {
	// CalendarName and Color are read from X-WR-CALNAME/
	// X-APPLE-CALENDAR-COLOR when present, empty otherwise.
	CalendarName string
	Color        string
	// RefreshInterval is the publisher's own stated poll cadence, read from
	// REFRESH-INTERVAL (RFC 7986) or, lacking that, X-PUBLISHED-TTL — nil
	// when the feed states neither or states one unparseably (#86,
	// ADR-0033). A poller honours this only when it is longer than its own
	// default; it never polls faster than asked.
	RefreshInterval *time.Duration
	Series          []ImportedSeries
	Skipped         []SkippedSeries
	// IgnoredVTodo/VJournal/VFreeBusy count components import doesn't model
	// at all, never converted into Series or Skipped entries.
	IgnoredVTodo, IgnoredVJournal, IgnoredVFreeBusy int
}

// ParseImportFile decodes r as an iCalendar document and classifies its
// contents for import. maxAttachmentSize and maxAttachmentsPerEvent are
// MAX_ATTACHMENT_SIZE/MAX_ATTACHMENTS_PER_EVENT (ADR-0040), enforced per
// series exactly as a live upload would be (#135).
func ParseImportFile(r io.Reader, maxAttachmentSize int64, maxAttachmentsPerEvent int) (*ParsedFile, error) {
	cal, err := ical.NewDecoder(r).Decode()
	if err != nil {
		return nil, fmt.Errorf("decode calendar: %w", err)
	}

	out := &ParsedFile{}
	if p := cal.Props.Get(propWRCalName); p != nil {
		out.CalendarName = p.Value
	}
	if p := cal.Props.Get(propAppleCalendarColor); p != nil {
		out.Color = p.Value
	}
	if p := cal.Props.Get(propRefreshInterval); p != nil {
		if d, err := p.Duration(); err == nil {
			out.RefreshInterval = &d
		}
	} else if p := cal.Props.Get(propPublishedTTL); p != nil {
		if d, err := p.Duration(); err == nil {
			out.RefreshInterval = &d
		}
	}

	var order []string
	groups := make(map[string][]ical.Event)
	for _, child := range cal.Children {
		switch child.Name {
		case ical.CompEvent:
			v := ical.Event{Component: child}
			uid, _ := v.Props.Text(ical.PropUID)
			if _, ok := groups[uid]; !ok {
				order = append(order, uid)
			}
			groups[uid] = append(groups[uid], v)
		case ical.CompToDo:
			out.IgnoredVTodo++
		case ical.CompJournal:
			out.IgnoredVJournal++
		case ical.CompFreeBusy:
			out.IgnoredVFreeBusy++
		}
	}

	for _, uid := range order {
		series, skipped := parseSeriesGroup(groups[uid], maxAttachmentSize, maxAttachmentsPerEvent)
		if skipped != nil {
			skipped.UID = uid
			out.Skipped = append(out.Skipped, *skipped)
			continue
		}
		out.Series = append(out.Series, *series)
	}

	return out, nil
}

// sampleTitle picks a best-effort title for a skipped group's sample list:
// the first VEVENT's SUMMARY, or "" if it has none.
func sampleTitle(events []ical.Event) string {
	if len(events) == 0 {
		return ""
	}
	title, _ := events[0].Props.Text(ical.PropSummary)
	return title
}

// parseSeriesGroup classifies and decodes one UID's worth of VEVENTs. The
// checks that precede ParseCalendarObject give a precise skip reason for the
// cases ADR-0030 calls out by name; ParseCalendarObject's own error (e.g. a
// second master, an unparseable date) folds into the generic "unparseable"
// reason since the codec doesn't distinguish them further.
func parseSeriesGroup(events []ical.Event, maxAttachmentSize int64, maxAttachmentsPerEvent int) (*ImportedSeries, *SkippedSeries) {
	var master *ical.Event
	multipleMasters := false
	for i := range events {
		if events[i].Props.Get(ical.PropRecurrenceID) == nil {
			if master != nil {
				multipleMasters = true
				continue
			}
			master = &events[i]
		}
	}
	if multipleMasters {
		return nil, &SkippedSeries{Reason: SkipUnparseable, Title: sampleTitle(events)}
	}
	if master == nil {
		return nil, &SkippedSeries{Reason: SkipOrphanRecurrence, Title: sampleTitle(events)}
	}
	if master.Props.Get(ical.PropDateTimeStart) == nil {
		return nil, &SkippedSeries{Reason: SkipMissingDTStart, Title: sampleTitle(events)}
	}
	if status, _ := master.Props.Text(ical.PropStatus); status == "CANCELLED" {
		return nil, &SkippedSeries{Reason: SkipCancelled, Title: sampleTitle(events)}
	}

	// An unresolvable TZID must be stripped before ParseCalendarObject runs:
	// go-ical's own Prop.DateTime resolves TZID eagerly and errors out, so
	// downgrading to a Floating Event has to happen on the raw property, not
	// on ParseCalendarObject's result (ADR-0030).
	floatingDowngrades := 0
	for i := range events {
		if stripUnresolvableTzids(&events[i]) {
			floatingDowngrades++
		}
	}

	children := make([]*ical.Component, len(events))
	for i, v := range events {
		children[i] = v.Component
	}
	synthetic := &ical.Calendar{Component: &ical.Component{Name: ical.CompCalendar, Children: children}}

	parsed, err := ParseCalendarObject(synthetic)
	if err != nil {
		return nil, &SkippedSeries{Reason: SkipUnparseable, Title: sampleTitle(events)}
	}

	imported := ImportedSeries{ParsedSeries: *parsed, FloatingDowngrades: floatingDowngrades}
	imported.DroppedAlarms = droppedAlarms(master, imported.Master.Reminders)
	imported.Attachments, imported.AttachmentsTooLarge, imported.AttachmentsTooMany, imported.AttachmentsIgnoredURI =
		parseSeriesAttachments(events, maxAttachmentSize, maxAttachmentsPerEvent)

	var rawOverrides []*ical.Event
	for i := range events {
		if events[i].Props.Get(ical.PropRecurrenceID) != nil {
			rawOverrides = append(rawOverrides, &events[i])
		}
	}
	for i := range imported.Overrides {
		if i < len(rawOverrides) {
			imported.DroppedAlarms += droppedAlarms(rawOverrides[i], imported.Overrides[i].Reminders)
		}
	}

	return &imported, nil
}

// stripUnresolvableTzids deletes the TZID param from v's DTSTART, DTEND,
// RECURRENCE-ID, and EXDATE properties wherever it names a zone Go cannot
// resolve, so parseEventTime falls through to its Floating Event case
// instead of erroring — turning the Event into a Floating Event rather than
// skipping it, since Go only recognizes IANA names and the non-IANA ones
// Outlook and older clients emit are common in the wild (ADR-0030). Reports
// whether anything was stripped.
func stripUnresolvableTzids(v *ical.Event) bool {
	stripped := false
	for _, name := range []string{ical.PropDateTimeStart, ical.PropDateTimeEnd, ical.PropRecurrenceID} {
		if prop := v.Props.Get(name); prop != nil && stripIfUnresolvable(prop) {
			stripped = true
		}
	}
	exdates := v.Props.Values(ical.PropExceptionDates)
	for i := range exdates {
		if stripIfUnresolvable(&exdates[i]) {
			stripped = true
		}
	}
	return stripped
}

// stripIfUnresolvable deletes prop's TZID param and reports true if it names
// a zone time.LoadLocation can't resolve; a no-op reporting false otherwise.
func stripIfUnresolvable(prop *ical.Prop) bool {
	tzid := prop.Params.Get(ical.PropTimezoneID)
	if tzid == "" {
		return false
	}
	if _, err := time.LoadLocation(tzid); err == nil {
		return false
	}
	prop.Params.Del(ical.PropTimezoneID)
	return true
}

// droppedAlarms counts v's DISPLAY/EMAIL VALARMs that didn't survive into
// reminders — an unsupported TRIGGER shape (RELATED=END, an absolute
// datetime, or a missing one), since parseVAlarms silently drops those
// rather than rejecting the VEVENT (ADR-0026). This recounts the raw VALARM
// children rather than changing parseVAlarms' signature, so the shared
// CalDAV decode path is untouched.
func droppedAlarms(v *ical.Event, reminders []repository.Reminder) int {
	total := 0
	for _, child := range v.Children {
		if child.Name != ical.CompAlarm {
			continue
		}
		if action, _ := child.Props.Text(ical.PropAction); action == "DISPLAY" || action == "EMAIL" {
			total++
		}
	}
	if total <= len(reminders) {
		return 0
	}
	return total - len(reminders)
}

// parseSeriesAttachments decodes every ATTACH property across events (one
// series' raw master and Override VEVENTs) into the Attachments its Master
// will carry, deduplicated by content across the whole series — a file this
// app exports (#134) puts the same ATTACH on every VEVENT in the object, and
// re-importing it must produce one Attachment, not one per VEVENT (ADR-0040).
// A URI ATTACH is never fetched (a request-forgery surface) and never stored
// — only counted. Each distinct value is counted at most once, in whichever
// one of the three "declined" buckets applies, or accepted, giving the four
// outcomes the Import summary reports (#135).
func parseSeriesAttachments(events []ical.Event, maxAttachmentSize int64, maxAttachmentsPerEvent int) (accepted []InlineAttachment, tooLarge, tooMany, ignoredURI int) {
	seenInline := make(map[[sha256.Size]byte]bool)
	seenURI := make(map[string]bool)

	for _, v := range events {
		for _, prop := range v.Props.Values(ical.PropAttach) {
			p := prop
			if p.ValueType() != ical.ValueBinary {
				if !seenURI[p.Value] {
					seenURI[p.Value] = true
					ignoredURI++
				}
				continue
			}

			data, err := p.Binary()
			if err != nil {
				continue
			}
			// The dedup key folds in the raw FMTTYPE/FILENAME params (before
			// either falls back) alongside the content hash, not the hash
			// alone — two independently-attached files that happen to share
			// identical bytes but carry different names/types are distinct
			// Attachments, not the "same ATTACH repeated on every VEVENT"
			// case #135 exists to collapse.
			rawContentType := p.Params.Get(attachFmtTypeParam)
			rawFilename := p.Params.Get(attachFilenameParam)
			key := sha256.Sum256([]byte(rawContentType + "\x00" + rawFilename + "\x00" + string(data)))
			if seenInline[key] {
				continue
			}
			seenInline[key] = true

			if int64(len(data)) > maxAttachmentSize {
				tooLarge++
				continue
			}
			if len(accepted) >= maxAttachmentsPerEvent {
				tooMany++
				continue
			}

			contentType := rawContentType
			if contentType == "" {
				contentType = http.DetectContentType(data)
			}
			filename := rawFilename
			if filename == "" {
				filename = fallbackAttachmentFilename(contentType, len(accepted)+1)
			}

			accepted = append(accepted, InlineAttachment{
				Filename:    filename,
				ContentType: contentType,
				Data:        data,
			})
		}
	}

	return accepted, tooLarge, tooMany, ignoredURI
}

// fallbackAttachmentFilename names an ATTACH with no FILENAME param
// "attachment-<n>", with a best-guess extension from contentType when mime
// knows one — n is the attachment's 1-based position among its series'
// accepted Attachments, so two nameless attachments on the same series don't
// collide.
func fallbackAttachmentFilename(contentType string, n int) string {
	name := fmt.Sprintf("attachment-%d", n)
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		sort.Strings(exts)
		name += exts[0]
	}
	return name
}
