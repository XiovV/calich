package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestFormatGoogleOriginalStart covers the events.instances `originalStart`
// filter a scoped recurring edit sends (#293, ADR-0078) — the value that
// resolves one instance's Provider id rather than constructing the composite
// `{id}_{timestamp}` form, which ADR-0075 warns breaks on exactly these edges.
func TestFormatGoogleOriginalStart(t *testing.T) {
	nyc := "America/New_York"

	tests := []struct {
		name         string
		recurrenceID time.Time
		allDay       bool
		tzid         *string
		want         string
	}{
		{
			name:         "all-day is a bare date, never timezone-converted",
			recurrenceID: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
			allDay:       true,
			tzid:         &nyc,
			want:         "2026-01-12",
		},
		{
			name: "timed before the March DST transition carries the standard -05:00 offset",
			// 2026-03-02 09:00 America/New_York, stored as the UTC instant.
			recurrenceID: time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC),
			tzid:         &nyc,
			want:         "2026-03-02T09:00:00-05:00",
		},
		{
			name: "timed after the March DST transition carries the daylight -04:00 offset",
			// 2026-03-16 09:00 America/New_York (DST began 2026-03-08).
			recurrenceID: time.Date(2026, 3, 16, 13, 0, 0, 0, time.UTC),
			tzid:         &nyc,
			want:         "2026-03-16T09:00:00-04:00",
		},
		{
			name:         "a Floating Occurrence has no zone to round-trip, so a Z instant",
			recurrenceID: time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC),
			tzid:         nil,
			want:         "2026-03-16T09:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatGoogleOriginalStart(tt.recurrenceID, tt.allDay, tt.tzid)
			if got != tt.want {
				t.Fatalf("formatGoogleOriginalStart = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGoogleClient_ListInstances covers the events.instances call itself
// (#293, ADR-0078): it hits the series' own /instances path carrying the
// originalStart filter, parses the single matching instance's id and etag,
// and reports an empty listing as errGoogleInstanceNotFound.
func TestGoogleClient_ListInstances(t *testing.T) {
	f := newFakeGoogleServer(t)
	f.instancesByOriginalStart = map[string]map[string]any{
		"2026-01-12T09:00:00-05:00": {
			"id":               "series-1_20260112T140000Z",
			"etag":             `"instance-etag-1"`,
			"recurringEventId": "series-1",
			"status":           "confirmed",
		},
	}
	// A connected account is needed so the fake server answers with a valid
	// bearer token; reuse the whole ConnectionService round trip.
	svc, _, _, _, _ := newTestConnectionServiceForWriteBack(t, f)
	client := svc.google

	got, err := client.listInstances(context.Background(), "fake-access-token", "primary", "series-1", "2026-01-12T09:00:00-05:00")
	if err != nil {
		t.Fatalf("listInstances: %v", err)
	}
	if got.ID != "series-1_20260112T140000Z" {
		t.Fatalf("expected the instance's own composite id, got %q", got.ID)
	}
	if got.ETag != `"instance-etag-1"` {
		t.Fatalf("expected the instance's own etag, got %q", got.ETag)
	}
	if len(f.instancesRequests) != 1 || f.instancesRequests[0].OriginalStart != "2026-01-12T09:00:00-05:00" {
		t.Fatalf("expected one instances request carrying the originalStart filter, got %+v", f.instancesRequests)
	}

	if _, err := client.listInstances(context.Background(), "fake-access-token", "primary", "series-1", "2099-01-01T00:00:00Z"); !errors.Is(err, errGoogleInstanceNotFound) {
		t.Fatalf("expected errGoogleInstanceNotFound for an original start the series never generated, got %v", err)
	}
}

// TestBuildGooglePatch_TitleOnlyChangeCarriesExactlyTheAllowedFields covers
// #290's acceptance-criteria bullet verbatim: "a title-only change produces
// a patch containing the title and nothing else — no guest list, no
// conference data, no visibility". Marshaling to a bare map is what proves
// this at the wire level, not just at the Go type level — a struct field
// with a bad json tag could still leak one of these even though
// googleEventPatchBody has no Go field for any of them.
func TestBuildGooglePatch_TitleOnlyChangeCarriesExactlyTheAllowedFields(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	patch := buildGooglePatch("Standup (renamed)", start, end, false, nil, "", "", "", "")

	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}

	wantKeys := map[string]bool{"summary": true, "description": true, "location": true, "start": true, "end": true, "source": true}
	for key := range m {
		if !wantKeys[key] {
			t.Fatalf("patch carried unexpected key %q (full body: %s)", key, raw)
		}
	}
	if m["summary"] != "Standup (renamed)" {
		t.Fatalf("expected summary %q, got %v", "Standup (renamed)", m["summary"])
	}

	// The load-bearing negative assertions: never present, whatever the
	// caller passed, because googleEventPatchBody has no field for any of
	// them at all (ADR-0075's "no guest list, no conference data, no
	// visibility").
	for _, forbidden := range []string{"attendees", "conferenceData", "visibility", "guestsCanModify", "transparency", "extendedProperties"} {
		if _, ok := m[forbidden]; ok {
			t.Fatalf("patch must never carry %q — a whole-Event replace must be impossible to express here", forbidden)
		}
	}
}

// TestBuildGooglePatch_OmitsRecurrenceForANonRecurringEvent covers a plain
// (non-recurring) Event: recurrence is entirely absent from the patch,
// never sent as an empty array — Google's own PATCH semantics read an
// absent key as "leave this alone" (there is nothing recurring to leave
// alone either way here).
func TestBuildGooglePatch_OmitsRecurrenceForANonRecurringEvent(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	patch := buildGooglePatch("Standup", start, end, false, nil, "", "", "", "")
	raw, _ := json.Marshal(patch)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)

	if _, ok := m["recurrence"]; ok {
		t.Fatalf("expected no recurrence key for a non-recurring event, got %s", raw)
	}
}

// TestBuildGooglePatch_RecurrenceCarriesRruleAndNeverAnExdate covers a Master
// edit's recurrence array (#293, ADR-0075, ADR-0078): it is exactly the one
// RRULE line and nothing else. A cancelled Occurrence is a `status: cancelled`
// instance at Google (CANCEL_INSTANCE), never an EXDATE entry here — both
// representations exist at Google and mixing them drifts.
func TestBuildGooglePatch_RecurrenceCarriesRruleAndNeverAnExdate(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	tzid := "America/New_York"

	patch := buildGooglePatch("Standup", start, end, false, &tzid, "FREQ=WEEKLY", "", "", "")

	if len(patch.Recurrence) != 1 {
		t.Fatalf("expected exactly the RRULE line, got %v", patch.Recurrence)
	}
	if patch.Recurrence[0] != "RRULE:FREQ=WEEKLY" {
		t.Fatalf("expected the RRULE line verbatim, got %q", patch.Recurrence[0])
	}
	for _, line := range patch.Recurrence {
		if len(line) >= 6 && line[:6] == "EXDATE" {
			t.Fatalf("a Master edit's recurrence array must never carry an EXDATE line, got %q", line)
		}
	}
}

// TestBuildGooglePatch_ClearedURLSendsExplicitNullSource covers Event URL's
// removal: an empty local URL must marshal to an explicit "source":null so
// Google actually clears its own source field, not an absent key (which
// Google reads as "leave whatever's there alone").
func TestBuildGooglePatch_ClearedURLSendsExplicitNullSource(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	patch := buildGooglePatch("Standup", start, end, false, nil, "", "", "", "")
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	rawSource, ok := m["source"]
	if !ok {
		t.Fatalf(`expected an explicit "source" key clearing Google's own field, got %s`, raw)
	}
	if rawSource != nil {
		t.Fatalf(`expected "source":null, got %v`, rawSource)
	}
}

// TestBuildGooglePatch_NonHTTPEventURLIsNeverSent covers a native-scheme
// Event URL (message://, a feed vendor scheme): CONTEXT.md's Event URL entry
// says such a link is offered as a click-through only when it's http(s), and
// Google's own source.url has no use for a link nothing there can open
// either — so it's dropped rather than pushed and rejected.
func TestBuildGooglePatch_NonHTTPEventURLIsNeverSent(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	patch := buildGooglePatch("Standup", start, end, false, nil, "", "", "", "message://abc")
	if patch.Source != nil {
		t.Fatalf("expected no source for a non-http Event URL, got %+v", patch.Source)
	}
}

// TestBuildGooglePatch_GoldenWireBodies pins the exact bytes every Write-back
// push sends, one case per encoding shape. It exists because its absence is
// what let Write-back ship broken: the assertions above this one check which
// top-level keys the body carries and never descend into `start`/`end`, so a
// shared decode struct with no `omitempty` shipped `"date":""` beside every
// populated `dateTime` and Google rejected every push with 400 — an edit, a
// create, an instance patch and a cancellation alike, since all four build
// their boundaries through encodeGoogleEventDateTime.
//
// The rule those bytes encode is stated in googleEventDateTimePatchJSON's own
// doc comment: `date` and `dateTime` are mutually exclusive at Google, exactly
// one is sent, and the unused half is an explicit null rather than an absent
// key so that editing a timed Event into an all-day one actually clears the
// stale value. A golden is the right shape for this because the failure mode is
// a change to the *wire format* that no type-level assertion can see; it should
// be updated deliberately, in a diff a reviewer can read, and never loosened
// into a partial match.
func TestBuildGooglePatch_GoldenWireBodies(t *testing.T) {
	sarajevo := "Europe/Sarajevo"
	start := time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		patch googleEventPatchBody
		want  string
	}{
		{
			name:  "timed with an Anchor zone sends dateTime and timeZone, date explicitly null",
			patch: buildGooglePatch("Standup", start, end, false, &sarajevo, "", "", "", ""),
			want:  `{"summary":"Standup","description":"","location":"","start":{"date":null,"dateTime":"2026-09-10T16:00:00+02:00","timeZone":"Europe/Sarajevo"},"end":{"date":null,"dateTime":"2026-09-10T17:00:00+02:00","timeZone":"Europe/Sarajevo"},"source":null}`,
		},
		{
			name:  "all-day sends a bare date, dateTime explicitly null and no zone at all",
			patch: buildGooglePatch("Holiday", start, end, true, &sarajevo, "", "", "", ""),
			want:  `{"summary":"Holiday","description":"","location":"","start":{"date":"2026-09-10","dateTime":null},"end":{"date":"2026-09-10","dateTime":null},"source":null}`,
		},
		{
			// A Floating Event (ADR-0019) has no zone to round-trip through, so
			// it travels as a "Z" instant with timeZone omitted entirely.
			name:  "floating sends a Z instant with no timeZone key",
			patch: buildGooglePatch("Floats", start, end, false, nil, "", "", "", ""),
			want:  `{"summary":"Floats","description":"","location":"","start":{"date":null,"dateTime":"2026-09-10T14:00:00Z"},"end":{"date":null,"dateTime":"2026-09-10T15:00:00Z"},"source":null}`,
		},
		{
			// The recurrence array is exactly the one RRULE line — never an
			// EXDATE, which ADR-0078 forbids as a second representation of a
			// cancellation Google already models as a cancelled instance.
			name:  "recurring adds exactly one RRULE line",
			patch: buildGooglePatch("Weekly", start, end, false, &sarajevo, "FREQ=WEEKLY", "", "", ""),
			want:  `{"summary":"Weekly","description":"","location":"","start":{"date":null,"dateTime":"2026-09-10T16:00:00+02:00","timeZone":"Europe/Sarajevo"},"end":{"date":null,"dateTime":"2026-09-10T17:00:00+02:00","timeZone":"Europe/Sarajevo"},"recurrence":["RRULE:FREQ=WEEKLY"],"source":null}`,
		},
		{
			name:  "an Event URL becomes a source object",
			patch: buildGooglePatch("Linked", start, end, false, &sarajevo, "", "", "", "https://example.com/x"),
			want:  `{"summary":"Linked","description":"","location":"","start":{"date":null,"dateTime":"2026-09-10T16:00:00+02:00","timeZone":"Europe/Sarajevo"},"end":{"date":null,"dateTime":"2026-09-10T17:00:00+02:00","timeZone":"Europe/Sarajevo"},"source":{"title":"Linked","url":"https://example.com/x"}}`,
		},
		{
			// Clearing Event URL is why Source is not omitempty: an absent key
			// would read to Google as "leave whatever is there alone".
			name:  "no Event URL sends source as an explicit null, not an absent key",
			patch: buildGooglePatch("Bare", start, end, false, nil, "", "some notes", "a room", ""),
			want:  `{"summary":"Bare","description":"some notes","location":"a room","start":{"date":null,"dateTime":"2026-09-10T14:00:00Z"},"end":{"date":null,"dateTime":"2026-09-10T15:00:00Z"},"source":null}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.patch)
			if err != nil {
				t.Fatalf("marshal patch: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("wire body mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// TestGoogleEventDateTimePatchJSON_RefusesAnAmbiguousBoundary covers the
// invariant the golden bodies above are instances of: a boundary carrying both
// date and dateTime, or neither, is the malformed pair Google answers with 400,
// so it fails at the seam rather than travelling. Enforcing it in MarshalJSON
// is what makes "exactly one of the pair" true by construction — there is no
// call site that can opt out of it.
func TestGoogleEventDateTimePatchJSON_RefusesAnAmbiguousBoundary(t *testing.T) {
	tests := []struct {
		name string
		dt   googleEventDateTimePatchJSON
	}{
		{name: "both set is the pair Google rejects", dt: googleEventDateTimePatchJSON{Date: "2026-09-10", DateTime: "2026-09-10T14:00:00Z"}},
		{name: "neither set carries no boundary at all", dt: googleEventDateTimePatchJSON{TimeZone: "Europe/Sarajevo"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := json.Marshal(tc.dt); err == nil {
				t.Fatal("expected marshalling an ambiguous boundary to fail, got nil error")
			}
		})
	}
}
