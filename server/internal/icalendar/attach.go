package icalendar

import (
	"encoding/base64"
	"fmt"
	"io"
	"strconv"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calich/server/internal/repository"
)

// AttachmentOpener opens attachmentID's stored bytes so a Calendar file
// target (CalendarFileTarget) can inline them. Supplied by the caller — the
// service/handlers layer, which owns the attachmentstore — so this package
// stays free of a dependency on the filesystem attachment store.
type AttachmentOpener func(attachmentID string) (io.ReadCloser, error)

// SerializationTarget is how ATTACH is rendered — the one property that
// differs between a Calendar object and a Calendar file (ADR-0041).
// Everything else the codec emits comes from the same unconditional path
// regardless of target. The zero value renders no ATTACH at all, which is
// what a caller wanting neither shape (the decode-side round-trip tests)
// passes.
type SerializationTarget struct {
	caldavURIPrefix string
	inline          *inlineAttachments
}

// OmissionReason is why an encode left an Attachment out of the calendar it
// produced. One reason today, named rather than implied so the Export
// summary reports the encoder's own answer instead of restating the rule
// (#217).
type OmissionReason string

// OmittedOverInlineCap means the Attachment is larger than the target's
// inline ceiling, so a Calendar file omits it rather than inlining or
// truncating it (ADR-0041).
const OmittedOverInlineCap OmissionReason = "over_inline_cap"

// OmittedAttachment is one Attachment an encode left out, and why — what
// the Export summary discloses to a human still able to act on it
// (ADR-0041). Attributed to the Event whose series the Attachment hangs off
// (ADR-0040). Every encoder entrypoint returns these alongside its
// calendar, so the pre-flight and the download cannot disagree: they are
// the same encode.
type OmittedAttachment struct {
	Filename   string
	SizeBytes  int64
	EventID    string
	EventTitle string
	Reason     OmissionReason
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

// attachManagedIDParam, attachFmtTypeParam, attachSizeParam and
// attachFilenameParam are RFC 8607's ATTACH parameters carrying a managed
// Attachment's identity and metadata alongside its URI value.
const (
	attachManagedIDParam = "MANAGED-ID"
	attachFmtTypeParam   = "FMTTYPE"
	attachSizeParam      = "SIZE"
	attachFilenameParam  = "FILENAME"
)

// appendAttachProps adds one ATTACH property per owner.Attachments entry onto
// v, rendered per target (ADR-0041):
//   - CalDAVTarget: the RFC 8607 managed-attachment URI (uriPrefix+id) as the
//     value, with MANAGED-ID/FMTTYPE/SIZE/FILENAME params (#133, ADR-0040).
//   - CalendarFileTarget: the Attachment's bytes inlined
//     (ENCODING=BASE64;VALUE=BINARY) with FMTTYPE/FILENAME params. An
//     Attachment over the target's inline cap is omitted, not truncated, and
//     returned as an OmittedAttachment attributed to owner.
//   - The zero SerializationTarget: a no-op.
//
// This inline-cap comparison is the only place the ceiling is applied — the
// Export summary reads these omissions rather than restating the predicate
// (#217).
func appendAttachProps(v *ical.Event, owner repository.Event, target SerializationTarget) ([]OmittedAttachment, error) {
	switch {
	case target.caldavURIPrefix != "":
		for _, a := range owner.Attachments {
			v.Props.Add(caldavAttachProp(a, target.caldavURIPrefix))
		}
	case target.inline != nil:
		var omitted []OmittedAttachment
		for _, a := range owner.Attachments {
			if a.SizeBytes > target.inline.maxBytes {
				omitted = append(omitted, OmittedAttachment{
					Filename:   a.Filename,
					SizeBytes:  a.SizeBytes,
					EventID:    owner.ID,
					EventTitle: owner.Title,
					Reason:     OmittedOverInlineCap,
				})
				continue
			}
			prop, err := inlineAttachProp(a, target.inline.open)
			if err != nil {
				return nil, fmt.Errorf("inline attachment %s: %w", a.ID, err)
			}
			v.Props.Add(prop)
		}
		return omitted, nil
	}
	return nil, nil
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
