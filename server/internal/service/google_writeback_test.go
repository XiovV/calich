package service

import (
	"encoding/json"
	"testing"
	"time"
)

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

	patch := buildGooglePatch("Standup (renamed)", start, end, false, nil, "", nil, "", "", "")

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

	patch := buildGooglePatch("Standup", start, end, false, nil, "", nil, "", "", "")
	raw, _ := json.Marshal(patch)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)

	if _, ok := m["recurrence"]; ok {
		t.Fatalf("expected no recurrence key for a non-recurring event, got %s", raw)
	}
}

// TestBuildGooglePatch_RecurrenceCarriesRruleAndStoredExdates covers a
// Master edit's recurrence array: Google replaces it wholesale, so
// buildGooglePatch must fold in every stored Exception alongside the RRULE
// line, or a Master edit would silently resurrect every occurrence this app
// already cancelled.
func TestBuildGooglePatch_RecurrenceCarriesRruleAndStoredExdates(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	tzid := "America/New_York"
	exdate := time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC)

	patch := buildGooglePatch("Standup", start, end, false, &tzid, "FREQ=WEEKLY", []time.Time{exdate}, "", "", "")

	if len(patch.Recurrence) != 2 {
		t.Fatalf("expected an RRULE line and one EXDATE line, got %v", patch.Recurrence)
	}
	if patch.Recurrence[0] != "RRULE:FREQ=WEEKLY" {
		t.Fatalf("expected the RRULE line first and verbatim, got %q", patch.Recurrence[0])
	}
}

// TestBuildGooglePatch_ClearedURLSendsExplicitNullSource covers Event URL's
// removal: an empty local URL must marshal to an explicit "source":null so
// Google actually clears its own source field, not an absent key (which
// Google reads as "leave whatever's there alone").
func TestBuildGooglePatch_ClearedURLSendsExplicitNullSource(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)

	patch := buildGooglePatch("Standup", start, end, false, nil, "", nil, "", "", "")
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

	patch := buildGooglePatch("Standup", start, end, false, nil, "", nil, "", "", "message://abc")
	if patch.Source != nil {
		t.Fatalf("expected no source for a non-http Event URL, got %+v", patch.Source)
	}
}
