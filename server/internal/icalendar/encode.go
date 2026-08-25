package icalendar

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/emersion/go-ical"
)

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
