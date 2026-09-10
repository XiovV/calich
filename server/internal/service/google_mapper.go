// google_mapper.go is the Google half of the Provider mapper (#287,
// ADR-0050): googleEvent JSON in, domain []IncomingSeries out, no clock and
// no database — following recurrence.Expand, reminder.DueAll and the
// ADR-0033 reconciler. This is where every one of Google's edge cases lands,
// so it is table-tested directly (google_mapper_test.go).
package service

import (
	"strings"
	"time"

	"github.com/XiovV/calich/server/internal/icalendar"
)

// googleEventDateTime is the start/end/originalStartTime shape Google sends
// on every Event resource: Date is set for an all-day boundary, DateTime for
// a timed one, and TimeZone (only ever alongside DateTime) is the IANA zone
// this app calls the Anchor zone.
type googleEventDateTime struct {
	Date, DateTime, TimeZone string
}

// googleAttendee is one entry of an Event's attendees array — enough to
// resolve the connecting User's own RSVP and a bare guest count (#287,
// ADR-0052) without modelling attendees as rows of our own.
type googleAttendee struct {
	Self           bool
	Resource       bool
	ResponseStatus string
}

// googleEntryPoint is one conferenceData.entryPoints row; only "video"
// carries the join link this app stores (#287, ADR-0052).
type googleEntryPoint struct {
	EntryPointType, URI string
}

type googleConferenceData struct {
	EntryPoints []googleEntryPoint
}

// googleEvent is one decoded Events.list item (#287). A Master carries a
// Recurrence array and an empty RecurringEventID; an instance of a
// recurring Event carries RecurringEventID (its Master's own ID) and
// OriginalStartTime, and is either a modified instance (Status != cancelled,
// becomes an Override) or a cancelled one (Status == cancelled, becomes an
// Exdate) — Google returns both kinds of instance alongside the Master when
// listed with singleEvents=false, regardless of showDeleted.
type googleEvent struct {
	ID          string
	ETag        string
	Status      string
	Summary     string
	Description string
	Location    string
	// ColorID is Google's own per-event colour id ("1".."11"), empty when the
	// event carries no colour. Mapped to a hex inbound (#289, ADR-0075) and
	// shadow-tracked; never sent back.
	ColorID           string
	Start, End        googleEventDateTime
	RecurringEventID  string
	OriginalStartTime *googleEventDateTime
	Recurrence        []string
	Attendees         []googleAttendee
	ConferenceData    *googleConferenceData
}

const googleStatusCancelled = "cancelled"

// Reasons mapGoogleEvents surfaces on GoogleMappingSummary — recurrence
// features Google can carry that this app's model (RRULE plus EXDATE,
// ADR-0016) has no home for, and a recurring instance whose Master never
// appeared in this fetch, counted rather than silently dropped in the
// spirit of the Import summary (ADR-0030, ADR-0050).
const (
	DroppedRDATE          = "RDATE recurrence line"
	DroppedEXRULE         = "EXRULE recurrence line"
	DroppedOrphanInstance = "recurring event instance without its master"
	// DroppedMissingTitle is a Master whose Summary Google omitted entirely
	// — what a visibility-restricted event (a calendar shared with only "See
	// only free/busy") comes back as. Unstorable outright (ErrInvalidTitle),
	// so it is dropped here rather than left to fail deep in the reconciler
	// and abort the whole Refresh (#293).
	DroppedMissingTitle = "event has no title"
)

// GoogleMappingSummary tallies what mapGoogleEvents could not carry across,
// grouped exactly like import.go's SkippedGroup so a future summary UI can
// render the two identically.
type GoogleMappingSummary struct {
	Dropped []SkippedGroup
	// OrphanExternalUIDs are the ExternalUIDs of every group mapGoogleEvents
	// dropped under DroppedOrphanInstance — a series this Calendar may
	// already store, that this fetch's listing still names (via an
	// instance's RecurringEventID) but cannot itself resolve. The caller
	// must fold these into ReconcileSeries' unparseableUIDs, exactly as
	// ADR-0053's third bucket requires: present but unmappable is never a
	// reason to tombstone. A guard checked after the fact isn't enough here
	// (ADR-0053's own standard for this class of bug), so mapGoogleEvents
	// hands the caller the exact set rather than trusting it to notice.
	OrphanExternalUIDs []string
}

// add records one occurrence of reason against title (best-effort context,
// mirroring skippedGroups' sample titles), creating the group on first use.
func (s *GoogleMappingSummary) add(reason, title string) {
	for i := range s.Dropped {
		if s.Dropped[i].Reason == reason {
			s.Dropped[i].Count++
			if len(s.Dropped[i].Samples) < maxSkippedSamples && title != "" {
				s.Dropped[i].Samples = append(s.Dropped[i].Samples, title)
			}
			return
		}
	}
	group := SkippedGroup{Reason: reason, Count: 1}
	if title != "" {
		group.Samples = append(group.Samples, title)
	}
	s.Dropped = append(s.Dropped, group)
}

// mapGoogleEvents groups events by series (an instance's RecurringEventID,
// or its own ID for a standalone Master) and maps each group to one
// IncomingSeries, keyed by the Master's own Google event id (#287,
// ADR-0052's "the Provider's own event id is stored as External UID"). A
// group whose Master never appeared in this fetch is skipped and counted
// under DroppedOrphanInstance — present-but-unmappable, never treated as an
// absence a Full Refresh would tombstone (ADR-0053's third bucket).
func mapGoogleEvents(events []googleEvent) ([]IncomingSeries, GoogleMappingSummary) {
	order, groups := groupGoogleEventsBySeries(events)

	series := make([]IncomingSeries, 0, len(order))
	var summary GoogleMappingSummary
	for _, key := range order {
		master, instances := splitMasterAndInstances(groups[key])
		if master == nil {
			summary.add(DroppedOrphanInstance, "")
			summary.OrphanExternalUIDs = append(summary.OrphanExternalUIDs, key)
			continue
		}

		write, dropped := mapGoogleMaster(*master, instances)
		if strings.TrimSpace(write.Title) == "" {
			summary.add(DroppedMissingTitle, "")
			summary.OrphanExternalUIDs = append(summary.OrphanExternalUIDs, master.ID)
			continue
		}
		for _, reason := range dropped {
			summary.add(reason, write.Title)
		}
		series = append(series, IncomingSeries{ExternalUID: master.ID, Write: write})
	}

	return series, summary
}

// groupGoogleEventsBySeries buckets events by series key — an instance's
// RecurringEventID, or a standalone Master's own ID — returning the keys in
// first-seen order so mapping stays deterministic. Shared by mapGoogleEvents
// (Full Refresh) and mapGoogleEventChanges (Delta Refresh).
func groupGoogleEventsBySeries(events []googleEvent) (order []string, groups map[string][]googleEvent) {
	groups = make(map[string][]googleEvent, len(events))
	for _, e := range events {
		key := e.RecurringEventID
		if key == "" {
			key = e.ID
		}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], e)
	}
	return order, groups
}

// splitMasterAndInstances separates one series' group into its Master (the
// row with no RecurringEventID, nil when this fetch didn't include it) and
// its recurring instances.
func splitMasterAndInstances(group []googleEvent) (master *googleEvent, instances []googleEvent) {
	for i := range group {
		if group[i].RecurringEventID == "" {
			m := group[i]
			master = &m
			continue
		}
		instances = append(instances, group[i])
	}
	return master, instances
}

// DeltaSeriesChange is one series a Delta Refresh's batch touched (#288,
// ADR-0053), keyed by ExternalUID (the Master's Google event id):
//
//   - Master non-nil: the Provider sent the Master in this batch, fully
//     mapped from it (its own in-batch instances already folded into
//     Master.Overrides / Master.Exdates). ReconcileDelta overlays these
//     Master-level fields onto the stored series without disturbing stored
//     Overrides the batch didn't mention.
//   - Master nil: only instances of an otherwise-unchanged series changed —
//     the common Delta case. Overrides and Cancellations carry just those,
//     to be merged into the stored series.
//
// A change is never an instruction to remove anything by absence: a
// deletion is a separate, explicit signal (mapGoogleEventChanges' second
// return), so ReconcileDelta's merge only ever adds or replaces.
type DeltaSeriesChange struct {
	ExternalUID   string
	Master        *SeriesWrite
	Overrides     []OverrideWrite
	Cancellations []time.Time
}

// mapGoogleEventChanges maps one Delta Refresh batch (#288, ADR-0053) into
// the series that changed and the series the Provider explicitly deleted.
// Absence carries no meaning here — it is the overwhelming majority of every
// response and means "unchanged" — so nothing about a series not appearing
// is ever returned. deletions holds only the ExternalUIDs of events the
// Provider stated are gone (a top-level status=cancelled). The summary's
// OrphanExternalUIDs collects instance-only changes whose series this
// Calendar does not store, for the caller to fold into ReconcileDelta's
// unparseable set — present-but-unmappable, never an absence (ADR-0053's
// third bucket).
func mapGoogleEventChanges(events []googleEvent) (changes []DeltaSeriesChange, deletions []string, summary GoogleMappingSummary) {
	live := make([]googleEvent, 0, len(events))
	for _, e := range events {
		if e.RecurringEventID == "" && e.Status == googleStatusCancelled {
			// A wholly deleted standalone Event or recurring series. The
			// Provider stating it explicitly is the entire point of Delta mode
			// — this is the one path that may tombstone, and only this one.
			deletions = append(deletions, e.ID)
			continue
		}
		live = append(live, e)
	}

	order, groups := groupGoogleEventsBySeries(live)
	for _, key := range order {
		master, instances := splitMasterAndInstances(groups[key])

		if master != nil {
			write, dropped := mapGoogleMaster(*master, instances)
			if strings.TrimSpace(write.Title) == "" {
				summary.add(DroppedMissingTitle, "")
				summary.OrphanExternalUIDs = append(summary.OrphanExternalUIDs, master.ID)
				continue
			}
			for _, reason := range dropped {
				summary.add(reason, write.Title)
			}
			changes = append(changes, DeltaSeriesChange{ExternalUID: master.ID, Master: &write})
			continue
		}

		overrides, cancellations := mapGoogleInstances(instances, key)
		if len(overrides) == 0 && len(cancellations) == 0 {
			summary.add(DroppedOrphanInstance, "")
			summary.OrphanExternalUIDs = append(summary.OrphanExternalUIDs, key)
			continue
		}
		changes = append(changes, DeltaSeriesChange{ExternalUID: key, Overrides: overrides, Cancellations: cancellations})
	}

	return changes, deletions, summary
}

// mapGoogleMaster maps one series' Master plus its instances (Overrides and
// cancelled Exceptions alike) to a SeriesWrite. dropped names, once per
// unsupported recurrence line actually present, which of RDATE/EXRULE this
// Master's own recurrence array carried and could not be modelled
// (ADR-0016, ADR-0050) — RRULE and EXDATE are both fully supported and cost
// nothing here.
func mapGoogleMaster(master googleEvent, instances []googleEvent) (SeriesWrite, []string) {
	start, allDay, tzid := decodeGoogleTime(master.Start)

	rrule, exdates, dropped := parseGoogleRecurrence(master.Recurrence, tzid)

	rsvp, guestCount := googleGuestInfo(master.Attendees)

	// Colour is seeded from the Provider's colorId into both the displayed
	// value and the shadow (#289, ADR-0075). On a first import both persist;
	// on a later Refresh the reconciler moves the displayed value forward
	// only while it still equals the shadow — an untouched Event follows the
	// Provider's recolours, one recoloured here stays put. Never pushed back.
	providerColor := googleEventColorHex(master.ColorID)

	// exdates is parseGoogleRecurrence's own fresh slice; folding the
	// cancelled instances into it here is a local mutation, nothing else
	// holds a reference.
	overrides, cancellations := mapGoogleInstances(instances, master.ID)
	exdates = append(exdates, cancellations...)

	write := SeriesWrite{
		Title:         master.Summary,
		Description:   master.Description,
		Location:      master.Location,
		Start:         start,
		End:           firstOf(decodeGoogleTime(master.End)),
		AllDay:        allDay,
		Tzid:          tzid,
		Rrule:         rrule,
		Exdates:       exdates,
		Overrides:     overrides,
		ExternalUID:   master.ID,
		Color:         providerColor,
		ProviderColor: providerColor,
		ProviderEtag:  googleEtag(master.ETag),
		RSVPStatus:    rsvp,
		ConferenceURL: googleConferenceURL(master.ConferenceData),
		GuestCount:    guestCount,
	}

	return write, dropped
}

// mapGoogleInstances maps a series' recurring instances — modified ones to
// Overrides, cancelled ones to Exdate instants — keyed to the Occurrence
// each replaces by its originalStartTime (iCalendar RECURRENCE-ID). An
// instance missing originalStartTime carries nothing to anchor to and is
// silently skipped rather than guessed at; Google always sends it on a real
// instance. Shared by mapGoogleMaster (Full Refresh, and a Delta Refresh
// whose batch includes the Master) and mapGoogleEventChanges' Master-absent
// case (a Delta Refresh that changed one instance of an otherwise-unchanged
// series).
func mapGoogleInstances(instances []googleEvent, externalUID string) (overrides []OverrideWrite, cancellations []time.Time) {
	for _, instance := range instances {
		if instance.OriginalStartTime == nil {
			continue
		}
		recurrenceID, _, _ := decodeGoogleTime(*instance.OriginalStartTime)

		if instance.Status == googleStatusCancelled {
			cancellations = append(cancellations, recurrenceID)
			continue
		}

		iStart, iAllDay, iTzid := decodeGoogleTime(instance.Start)
		iRsvp, iGuestCount := googleGuestInfo(instance.Attendees)
		iProviderColor := googleEventColorHex(instance.ColorID)
		overrides = append(overrides, OverrideWrite{
			RecurrenceID:  recurrenceID,
			Title:         instance.Summary,
			Description:   instance.Description,
			Location:      instance.Location,
			Start:         iStart,
			End:           firstOf(decodeGoogleTime(instance.End)),
			AllDay:        iAllDay,
			Tzid:          iTzid,
			ExternalUID:   externalUID,
			Color:         iProviderColor,
			ProviderColor: iProviderColor,
			ProviderEtag:  googleEtag(instance.ETag),
			RSVPStatus:    iRsvp,
			ConferenceURL: googleConferenceURL(instance.ConferenceData),
			GuestCount:    iGuestCount,
		})
	}
	return overrides, cancellations
}

// firstOf discards decodeGoogleTime's allDay/tzid results — used for an
// End boundary, whose own AllDay/Tzid always mirror Start's and so aren't
// carried a second time.
func firstOf(t time.Time, _ bool, _ *string) time.Time {
	return t
}

// decodeGoogleTime maps one googleEventDateTime to the (instant, allDay,
// tzid) triple SeriesWrite/OverrideWrite store (ADR-0019) —
// icalendar.ResolveAnchorZone's decision rule (the same one
// icalendar.parseEventTime applies to an ICS property, so the two never
// drift apart) applied to Google's own Date/TimeZone/DateTime fields.
// Google's own half-open convention for an all-day End already matches
// ADR-0017's exactly, so no conversion happens here.
func decodeGoogleTime(dt googleEventDateTime) (t time.Time, allDay bool, tzid *string) {
	isDateOnly := dt.Date != ""
	allDay, tzid = icalendar.ResolveAnchorZone(isDateOnly, dt.TimeZone, strings.HasSuffix(dt.DateTime, "Z"))

	if isDateOnly {
		d, err := time.Parse("2006-01-02", dt.Date)
		if err != nil {
			return time.Time{}, allDay, tzid
		}
		return d.UTC(), allDay, tzid
	}

	parsed, err := time.Parse(time.RFC3339, dt.DateTime)
	if err != nil {
		return time.Time{}, allDay, tzid
	}
	return parsed.UTC(), allDay, tzid
}

// parseGoogleRecurrence decomposes a Master's recurrence array into the
// single RRULE this app models plus every EXDATE (ADR-0016). RDATE and
// EXRULE lines are counted in dropped rather than applied — this app has no
// home for either (ADR-0050) — and a line this app cannot even parse is
// skipped the same way, since a malformed EXDATE must never silently turn
// into a data-losing Exception. fallbackTzid anchors an EXDATE line that
// carries no TZID of its own to the Master's own Anchor zone, mirroring how
// the Master's own instant was decoded.
func parseGoogleRecurrence(lines []string, fallbackTzid *string) (rrule string, exdates []time.Time, dropped []string) {
	for _, line := range lines {
		name, params, value := splitGoogleRecurrenceLine(line)
		switch strings.ToUpper(name) {
		case "RRULE":
			rrule = value
		case "EXDATE":
			parsed, err := parseGoogleExdateValue(params, value, fallbackTzid)
			if err == nil {
				exdates = append(exdates, parsed...)
			}
		case "RDATE":
			dropped = append(dropped, DroppedRDATE)
		case "EXRULE":
			dropped = append(dropped, DroppedEXRULE)
		}
	}
	return rrule, exdates, dropped
}

// splitGoogleRecurrenceLine decomposes one RFC5545 content line — Google's
// own format for every entry of an Event's recurrence array — into its
// name ("RRULE", "EXDATE", ...), its ";"-delimited parameters (as a
// name->value map, e.g. {"TZID": "America/New_York"}), and its raw value
// (everything after the first ":").
func splitGoogleRecurrenceLine(line string) (name string, params map[string]string, value string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return line, nil, ""
	}
	header, value := line[:colon], line[colon+1:]

	parts := strings.Split(header, ";")
	name = parts[0]
	if len(parts) > 1 {
		params = make(map[string]string, len(parts)-1)
		for _, p := range parts[1:] {
			if eq := strings.IndexByte(p, '='); eq >= 0 {
				params[strings.ToUpper(p[:eq])] = p[eq+1:]
			}
		}
	}
	return name, params, value
}

// parseGoogleExdateValue parses one EXDATE line's comma-separated value
// list into instants. VALUE=DATE (or an 8-character token) means an
// all-day exclusion; a trailing "Z" means an absolute instant; otherwise
// the TZID param — or, absent that, fallbackTzid — localizes it, matching
// decodeGoogleTime's own rules for the Master's boundaries.
func parseGoogleExdateValue(params map[string]string, value string, fallbackTzid *string) ([]time.Time, error) {
	tzid := params["TZID"]
	if tzid == "" && fallbackTzid != nil {
		tzid = *fallbackTzid
	}

	var out []time.Time
	for _, token := range strings.Split(value, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		if params["VALUE"] == "DATE" || len(token) == 8 {
			t, err := time.Parse("20060102", token)
			if err != nil {
				return nil, err
			}
			out = append(out, t.UTC())
			continue
		}

		if strings.HasSuffix(token, "Z") {
			t, err := time.Parse("20060102T150405Z", token)
			if err != nil {
				return nil, err
			}
			out = append(out, t.UTC())
			continue
		}

		loc := time.UTC
		if tzid != "" {
			l, err := time.LoadLocation(tzid)
			if err != nil {
				return nil, err
			}
			loc = l
		}
		t, err := time.ParseInLocation("20060102T150405", token, loc)
		if err != nil {
			return nil, err
		}
		out = append(out, t.UTC())
	}
	return out, nil
}

// googleEtag strips the double-quote wrapping Google's own etag values
// carry (e.g. `"\"abc123\""` decoded to `"abc123"`), storing the bare
// validator this app's own If-Match usage will send back verbatim. Nil for
// an empty etag rather than a pointer to "".
func googleEtag(raw string) *string {
	trimmed := strings.Trim(raw, `"`)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// googleGuestInfo reads one event's attendees for the connecting User's own
// RSVP (the "self" attendee's responseStatus) and a bare guest count —
// every other attendee that isn't a resource, such as a meeting room
// (#287, ADR-0052). Attendees themselves are never modelled as rows.
func googleGuestInfo(attendees []googleAttendee) (rsvp *string, guestCount int) {
	for _, a := range attendees {
		if a.Self {
			if a.ResponseStatus != "" {
				status := a.ResponseStatus
				rsvp = &status
			}
			continue
		}
		if a.Resource {
			continue
		}
		guestCount++
	}
	return rsvp, guestCount
}

// googleConferenceURL returns the first "video" entry point's join link,
// nil when cd carries none — the conference join URL this app stores in
// its own field rather than smuggled into Location (#287, ADR-0052).
func googleConferenceURL(cd *googleConferenceData) *string {
	if cd == nil {
		return nil
	}
	for _, ep := range cd.EntryPoints {
		if ep.EntryPointType == "video" && ep.URI != "" {
			uri := ep.URI
			return &uri
		}
	}
	return nil
}
