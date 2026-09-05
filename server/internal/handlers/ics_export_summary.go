// ics_export_summary.go implements the Export summary's pre-flight (#134,
// ADR-0041): which Attachments a would-be export would have to omit. It asks
// the encoder rather than restating its rule — the same encode the download
// performs, run against dryRunTarget so no bytes leave disk, and the calendar
// it produces discarded. The inline cap therefore lives in exactly one place,
// and the pre-flight cannot disagree with the download that follows it (#217).
package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
)

// maxOversizedSamples caps how many oversized Attachments an export
// pre-flight response names individually — the all-Calendars case can in
// principle turn up many, and the dialog that reads this is a count plus
// the first few, not an unbounded list. Count still reports the true total.
const maxOversizedSamples = 5

type oversizedAttachmentResponse struct {
	Filename   string `json:"filename"`
	SizeBytes  int64  `json:"sizeBytes"`
	EventTitle string `json:"eventTitle"`
	EventID    string `json:"eventId"`
}

type exportSummaryResponse struct {
	Oversized []oversizedAttachmentResponse `json:"oversized"`
	Count     int                           `json:"count"`
}

// toExportSummaryResponse renders the encoder's omissions on the wire,
// capping entries at maxOversizedSamples for display while keeping Count as
// the true total, so a caller with many oversized Attachments (the
// all-Calendars case especially) still learns how many there are without the
// response growing unbounded.
func toExportSummaryResponse(omitted []icalendar.OmittedAttachment) exportSummaryResponse {
	entries := make([]oversizedAttachmentResponse, 0, len(omitted))
	for _, o := range omitted {
		if len(entries) == maxOversizedSamples {
			break
		}
		entries = append(entries, oversizedAttachmentResponse{
			Filename:   o.Filename,
			SizeBytes:  o.SizeBytes,
			EventTitle: o.EventTitle,
			EventID:    o.EventID,
		})
	}
	return exportSummaryResponse{Oversized: entries, Count: len(omitted)}
}

// ICSOversizedAttachments serves GET /api/events/{id}/ics/oversized-attachments:
// the Export summary pre-flight for a single Event's download (#134,
// ADR-0041). It takes the same scope/occurrenceStart pair the download does
// and dry-runs the same encoder entrypoint, so its answer describes the
// download that follows it — including scope=occurrence, which drops nothing
// silently any more (#217).
func (h *EventHandler) ICSOversizedAttachments(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	scope, ok := parseICSScope(w, r)
	if !ok {
		return
	}

	var (
		omitted []icalendar.OmittedAttachment
		err     error
	)
	if scope.occurrence {
		var occurrence repository.Event
		occurrence, err = h.events.GetOccurrence(r.Context(), userID, id, scope.occurrenceStart)
		if respondError(w, err, occurrenceErrors, "failed to load occurrence") {
			return
		}
		_, omitted, err = icalendar.OccurrenceToICal(uuid.NewString(), occurrence, dryRunTarget())
	} else {
		var master repository.Event
		var overrides []repository.Event
		master, overrides, err = h.events.GetSeriesForEvent(r.Context(), userID, id)
		if respondError(w, err, eventNotFoundErrors, "failed to load event") {
			return
		}
		_, omitted, err = icalendar.SeriesToICal(master, overrides, dryRunTarget())
	}
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build calendar object")
		return
	}

	httpresponse.JSON(w, http.StatusOK, toExportSummaryResponse(omitted))
}

// omittedForCalendar is icsForCalendar's pre-flight twin: the same
// CalendarToICal encode over the same series, reporting what it omitted and
// discarding the calendar itself.
func (h *CalendarHandler) omittedForCalendar(ctx context.Context, userID int64, calendar repository.Calendar) ([]icalendar.OmittedAttachment, error) {
	masters, overridesByParent, err := h.events.ListSeriesByCalendar(ctx, userID, calendar.ID)
	if err != nil {
		return nil, err
	}

	_, omitted, err := icalendar.CalendarToICal(calendar.Name, calendar.Color, masters, overridesByParent, dryRunTarget())
	return omitted, err
}

// ICSOversizedAttachments serves GET /api/calendars/{id}/ics/oversized-attachments:
// the Export summary pre-flight for one Calendar's download (#134,
// ADR-0041).
func (h *CalendarHandler) ICSOversizedAttachments(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	calendar, err := h.calendars.Get(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to load calendar") {
		return
	}

	omitted, err := h.omittedForCalendar(r.Context(), userID, calendar)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build export summary")
		return
	}

	httpresponse.JSON(w, http.StatusOK, toExportSummaryResponse(omitted))
}

// ICSAllOversizedAttachments serves GET /api/calendars/ics/oversized-attachments:
// the Export summary pre-flight for the download-all zip (#134, ADR-0041) —
// every owned Calendar ICSAll itself would include, Subscribed Calendars
// excluded the same way (a frozen snapshot is the wrong artifact for one).
func (h *CalendarHandler) ICSAllOversizedAttachments(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	calendars, err := h.calendars.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list calendars")
		return
	}

	var omitted []icalendar.OmittedAttachment
	for _, calendar := range calendars {
		if calendar.Source != nil {
			continue
		}

		calendarOmitted, err := h.omittedForCalendar(r.Context(), userID, calendar)
		if err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build export summary")
			return
		}
		omitted = append(omitted, calendarOmitted...)
	}

	httpresponse.JSON(w, http.StatusOK, toExportSummaryResponse(omitted))
}
