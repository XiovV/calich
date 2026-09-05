package service

import (
	"testing"
)

func TestMapGoogleEvents_NonRecurringEvent(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "evt-1",
			ETag:    `"abc123"`,
			Summary: "Dentist",
			Start:   googleEventDateTime{DateTime: "2026-01-15T10:00:00-05:00", TimeZone: "America/New_York"},
			End:     googleEventDateTime{DateTime: "2026-01-15T11:00:00-05:00", TimeZone: "America/New_York"},
		},
	}

	series, summary := mapGoogleEvents(events)

	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	if len(summary.Dropped) != 0 {
		t.Fatalf("expected nothing dropped, got %+v", summary.Dropped)
	}

	s := series[0]
	if s.ExternalUID != "evt-1" {
		t.Fatalf("expected ExternalUID %q, got %q", "evt-1", s.ExternalUID)
	}
	if s.Write.Title != "Dentist" {
		t.Fatalf("expected title %q, got %q", "Dentist", s.Write.Title)
	}
	if s.Write.Rrule != "" {
		t.Fatalf("expected no rrule for a non-recurring event, got %q", s.Write.Rrule)
	}
	if s.Write.Tzid == nil || *s.Write.Tzid != "America/New_York" {
		t.Fatalf("expected Anchor zone America/New_York, got %v", s.Write.Tzid)
	}
	if s.Write.ProviderEtag == nil || *s.Write.ProviderEtag != "abc123" {
		t.Fatalf("expected etag abc123 with quotes stripped, got %v", s.Write.ProviderEtag)
	}
}

func TestMapGoogleEvents_RecurringMasterWithOverrideAndException(t *testing.T) {
	events := []googleEvent{
		{
			ID:         "series-1",
			Summary:    "Standup",
			Start:      googleEventDateTime{DateTime: "2026-01-05T09:00:00-05:00", TimeZone: "America/New_York"},
			End:        googleEventDateTime{DateTime: "2026-01-05T09:15:00-05:00", TimeZone: "America/New_York"},
			Recurrence: []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"},
		},
		{
			ID:                "series-1_20260112T140000Z",
			RecurringEventID:  "series-1",
			OriginalStartTime: &googleEventDateTime{DateTime: "2026-01-12T09:00:00-05:00", TimeZone: "America/New_York"},
			Summary:           "Standup (moved)",
			Start:             googleEventDateTime{DateTime: "2026-01-12T10:00:00-05:00", TimeZone: "America/New_York"},
			End:               googleEventDateTime{DateTime: "2026-01-12T10:15:00-05:00", TimeZone: "America/New_York"},
			Status:            "confirmed",
		},
		{
			ID:                "series-1_20260119T140000Z",
			RecurringEventID:  "series-1",
			OriginalStartTime: &googleEventDateTime{DateTime: "2026-01-19T09:00:00-05:00", TimeZone: "America/New_York"},
			Status:            "cancelled",
		},
	}

	series, summary := mapGoogleEvents(events)

	if len(series) != 1 {
		t.Fatalf("expected 1 series (master + instances fold into one), got %d", len(series))
	}
	if len(summary.Dropped) != 0 {
		t.Fatalf("expected nothing dropped, got %+v", summary.Dropped)
	}

	write := series[0].Write
	if write.Rrule != "FREQ=WEEKLY;BYDAY=MO" {
		t.Fatalf("expected the RRULE value stripped of its prefix, got %q", write.Rrule)
	}
	if len(write.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(write.Overrides))
	}
	if write.Overrides[0].Title != "Standup (moved)" {
		t.Fatalf("expected override title, got %q", write.Overrides[0].Title)
	}
	wantRecurrenceID := mustTime(t, "2026-01-12T14:00:00Z")
	if !write.Overrides[0].RecurrenceID.Equal(wantRecurrenceID) {
		t.Fatalf("expected override RecurrenceID %v, got %v", wantRecurrenceID, write.Overrides[0].RecurrenceID)
	}

	if len(write.Exdates) != 1 {
		t.Fatalf("expected 1 exdate from the cancelled instance, got %d", len(write.Exdates))
	}
	wantExdate := mustTime(t, "2026-01-19T14:00:00Z")
	if !write.Exdates[0].Equal(wantExdate) {
		t.Fatalf("expected exdate %v, got %v", wantExdate, write.Exdates[0])
	}
}

func TestMapGoogleEvents_AllDayEventHasNoAnchorZone(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "evt-allday",
			Summary: "Conference",
			Start:   googleEventDateTime{Date: "2026-03-10"},
			End:     googleEventDateTime{Date: "2026-03-13"},
		},
	}

	series, _ := mapGoogleEvents(events)
	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}

	write := series[0].Write
	if !write.AllDay {
		t.Fatalf("expected AllDay true")
	}
	if write.Tzid != nil {
		t.Fatalf("expected no Anchor zone on an all-day event, got %v", write.Tzid)
	}
	if !write.Start.Equal(mustTime(t, "2026-03-10T00:00:00Z")) {
		t.Fatalf("unexpected start: %v", write.Start)
	}
	if !write.End.Equal(mustTime(t, "2026-03-13T00:00:00Z")) {
		t.Fatalf("unexpected end (Google's own exclusive end already matches ADR-0017): %v", write.End)
	}
}

func TestMapGoogleEvents_UTCEventWithNoTimeZoneNormalizesToEtcUTC(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "evt-utc",
			Summary: "UTC meeting",
			Start:   googleEventDateTime{DateTime: "2026-06-01T12:00:00Z"},
			End:     googleEventDateTime{DateTime: "2026-06-01T13:00:00Z"},
		},
	}

	series, _ := mapGoogleEvents(events)
	write := series[0].Write
	if write.Tzid == nil || *write.Tzid != "Etc/UTC" {
		t.Fatalf("expected Etc/UTC anchor zone, got %v", write.Tzid)
	}
}

func TestMapGoogleEvents_FloatingEventHasNoTimeZoneAndNoZSuffix(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "evt-floating",
			Summary: "Take meds",
			Start:   googleEventDateTime{DateTime: "2026-06-01T09:00:00+00:00"},
			End:     googleEventDateTime{DateTime: "2026-06-01T09:05:00+00:00"},
		},
	}

	series, _ := mapGoogleEvents(events)
	write := series[0].Write
	if write.Tzid != nil {
		t.Fatalf("expected a Floating Event (nil tzid), got %v", write.Tzid)
	}
}

func TestMapGoogleEvents_RDATEAndEXRULEAreDroppedAndCounted(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "series-rdate",
			Summary: "Irregular meeting",
			Start:   googleEventDateTime{DateTime: "2026-02-01T09:00:00-05:00", TimeZone: "America/New_York"},
			End:     googleEventDateTime{DateTime: "2026-02-01T09:30:00-05:00", TimeZone: "America/New_York"},
			Recurrence: []string{
				"RRULE:FREQ=WEEKLY",
				"RDATE:20260203T140000Z",
				"EXRULE:FREQ=DAILY",
			},
		},
	}

	series, summary := mapGoogleEvents(events)
	if len(series) != 1 {
		t.Fatalf("expected the series to still be written despite unsupported lines, got %d", len(series))
	}
	if series[0].Write.Rrule != "FREQ=WEEKLY" {
		t.Fatalf("expected RRULE to still be applied, got %q", series[0].Write.Rrule)
	}

	if len(summary.Dropped) != 2 {
		t.Fatalf("expected 2 dropped groups (RDATE, EXRULE), got %+v", summary.Dropped)
	}
	byReason := map[string]SkippedGroup{}
	for _, g := range summary.Dropped {
		byReason[g.Reason] = g
	}
	if g, ok := byReason[DroppedRDATE]; !ok || g.Count != 1 {
		t.Fatalf("expected 1 dropped RDATE, got %+v", byReason[DroppedRDATE])
	}
	if g, ok := byReason[DroppedEXRULE]; !ok || g.Count != 1 {
		t.Fatalf("expected 1 dropped EXRULE, got %+v", byReason[DroppedEXRULE])
	}
}

func TestMapGoogleEvents_EXDATEWithTZIDParamIsParsed(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "series-exdate",
			Summary: "Weekly sync",
			Start:   googleEventDateTime{DateTime: "2026-02-02T09:00:00-05:00", TimeZone: "America/New_York"},
			End:     googleEventDateTime{DateTime: "2026-02-02T09:30:00-05:00", TimeZone: "America/New_York"},
			Recurrence: []string{
				"RRULE:FREQ=WEEKLY",
				"EXDATE;TZID=America/New_York:20260209T090000",
			},
		},
	}

	series, summary := mapGoogleEvents(events)
	if len(summary.Dropped) != 0 {
		t.Fatalf("expected nothing dropped, got %+v", summary.Dropped)
	}
	if len(series[0].Write.Exdates) != 1 {
		t.Fatalf("expected 1 exdate, got %d", len(series[0].Write.Exdates))
	}
	want := mustTime(t, "2026-02-09T14:00:00Z")
	if !series[0].Write.Exdates[0].Equal(want) {
		t.Fatalf("expected exdate %v, got %v", want, series[0].Write.Exdates[0])
	}
}

func TestMapGoogleEvents_EXDATEAllDayValue(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "series-exdate-allday",
			Summary: "Daily standup",
			Start:   googleEventDateTime{Date: "2026-02-02"},
			End:     googleEventDateTime{Date: "2026-02-03"},
			Recurrence: []string{
				"RRULE:FREQ=DAILY",
				"EXDATE;VALUE=DATE:20260204",
			},
		},
	}

	series, _ := mapGoogleEvents(events)
	if len(series[0].Write.Exdates) != 1 {
		t.Fatalf("expected 1 exdate, got %d", len(series[0].Write.Exdates))
	}
	want := mustTime(t, "2026-02-04T00:00:00Z")
	if !series[0].Write.Exdates[0].Equal(want) {
		t.Fatalf("expected exdate %v, got %v", want, series[0].Write.Exdates[0])
	}
}

func TestMapGoogleEvents_OrphanInstanceWithNoMasterIsSkippedAndCounted(t *testing.T) {
	events := []googleEvent{
		{
			ID:                "orphan-instance",
			RecurringEventID:  "missing-master",
			OriginalStartTime: &googleEventDateTime{DateTime: "2026-01-12T09:00:00-05:00", TimeZone: "America/New_York"},
			Summary:           "Orphan",
		},
	}

	series, summary := mapGoogleEvents(events)
	if len(series) != 0 {
		t.Fatalf("expected no series written for an orphan instance, got %d", len(series))
	}
	if len(summary.Dropped) != 1 || summary.Dropped[0].Reason != DroppedOrphanInstance || summary.Dropped[0].Count != 1 {
		t.Fatalf("expected 1 DroppedOrphanInstance, got %+v", summary.Dropped)
	}
	if len(summary.OrphanExternalUIDs) != 1 || summary.OrphanExternalUIDs[0] != "missing-master" {
		t.Fatalf("expected the orphan's ExternalUID surfaced so the caller never tombstones it, got %v", summary.OrphanExternalUIDs)
	}
}

func TestMapGoogleEvents_RSVPAndGuestCountExcludeSelfAndResources(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "evt-attendees",
			Summary: "Team lunch",
			Start:   googleEventDateTime{DateTime: "2026-04-01T12:00:00-04:00", TimeZone: "America/New_York"},
			End:     googleEventDateTime{DateTime: "2026-04-01T13:00:00-04:00", TimeZone: "America/New_York"},
			Attendees: []googleAttendee{
				{Self: true, ResponseStatus: "accepted"},
				{ResponseStatus: "needsAction"},
				{ResponseStatus: "declined"},
				{Resource: true, ResponseStatus: "accepted"},
			},
		},
	}

	series, _ := mapGoogleEvents(events)
	write := series[0].Write
	if write.RSVPStatus == nil || *write.RSVPStatus != "accepted" {
		t.Fatalf("expected self's RSVP accepted, got %v", write.RSVPStatus)
	}
	if write.GuestCount != 2 {
		t.Fatalf("expected guest count 2 (excluding self and the resource), got %d", write.GuestCount)
	}
}

func TestMapGoogleEvents_ConferenceURLFromVideoEntryPoint(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "evt-conf",
			Summary: "Video call",
			Start:   googleEventDateTime{DateTime: "2026-04-01T12:00:00-04:00", TimeZone: "America/New_York"},
			End:     googleEventDateTime{DateTime: "2026-04-01T13:00:00-04:00", TimeZone: "America/New_York"},
			ConferenceData: &googleConferenceData{
				EntryPoints: []googleEntryPoint{
					{EntryPointType: "phone", URI: "tel:+1-555-0100"},
					{EntryPointType: "video", URI: "https://meet.google.com/abc-defg-hij"},
				},
			},
		},
	}

	series, _ := mapGoogleEvents(events)
	write := series[0].Write
	if write.ConferenceURL == nil || *write.ConferenceURL != "https://meet.google.com/abc-defg-hij" {
		t.Fatalf("expected the video entry point's URI, got %v", write.ConferenceURL)
	}
}

func TestMapGoogleEvents_NoConferenceDataIsNil(t *testing.T) {
	events := []googleEvent{
		{
			ID:      "evt-no-conf",
			Summary: "Plain meeting",
			Start:   googleEventDateTime{DateTime: "2026-04-01T12:00:00-04:00", TimeZone: "America/New_York"},
			End:     googleEventDateTime{DateTime: "2026-04-01T13:00:00-04:00", TimeZone: "America/New_York"},
		},
	}

	series, _ := mapGoogleEvents(events)
	if series[0].Write.ConferenceURL != nil {
		t.Fatalf("expected nil conference URL, got %v", series[0].Write.ConferenceURL)
	}
}

func TestParseGoogleExdateValue_MultipleCommaSeparatedValues(t *testing.T) {
	parsed, err := parseGoogleExdateValue(map[string]string{"TZID": "America/New_York"}, "20260209T090000,20260216T090000", nil)
	if err != nil {
		t.Fatalf("parse exdate value: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 parsed values, got %d", len(parsed))
	}
	if !parsed[0].Equal(mustTime(t, "2026-02-09T14:00:00Z")) {
		t.Fatalf("unexpected first value: %v", parsed[0])
	}
	if !parsed[1].Equal(mustTime(t, "2026-02-16T14:00:00Z")) {
		t.Fatalf("unexpected second value: %v", parsed[1])
	}
}

func TestSplitGoogleRecurrenceLine(t *testing.T) {
	cases := []struct {
		line       string
		wantName   string
		wantParams map[string]string
		wantValue  string
	}{
		{"RRULE:FREQ=WEEKLY;BYDAY=MO", "RRULE", nil, "FREQ=WEEKLY;BYDAY=MO"},
		{"EXDATE;TZID=America/New_York:20260209T090000", "EXDATE", map[string]string{"TZID": "America/New_York"}, "20260209T090000"},
		{"EXDATE;VALUE=DATE:20260204", "EXDATE", map[string]string{"VALUE": "DATE"}, "20260204"},
	}
	for _, c := range cases {
		name, params, value := splitGoogleRecurrenceLine(c.line)
		if name != c.wantName || value != c.wantValue {
			t.Fatalf("line %q: got name=%q value=%q, want name=%q value=%q", c.line, name, value, c.wantName, c.wantValue)
		}
		if len(params) != len(c.wantParams) {
			t.Fatalf("line %q: got params=%v, want %v", c.line, params, c.wantParams)
		}
		for k, v := range c.wantParams {
			if params[k] != v {
				t.Fatalf("line %q: param %q = %q, want %q", c.line, k, params[k], v)
			}
		}
	}
}

func TestGoogleEtag_StripsQuotes(t *testing.T) {
	if got := googleEtag(`"abc123"`); got == nil || *got != "abc123" {
		t.Fatalf("expected abc123, got %v", got)
	}
	if got := googleEtag(""); got != nil {
		t.Fatalf("expected nil for empty etag, got %v", got)
	}
}

func TestMapGoogleEventChanges_TopLevelCancelledIsADeletionNotAnUpsert(t *testing.T) {
	events := []googleEvent{
		{ID: "evt-gone", Status: "cancelled"},
		{
			ID:      "evt-live",
			Summary: "Still here",
			Start:   googleEventDateTime{DateTime: "2026-03-01T10:00:00Z"},
			End:     googleEventDateTime{DateTime: "2026-03-01T11:00:00Z"},
		},
	}

	changes, deletions, _ := mapGoogleEventChanges(events)

	if len(deletions) != 1 || deletions[0] != "evt-gone" {
		t.Fatalf("expected evt-gone in deletions, got %+v", deletions)
	}
	if len(changes) != 1 || changes[0].ExternalUID != "evt-live" {
		t.Fatalf("expected only evt-live as a change, got %+v", changes)
	}
	if changes[0].Master == nil {
		t.Fatalf("expected the live event to carry a Master")
	}
}

func TestMapGoogleEventChanges_MasterInBatchProducesFullSeries(t *testing.T) {
	events := []googleEvent{
		{
			ID:         "series-1",
			Summary:    "Standup",
			Start:      googleEventDateTime{DateTime: "2026-01-05T09:00:00-05:00", TimeZone: "America/New_York"},
			End:        googleEventDateTime{DateTime: "2026-01-05T09:15:00-05:00", TimeZone: "America/New_York"},
			Recurrence: []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"},
		},
		{
			ID:                "series-1_i1",
			RecurringEventID:  "series-1",
			OriginalStartTime: &googleEventDateTime{DateTime: "2026-01-12T09:00:00-05:00", TimeZone: "America/New_York"},
			Summary:           "Standup (moved)",
			Start:             googleEventDateTime{DateTime: "2026-01-12T10:00:00-05:00", TimeZone: "America/New_York"},
			End:               googleEventDateTime{DateTime: "2026-01-12T10:15:00-05:00", TimeZone: "America/New_York"},
			Status:            "confirmed",
		},
	}

	changes, deletions, _ := mapGoogleEventChanges(events)

	if len(deletions) != 0 {
		t.Fatalf("expected no deletions, got %+v", deletions)
	}
	if len(changes) != 1 || changes[0].Master == nil {
		t.Fatalf("expected one change carrying a Master, got %+v", changes)
	}
	if changes[0].Master.Rrule != "FREQ=WEEKLY;BYDAY=MO" {
		t.Fatalf("expected the RRULE carried, got %q", changes[0].Master.Rrule)
	}
	if len(changes[0].Master.Overrides) != 1 {
		t.Fatalf("expected the in-batch instance folded into the Master, got %+v", changes[0].Master.Overrides)
	}
}

func TestMapGoogleEventChanges_InstanceOnlyChangeHasNoMaster(t *testing.T) {
	events := []googleEvent{
		{
			ID:                "series-1_i2",
			RecurringEventID:  "series-1",
			OriginalStartTime: &googleEventDateTime{DateTime: "2026-01-19T09:00:00Z"},
			Summary:           "Standup (moved again)",
			Start:             googleEventDateTime{DateTime: "2026-01-19T10:00:00Z"},
			End:               googleEventDateTime{DateTime: "2026-01-19T10:15:00Z"},
			Status:            "confirmed",
		},
		{
			ID:                "series-1_i3",
			RecurringEventID:  "series-1",
			OriginalStartTime: &googleEventDateTime{DateTime: "2026-01-26T09:00:00Z"},
			Status:            "cancelled",
		},
	}

	changes, _, _ := mapGoogleEventChanges(events)

	if len(changes) != 1 {
		t.Fatalf("expected one merged change for series-1, got %+v", changes)
	}
	c := changes[0]
	if c.ExternalUID != "series-1" || c.Master != nil {
		t.Fatalf("expected a Master-absent change keyed by series-1, got %+v", c)
	}
	if len(c.Overrides) != 1 || len(c.Cancellations) != 1 {
		t.Fatalf("expected 1 override and 1 cancellation, got %+v", c)
	}
}

func TestMapGoogleEventChanges_OrphanInstanceIsCountedNotEmitted(t *testing.T) {
	events := []googleEvent{
		{
			ID:               "orphan_i1",
			RecurringEventID: "orphan-series",
			// No OriginalStartTime — nothing to anchor to.
			Summary: "Ghost",
			Status:  "confirmed",
		},
	}

	changes, _, summary := mapGoogleEventChanges(events)

	if len(changes) != 0 {
		t.Fatalf("expected no changes for an unanchored orphan, got %+v", changes)
	}
	if len(summary.OrphanExternalUIDs) != 1 || summary.OrphanExternalUIDs[0] != "orphan-series" {
		t.Fatalf("expected orphan-series counted, got %+v", summary.OrphanExternalUIDs)
	}
}

func TestMapGoogleEventChanges_RDATEStillCounted(t *testing.T) {
	events := []googleEvent{
		{
			ID:         "series-rdate",
			Summary:    "Odd cadence",
			Start:      googleEventDateTime{DateTime: "2026-01-05T09:00:00Z"},
			End:        googleEventDateTime{DateTime: "2026-01-05T10:00:00Z"},
			Recurrence: []string{"RRULE:FREQ=WEEKLY", "RDATE:20260210T090000Z"},
		},
	}

	_, _, summary := mapGoogleEventChanges(events)

	if countDropped(summary) != 1 {
		t.Fatalf("expected one dropped recurrence line, got %+v", summary.Dropped)
	}
}
