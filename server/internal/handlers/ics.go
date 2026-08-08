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

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/service"
)

// calendarFileTarget builds the icalendar.SerializationTarget every export
// call site renders ATTACH with (ADR-0041): bytes inlined from store, capped
// at maxImportUploadBytes — the same cap the ICS importer enforces, so a
// file this instance produces is a file it could accept back.
func calendarFileTarget(store *attachmentstore.Store) icalendar.SerializationTarget {
	return icalendar.CalendarFileTarget(maxImportUploadBytes, func(id string) (io.ReadCloser, error) {
		return store.Open(id)
	})
}

var occurrenceErrors = alsoHandling(eventNotFoundErrors,
	errorCase{service.ErrOccurrenceNotFound, notFound("occurrence not found")},
)

// ICS serves GET /api/events/{id}/ics: the whole calendar object
// (scope=all, the default) or one flattened Occurrence (scope=occurrence,
// requiring occurrenceStart).
func (h *EventHandler) ICS(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "all"
	}

	switch scope {
	case "all":
		h.icsAll(w, r, userID, id)
	case "occurrence":
		h.icsOccurrence(w, r, userID, id)
	default:
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", `scope must be "all" or "occurrence"`)
	}
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
	// bytes are inlined rather than referenced.
	cal, err := icalendar.SeriesToICal(master, overrides, calendarFileTarget(h.attachments))
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build calendar object")
		return
	}
	writeICS(w, master.Title, cal)
}

// icsOccurrence renders the named Occurrence flattened: a fresh UID, no
// RRULE, no RECURRENCE-ID, concrete start/end, with the matching Override's
// fields substituted when one exists at that recurrence id (#76).
func (h *EventHandler) icsOccurrence(w http.ResponseWriter, r *http.Request, userID int64, id string) {
	raw := r.URL.Query().Get("occurrenceStart")
	if raw == "" {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "occurrenceStart is required for scope=occurrence")
		return
	}
	occurrenceStart, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "occurrenceStart must be an RFC3339 timestamp")
		return
	}

	occurrence, err := h.events.GetOccurrence(r.Context(), userID, id, occurrenceStart)
	if respondError(w, err, occurrenceErrors, "failed to load occurrence") {
		return
	}

	cal, err := icalendar.OccurrenceToICal(uuid.NewString(), occurrence)
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
