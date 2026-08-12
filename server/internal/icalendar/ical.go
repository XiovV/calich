// ical.go recomposes a series (a Master plus its Overrides and Exceptions)
// into the single VCALENDAR CalDAV expects for one calendar object
// (ADR-0025), and derives that object's ETag from the reconstruction rather
// than any raw stored bytes (ADR-0026, so PUT/GET ETags never mismatch and
// cause a re-sync loop).
package icalendar

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/recurrence"
	"github.com/XiovV/calendar/server/internal/repository"
)

// ProdID is the PRODID emitted on every VCALENDAR this app produces,
// including the ICS import/export files (ADR-0031).
const ProdID = "-//calendar//caldavserver//EN"

// allDayReminderAnchorOffset is how far after midnight (an all-day Event's
// stored start) a Reminder's offset is actually measured from — 09:00, so a
// Reminder never fires in the middle of the night (ADR-0020).
const allDayReminderAnchorOffset = 9 * time.Hour

// AttachmentOpener opens attachmentID's stored bytes so a Calendar file
// target (CalendarFileTarget) can inline them. Supplied by the caller — the
// service/handlers layer, which owns the attachmentstore — so this package
// stays free of a dependency on the filesystem attachment store.
type AttachmentOpener func(attachmentID string) (io.ReadCloser, error)

// SerializationTarget is how ATTACH is rendered — the one property that
// differs between a Calendar object and a Calendar file (ADR-0041).
// Everything else the codec emits comes from the same unconditional path
// regardless of target. The zero value renders no ATTACH at all
// (OccurrenceToICal's flattened single Occurrence never carries one by
// construction and so never constructs a SerializationTarget).
type SerializationTarget struct {
	caldavURIPrefix string
	inline          *inlineAttachments
}

// inlineAttachments holds a CalendarFileTarget's inline ceiling and how to
// read an Attachment's bytes.
type inlineAttachments struct {
	maxBytes int64
	open     AttachmentOpener
}

// CalDAVTarget renders ATTACH as an RFC 8607 managed-attachment reference —
// MANAGED-ID plus a URI built from uriPrefix (e.g. "/dav/attachments/") —
// the shape a Calendar object synced from this server carries.
func CalDAVTarget(uriPrefix string) SerializationTarget {
	return SerializationTarget{caldavURIPrefix: uriPrefix}
}

// CalendarFileTarget renders ATTACH with the bytes inlined
// (ENCODING=BASE64;VALUE=BINARY), read via open. An Attachment over
// maxInlineBytes is omitted rather than inlined or truncated (ADR-0041) —
// the same cap the ICS importer enforces on upload, so a file this instance
// produces is a file it could accept back. The shape a standalone Calendar
// file, handed to the world outside this instance, carries.
func CalendarFileTarget(maxInlineBytes int64, open AttachmentOpener) SerializationTarget {
	return SerializationTarget{inline: &inlineAttachments{maxBytes: maxInlineBytes, open: open}}
}

// SeriesToICal recomposes master and its overrides into one VCALENDAR: a
// VTIMEZONE for each distinct TZID the series references, then the Master
// VEVENT (carrying EXDATE for each cancelled Occurrence) followed by one
// VEVENT per Override ordered by RecurrenceID, all sharing master's id as
// their UID (ADR-0025). target picks how master.Attachments are rendered as
// ATTACH — see appendSeriesVEvents.
func SeriesToICal(master repository.Event, overrides []repository.Event, target SerializationTarget) (*ical.Calendar, error) {
	cal := newVCalendar()

	for _, tzid := range seriesTzids(master, overrides) {
		vtz, err := buildVTimezone(tzid)
		if err != nil {
			return nil, fmt.Errorf("build vtimezone %q: %w", tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	if err := appendSeriesVEvents(cal, master, overrides, target); err != nil {
		return nil, err
	}
	return cal, nil
}

// newVCalendar starts a VCALENDAR carrying this app's PRODID and VERSION —
// the two properties every VCALENDAR this package emits shares.
func newVCalendar() *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, ProdID)
	cal.Props.SetText(ical.PropVersion, "2.0")
	return cal
}

// appendSeriesVEvents appends master's VEVENT (with its EXDATEs) followed by
// one VEVENT per override (sorted by RecurrenceID) to cal.Children — the
// per-series VEVENT-building step SeriesToICal and CalendarToICal both need,
// the latter once per series in a Calendar. target's ATTACH rendering (see
// appendAttachProps) is added to every VEVENT emitted, master and every
// override alike — an Attachment belongs to the whole series, never to one
// Occurrence (ADR-0040).
func appendSeriesVEvents(cal *ical.Calendar, master repository.Event, overrides []repository.Event, target SerializationTarget) error {
	masterEvent, err := buildVEvent(master, master.ID, nil, nil)
	if err != nil {
		return fmt.Errorf("build master vevent: %w", err)
	}
	for _, exdate := range master.Exdates {
		prop, err := newDateTimeProp(ical.PropExceptionDates, exdate, master.AllDay, master.Tzid)
		if err != nil {
			return fmt.Errorf("build exdate: %w", err)
		}
		masterEvent.Props.Add(prop)
	}
	if err := appendAttachProps(masterEvent, master.Attachments, target); err != nil {
		return fmt.Errorf("append master attachments: %w", err)
	}
	cal.Children = append(cal.Children, masterEvent.Component)

	sorted := append([]repository.Event(nil), overrides...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].RecurrenceID.Before(*sorted[j].RecurrenceID)
	})
	for _, override := range sorted {
		overrideEvent, err := buildVEvent(override, master.ID, override.RecurrenceID, &master)
		if err != nil {
			return fmt.Errorf("build override vevent: %w", err)
		}
		if err := appendAttachProps(overrideEvent, master.Attachments, target); err != nil {
			return fmt.Errorf("append override attachments: %w", err)
		}
		cal.Children = append(cal.Children, overrideEvent.Component)
	}
	return nil
}

// attachManagedIDParam, attachFmtTypeParam, attachSizeParam and
// attachFilenameParam are RFC 8607's ATTACH parameters carrying a managed
// Attachment's identity and metadata alongside its URI value.
const (
	attachManagedIDParam = "MANAGED-ID"
	attachFmtTypeParam   = "FMTTYPE"
	attachSizeParam      = "SIZE"
	attachFilenameParam  = "FILENAME"
)

// appendAttachProps adds one ATTACH property per attachment onto v, rendered
// per target (ADR-0041):
//   - CalDAVTarget: the RFC 8607 managed-attachment URI (uriPrefix+id) as the
//     value, with MANAGED-ID/FMTTYPE/SIZE/FILENAME params (#133, ADR-0040).
//   - CalendarFileTarget: the Attachment's bytes inlined
//     (ENCODING=BASE64;VALUE=BINARY) with FMTTYPE/FILENAME params. An
//     Attachment over the target's inline cap is omitted, not truncated.
//   - The zero SerializationTarget (OccurrenceToICal): a no-op.
func appendAttachProps(v *ical.Event, attachments []repository.Attachment, target SerializationTarget) error {
	switch {
	case target.caldavURIPrefix != "":
		for _, a := range attachments {
			v.Props.Add(caldavAttachProp(a, target.caldavURIPrefix))
		}
	case target.inline != nil:
		for _, a := range attachments {
			if a.SizeBytes > target.inline.maxBytes {
				continue
			}
			prop, err := inlineAttachProp(a, target.inline.open)
			if err != nil {
				return fmt.Errorf("inline attachment %s: %w", a.ID, err)
			}
			v.Props.Add(prop)
		}
	}
	return nil
}

// caldavAttachProp builds one RFC 8607 managed-attachment reference ATTACH
// for a (#133, ADR-0040).
func caldavAttachProp(a repository.Attachment, uriPrefix string) *ical.Prop {
	prop := ical.NewProp(ical.PropAttach)
	prop.SetValueType(ical.ValueURI)
	prop.Value = uriPrefix + a.ID
	prop.Params.Set(attachManagedIDParam, a.ID)
	if a.ContentType != "" {
		prop.Params.Set(attachFmtTypeParam, a.ContentType)
	}
	prop.Params.Set(attachSizeParam, strconv.FormatInt(a.SizeBytes, 10))
	if a.Filename != "" {
		prop.Params.Set(attachFilenameParam, a.Filename)
	}
	return prop
}

// inlineAttachProp builds one inline ATTACH for a, reading its bytes via
// open (ADR-0041) — a Calendar file carries the bytes themselves since a
// managed-attachment reference means nothing outside this instance.
func inlineAttachProp(a repository.Attachment, open AttachmentOpener) (*ical.Prop, error) {
	r, err := open(a.ID)
	if err != nil {
		return nil, fmt.Errorf("open attachment: %w", err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}

	prop := ical.NewProp(ical.PropAttach)
	prop.SetValueType(ical.ValueBinary)
	prop.Params.Set("ENCODING", "BASE64")
	prop.Value = base64.StdEncoding.EncodeToString(data)
	if a.ContentType != "" {
		prop.Params.Set(attachFmtTypeParam, a.ContentType)
	}
	if a.Filename != "" {
		prop.Params.Set(attachFilenameParam, a.Filename)
	}
	return prop, nil
}

// seriesTzids returns the distinct, sorted TZIDs master and overrides
// anchor to (a Floating Event's nil Tzid contributes none), so SeriesToICal
// emits exactly one VTIMEZONE per zone actually referenced, in a
// deterministic order.
func seriesTzids(master repository.Event, overrides []repository.Event) []string {
	seen := make(map[string]struct{})
	if master.Tzid != nil {
		seen[*master.Tzid] = struct{}{}
	}
	for _, o := range overrides {
		if o.Tzid != nil {
			seen[*o.Tzid] = struct{}{}
		}
	}

	tzids := make([]string, 0, len(seen))
	for tzid := range seen {
		tzids = append(tzids, tzid)
	}
	sort.Strings(tzids)
	return tzids
}

// buildVEvent renders one Event row (a Master or an Override) as a VEVENT.
// recurrenceID is nil for a Master. recurrenceIDAnchor is the Master whose
// rrule generated recurrenceID — its AllDay/Tzid define how RECURRENCE-ID is
// formatted, since it must match the original Occurrence the rule produced,
// independent of anything the Override itself changed. e.Color, if set, is
// snapped to the nearest CSS3 keyword and written as COLOR (RFC 7986,
// ADR-0043); an inherited (nil) color emits no COLOR property at all.
func buildVEvent(e repository.Event, uid string, recurrenceID *time.Time, recurrenceIDAnchor *repository.Event) (*ical.Event, error) {
	v := ical.NewEvent()
	v.Props.SetText(ical.PropUID, uid)
	v.Props.SetDateTime(ical.PropDateTimeStamp, e.CreatedAt.UTC())
	v.Props.SetText(ical.PropSummary, e.Title)
	if e.Description != "" {
		v.Props.SetText(ical.PropDescription, e.Description)
	}
	if e.Location != "" {
		v.Props.SetText(ical.PropLocation, e.Location)
	}
	if e.Color != nil {
		r, g, b, err := parseHexRGB(*e.Color)
		if err != nil {
			return nil, fmt.Errorf("parse event color: %w", err)
		}
		v.Props.SetText(ical.PropColor, nearestCSS3Keyword(r, g, b))
	}

	startProp, err := newDateTimeProp(ical.PropDateTimeStart, e.Start, e.AllDay, e.Tzid)
	if err != nil {
		return nil, err
	}
	v.Props.Add(startProp)
	endProp, err := newDateTimeProp(ical.PropDateTimeEnd, e.End, e.AllDay, e.Tzid)
	if err != nil {
		return nil, err
	}
	v.Props.Add(endProp)

	if e.Rrule != "" {
		// Not SetText: it marks the prop VALUE=TEXT and backslash-escapes
		// ";", which would corrupt RRULE's own ";"-delimited syntax.
		rruleProp := ical.NewProp(ical.PropRecurrenceRule)
		rruleProp.Value = e.Rrule
		v.Props.Set(rruleProp)
	}
	if recurrenceID != nil {
		prop, err := newDateTimeProp(ical.PropRecurrenceID, *recurrenceID, recurrenceIDAnchor.AllDay, recurrenceIDAnchor.Tzid)
		if err != nil {
			return nil, fmt.Errorf("build recurrence-id: %w", err)
		}
		v.Props.Add(prop)
	}

	appendOrganizerProp(v, e)
	appendAttendeeProps(v, e.Attendees)

	for _, reminder := range e.Reminders {
		v.Children = append(v.Children, buildVAlarm(e, reminder))
	}

	return v, nil
}

// appendOrganizerProp adds ORGANIZER naming e's Organizer — the User named
// by CreatedBy — with CN=CreatedByName and that User's own Email as the
// mailto address, the CalDAV/Calendar-file form ADR-0059 defines (the
// Invitation's own ORGANIZER, naming this instance's address instead, is
// built separately). A no-op when CreatedBy is nil: an Event whose
// Organizer's account was since deleted simply has none (CONTEXT.md).
func appendOrganizerProp(v *ical.Event, e repository.Event) {
	if e.CreatedBy == nil {
		return
	}
	prop := ical.NewProp(ical.PropOrganizer)
	if e.CreatedByName != "" {
		prop.Params.Set(ical.ParamCommonName, e.CreatedByName)
	}
	prop.Value = "mailto:" + e.CreatedByEmail
	v.Props.Add(prop)
}

// appendAttendeeProps adds one ATTENDEE per attendees, naming their Response
// as PARTSTAT — attendees.response already stores iCalendar's PARTSTAT
// vocabulary verbatim (ADR-0046), upper-cased here since the column holds it
// lowercase (ADR-0062). No properties at all for an empty attendees, so an
// Event with no Attendees round-trips with no ATTENDEE lines.
func appendAttendeeProps(v *ical.Event, attendees []repository.AttendeeWithName) {
	for _, a := range attendees {
		prop := ical.NewProp(ical.PropAttendee)
		if a.Name != "" {
			prop.Params.Set(ical.ParamCommonName, a.Name)
		}
		prop.Params.Set(ical.ParamParticipationStatus, strings.ToUpper(a.Response))
		prop.Value = "mailto:" + a.Email
		v.Props.Add(prop)
	}
}

// buildVAlarm renders one Reminder as a VALARM: ACTION:DISPLAY for the
// notification Channel, ACTION:EMAIL for the email Channel, and a TRIGGER
// offset from DTSTART (ADR-0020). For an all-day Event the Reminder's offset
// is measured from 09:00 on the start date rather than midnight (DTSTART),
// so the trigger is shifted back by that same 9 hours to compensate.
func buildVAlarm(e repository.Event, reminder repository.Reminder) *ical.Component {
	alarm := ical.NewComponent(ical.CompAlarm)

	action := "DISPLAY"
	if reminder.Channel == "email" {
		action = "EMAIL"
	}
	alarm.Props.SetText(ical.PropAction, action)
	alarm.Props.SetText(ical.PropDescription, e.Title)
	if action == "EMAIL" {
		alarm.Props.SetText(ical.PropSummary, e.Title)
	}

	trigger := -time.Duration(reminder.OffsetMinutes) * time.Minute
	if e.AllDay {
		trigger += allDayReminderAnchorOffset
	}
	triggerProp := ical.NewProp(ical.PropTrigger)
	triggerProp.SetDuration(trigger)
	alarm.Props.Add(triggerProp)

	return alarm
}

// newDateTimeProp renders t as an all-day DATE, a zoned/absolute DATE-TIME
// (named tzid, or "Etc/UTC"), or a floating DATE-TIME (nil tzid) — the three
// DTSTART forms ADR-0019 defines. A Floating Event's instant is stored as a
// literal wall-clock (no real zone), so it is formatted as-is with no TZID
// param and no "Z" suffix, rather than converted through t's Go Location.
func newDateTimeProp(name string, t time.Time, allDay bool, tzid *string) (*ical.Prop, error) {
	prop := ical.NewProp(name)

	if allDay {
		prop.SetDate(t.UTC())
		return prop, nil
	}

	if tzid == nil {
		prop.SetValueType(ical.ValueDateTime)
		prop.Value = t.UTC().Format("20060102T150405")
		return prop, nil
	}

	loc, err := recurrence.ResolveLocation(tzid)
	if err != nil {
		return nil, err
	}
	prop.SetDateTime(t.In(loc))
	return prop, nil
}

// CalendarETag derives a calendar object's ETag from its normalized
// reconstruction (ADR-0026): a hash of the exact bytes GetCalendarObject
// serves, never the client's raw PUT bytes, so a PUT's response ETag always
// matches the next GET and clients never loop re-fetching a "changed"
// object.
func CalendarETag(cal *ical.Calendar) (string, error) {
	body, err := Encode(cal)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// Encode renders cal as the raw bytes an ICS file or a CalDAV GET response
// carries.
func Encode(cal *ical.Calendar) ([]byte, error) {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, fmt.Errorf("encode calendar: %w", err)
	}
	return buf.Bytes(), nil
}

// EncodeEmpty renders cal — a VCALENDAR with no VEVENT/VTIMEZONE children —
// as raw ICS bytes. go-ical's Encode refuses any VCALENDAR with zero
// Children ("calendar is empty"), which is right for a CalDAV object but
// wrong for a Calendar with no Events: the bulk export (#92) must still be
// able to emit an entry for it, carrying PRODID/VERSION/X-WR-CALNAME/
// X-APPLE-CALENDAR-COLOR so its name and color survive the round trip. This
// replicates go-ical's own unexported property encoding (sorted names, one
// line per value, no folding — go-ical does none either) without its
// emptiness check, which only applies at the top level.
func EncodeEmpty(cal *ical.Calendar) ([]byte, error) {
	if len(cal.Children) != 0 {
		return nil, fmt.Errorf("encode empty calendar: calendar has children")
	}

	var buf bytes.Buffer
	buf.WriteString("BEGIN:VCALENDAR\r\n")

	names := make([]string, 0, len(cal.Props))
	for name := range cal.Props {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		for _, prop := range cal.Props[name] {
			if err := encodeEmptyCalendarProp(&buf, &prop); err != nil {
				return nil, fmt.Errorf("encode calendar: %w", err)
			}
		}
	}

	buf.WriteString("END:VCALENDAR\r\n")
	return buf.Bytes(), nil
}

// encodeEmptyCalendarProp writes one property line exactly as go-ical's
// unexported Encoder.encodeProp does: sorted params, comma-joined values,
// quoted when a value contains one of ;:, — the two encoders must stay
// identical or EncodeEmpty's output would diverge from Encode's for the
// same property shape.
func encodeEmptyCalendarProp(buf *bytes.Buffer, prop *ical.Prop) error {
	buf.WriteString(prop.Name)

	paramNames := make([]string, 0, len(prop.Params))
	for name := range prop.Params {
		paramNames = append(paramNames, name)
	}
	sort.Strings(paramNames)

	for _, name := range paramNames {
		buf.WriteString(";")
		buf.WriteString(name)
		buf.WriteString("=")

		for i, v := range prop.Params[name] {
			if i > 0 {
				buf.WriteString(",")
			}
			if strings.ContainsRune(v, '"') {
				return fmt.Errorf("encode param value: contains a double-quote")
			}
			if strings.ContainsAny(v, ";:,") {
				buf.WriteString(`"` + v + `"`)
			} else {
				buf.WriteString(v)
			}
		}
	}

	buf.WriteString(":")
	if strings.ContainsAny(prop.Value, "\r\n") {
		return fmt.Errorf("encode property value: contains a CR or LF")
	}
	buf.WriteString(prop.Value)
	buf.WriteString("\r\n")
	return nil
}

// propWRCalName and propAppleCalendarColor are non-standard but
// widely-supported VCALENDAR properties (no constant in go-ical) carrying a
// Calendar's name and color through export/import, so both survive into
// other apps and back into this app's own importer (#76).
const (
	propWRCalName          = "X-WR-CALNAME"
	propAppleCalendarColor = "X-APPLE-CALENDAR-COLOR"
)

// propRefreshInterval and propPublishedTTL are the two ways a publisher
// states its own feed's poll cadence: RFC 7986's registered property, and
// the older Microsoft/Google convention it superseded. A Refresh honours
// whichever is present, preferring the RFC 7986 form (#86, ADR-0033).
const (
	propRefreshInterval = "REFRESH-INTERVAL"
	propPublishedTTL    = "X-PUBLISHED-TTL"
)

// setRawText sets an X- property to value with no VALUE=TEXT param and no
// backslash-escaping — go-ical's SetText adds both for any property it has
// no built-in default value type for, which is correct but not how other
// calendar apps write these two conventionally-plain-text extension
// properties (matching how buildVEvent's RRULE prop is built for the same
// reason).
func setRawText(props ical.Props, name, value string) {
	prop := ical.NewProp(name)
	prop.Value = value
	props.Set(prop)
}

// CalendarToICal recomposes every series in a Calendar into one VCALENDAR:
// a VTIMEZONE for each distinct TZID referenced across every series, then
// each Master's VEVENT (with its Overrides), in the same shape SeriesToICal
// produces for one series. name and color are carried as X-WR-CALNAME/
// X-APPLE-CALENDAR-COLOR so a Calendar's identity survives the round trip
// (#76) — masters need not be pre-sorted; the output orders them by ID for
// a deterministic file. Every export call site passes a CalendarFileTarget
// (ADR-0041); CalendarToICal is never used to serve CalDAV.
func CalendarToICal(name, color string, masters []repository.Event, overridesByParent map[string][]repository.Event, target SerializationTarget) (*ical.Calendar, error) {
	cal := newVCalendar()
	setRawText(cal.Props, propWRCalName, name)
	if color != "" {
		setRawText(cal.Props, propAppleCalendarColor, color)
	}

	for _, tzid := range calendarTzids(masters, overridesByParent) {
		vtz, err := buildVTimezone(tzid)
		if err != nil {
			return nil, fmt.Errorf("build vtimezone %q: %w", tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	sorted := append([]repository.Event(nil), masters...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, master := range sorted {
		if err := appendSeriesVEvents(cal, master, overridesByParent[master.ID], target); err != nil {
			return nil, err
		}
	}
	return cal, nil
}

// calendarTzids is seriesTzids generalized across every series in a
// Calendar, so CalendarToICal emits exactly one VTIMEZONE per zone actually
// referenced anywhere in it.
func calendarTzids(masters []repository.Event, overridesByParent map[string][]repository.Event) []string {
	seen := make(map[string]struct{})
	for _, master := range masters {
		for _, tzid := range seriesTzids(master, overridesByParent[master.ID]) {
			seen[tzid] = struct{}{}
		}
	}

	tzids := make([]string, 0, len(seen))
	for tzid := range seen {
		tzids = append(tzids, tzid)
	}
	sort.Strings(tzids)
	return tzids
}

// OccurrenceToICal renders one flattened Occurrence — e's Start/End already
// concrete, its Rrule already cleared by the caller (service.GetOccurrence)
// — as a standalone VCALENDAR carrying a single VEVENT: uid (a fresh id,
// never the series' own UID) with no RRULE and no RECURRENCE-ID. Emitting
// the series' UID with a RECURRENCE-ID instead would describe an orphan
// detached instance, which is both a bad shape for a recipient client and
// the exact shape this app's own ICS importer rejects (#76).
func OccurrenceToICal(uid string, e repository.Event) (*ical.Calendar, error) {
	cal := newVCalendar()

	if e.Tzid != nil {
		vtz, err := buildVTimezone(*e.Tzid)
		if err != nil {
			return nil, fmt.Errorf("build vtimezone %q: %w", *e.Tzid, err)
		}
		cal.Children = append(cal.Children, vtz)
	}

	v, err := buildVEvent(e, uid, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("build occurrence vevent: %w", err)
	}
	cal.Children = append(cal.Children, v.Component)
	return cal, nil
}
