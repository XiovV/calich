// ics.go implements ICS export (#76): read-only endpoints that render an
// Event, a Calendar, or every Calendar as iCalendar/zip downloads, reusing
// internal/icalendar's codec rather than building a second one.
package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/attachmentstore"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/service"
)

// exportTarget builds the icalendar.SerializationTarget every export renders
// ATTACH with (ADR-0041): bytes inlined via open, capped at
// maxImportUploadBytes — the same cap the ICS importer enforces, so a file
// this instance produces is a file it could accept back. The cap is named
// here and nowhere else, so a download and the pre-flight that precedes it
// cannot be capped differently (#217).
func exportTarget(open icalendar.AttachmentOpener) icalendar.SerializationTarget {
	return icalendar.CalendarFileTarget(maxImportUploadBytes, open)
}

// calendarFileTarget is the target a download encodes with: the Attachment's
// real bytes, read from store.
func calendarFileTarget(store *attachmentstore.Store) icalendar.SerializationTarget {
	return exportTarget(func(id string) (io.ReadCloser, error) {
		return store.Open(id)
	})
}

// dryRunTarget is calendarFileTarget's metadata-only twin, and what the
// Export summary pre-flight encodes with (#217): the same target at the same
// cap, so it omits exactly what the download omits, but reading no bytes off
// disk. Sound because the codec decides an omission from an Attachment's
// recorded size before it ever consults the opener — so the pre-flight learns
// what the download would drop without paying to produce the file, which for
// the all-Calendars case is every Attachment on the instance read and
// base64'd to be thrown away.
func dryRunTarget() icalendar.SerializationTarget {
	return exportTarget(func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("")), nil
	})
}

var occurrenceErrors = alsoHandling(eventNotFoundErrors,
	errorCase{service.ErrOccurrenceNotFound, notFound("occurrence not found")},
)

// icsScope is the export scope GET /api/events/{id}/ics and its Export
// summary pre-flight both take: the whole Calendar object ("all", the
// default) or one flattened Occurrence ("occurrence", requiring
// occurrenceStart). Parsed in one place so the pre-flight can never describe
// a different download than the one that follows it (#217).
type icsScope struct {
	occurrence      bool
	occurrenceStart time.Time
}

// parseICSScope reads the scope/occurrenceStart query pair, responding 400
// and reporting false if either is missing or malformed.
func parseICSScope(w http.ResponseWriter, r *http.Request) (icsScope, bool) {
	switch r.URL.Query().Get("scope") {
	case "", "all":
		return icsScope{}, true
	case "occurrence":
		raw := r.URL.Query().Get("occurrenceStart")
		if raw == "" {
			httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "occurrenceStart is required for scope=occurrence")
			return icsScope{}, false
		}
		occurrenceStart, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "occurrenceStart must be an RFC3339 timestamp")
			return icsScope{}, false
		}
		return icsScope{occurrence: true, occurrenceStart: occurrenceStart}, true
	default:
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", `scope must be "all" or "occurrence"`)
		return icsScope{}, false
	}
}

// ICS serves GET /api/events/{id}/ics: the whole calendar object
// (scope=all, the default) or one flattened Occurrence (scope=occurrence,
// requiring occurrenceStart).
func (h *EventHandler) ICS(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	scope, ok := parseICSScope(w, r)
	if !ok {
		return
	}
	if scope.occurrence {
		h.icsOccurrence(w, r, userID, id, scope.occurrenceStart)
		return
	}
	h.icsAll(w, r, userID, id)
}

// icsAll renders id's whole Calendar object — Master + Overrides + EXDATEs —
// straight from the codec (#76). id may name either a Master or an
// Override; both resolve to the same series.
func (h *EventHandler) icsAll(w http.ResponseWriter, r *http.Request, userID int64, id string) {
	master, overrides, err := h.events.GetSeriesForEvent(r.Context(), userID, id)
	if respondError(w, err, eventNotFoundErrors, "failed to load event") {
		return
	}

	// This standalone .ics is a Calendar file (ADR-0041): its Attachments'
	// bytes are inlined rather than referenced. What the encode omitted was
	// already disclosed by the Export summary pre-flight the user confirmed.
	cal, _, err := icalendar.SeriesToICal(master, overrides, calendarFileTarget(h.attachments))
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build calendar object")
		return
	}
	writeICS(w, master.Title, cal)
}

// icsOccurrence renders the named Occurrence flattened: a fresh UID, no
// RRULE, no RECURRENCE-ID, concrete start/end, with the matching Override's
// fields substituted when one exists at that recurrence id (#76). A Calendar
// file either way, so its series' Attachments are inlined exactly as
// scope=all inlines them (#217, ADR-0041).
func (h *EventHandler) icsOccurrence(w http.ResponseWriter, r *http.Request, userID int64, id string, occurrenceStart time.Time) {
	occurrence, err := h.events.GetOccurrence(r.Context(), userID, id, occurrenceStart)
	if respondError(w, err, occurrenceErrors, "failed to load occurrence") {
		return
	}

	cal, _, err := icalendar.OccurrenceToICal(uuid.NewString(), occurrence, calendarFileTarget(h.attachments))
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build calendar object")
		return
	}
	writeICS(w, occurrence.Title, cal)
}

// writeICS encodes cal and writes it as a text/calendar download named
// after name (sanitized, falling back to "event" if name is blank).
func writeICS(w http.ResponseWriter, name string, cal *ical.Calendar) {
	body, err := icalendar.Encode(cal)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to encode calendar object")
		return
	}
	httpresponse.ICS(w, sanitizeICSFilename(name, "event")+".ics", body)
}

// sanitizeICSFilename renders name as a safe single-segment filename
// component: blank (after trimming) falls back to fallback, and "/", "\",
// and control characters — unsafe in a Content-Disposition filename or a
// zip entry name — are replaced with "-".
func sanitizeICSFilename(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallback
	}

	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			b.WriteRune('-')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// uniqueZipEntryName disambiguates name against every name already seen in
// used (tracked by the caller across one archive) by suffixing "-2", "-3",
// etc. before name's extension — Calendars are not required to have unique
// names, but zip entries in one archive should be.
func uniqueZipEntryName(used map[string]int, name string) string {
	used[name]++
	if used[name] == 1 {
		return name
	}

	base, ext := name, ""
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		base, ext = name[:i], name[i:]
	}
	return fmt.Sprintf("%s-%d%s", base, used[name], ext)
}
