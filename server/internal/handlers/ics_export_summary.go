// ics_export_summary.go implements the Export summary's pre-flight (#134,
// ADR-0041): a metadata-only check of which Attachments a would-be export
// would have to omit — over maxImportUploadBytes, the same cap the encoder
// itself enforces (icalendar.CalendarFileTarget) — computed entirely from
// Attachment rows already loaded for the real export (event_attachments'
// size_bytes column), generating no file and reading no bytes off disk.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
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

// oversizedForEvent returns one oversizedAttachmentResponse per Attachment
// on attachments over maxImportUploadBytes, attributed to (eventTitle,
// eventID) — attachments always belong to a Master (ADR-0040), so callers
// pass the Master's own title/id.
func oversizedForEvent(attachments []repository.Attachment, eventTitle, eventID string) []oversizedAttachmentResponse {
	var out []oversizedAttachmentResponse
	for _, a := range attachments {
		if a.SizeBytes <= maxImportUploadBytes {
			continue
		}
		out = append(out, oversizedAttachmentResponse{
			Filename:   a.Filename,
			SizeBytes:  a.SizeBytes,
			EventTitle: eventTitle,
			EventID:    eventID,
		})
	}
	return out
}

// toExportSummaryResponse caps entries at maxOversizedSamples for display
// while keeping Count as the true total, so a caller with many oversized
// Attachments (the all-Calendars case especially) still learns how many
// there are without the response growing unbounded.
func toExportSummaryResponse(entries []oversizedAttachmentResponse) exportSummaryResponse {
	if len(entries) > maxOversizedSamples {
		return exportSummaryResponse{Oversized: entries[:maxOversizedSamples], Count: len(entries)}
	}
	return exportSummaryResponse{Oversized: entries, Count: len(entries)}
}

// ICSOversizedAttachments serves GET /api/events/{id}/ics/oversized-attachments:
// the Export summary pre-flight for a single Event's download (#134,
// ADR-0041).
func (h *EventHandler) ICSOversizedAttachments(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	master, _, err := h.events.GetSeriesForEvent(r.Context(), userID, id)
	if respondError(w, err, eventNotFoundErrors, "failed to load event") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toExportSummaryResponse(oversizedForEvent(master.Attachments, master.Title, master.ID)))
}

// ICSOversizedAttachments serves GET /api/calendars/{id}/ics/oversized-attachments:
// the Export summary pre-flight for one Calendar's download (#134,
// ADR-0041).
func (h *CalendarHandler) ICSOversizedAttachments(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	calendar, err := h.calendars.Get(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to load calendar") {
		return
	}

	masters, _, err := h.events.ListSeriesByCalendar(r.Context(), userID, calendar.ID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list events")
		return
	}

	var entries []oversizedAttachmentResponse
	for _, master := range masters {
		entries = append(entries, oversizedForEvent(master.Attachments, master.Title, master.ID)...)
	}

	httpresponse.JSON(w, http.StatusOK, toExportSummaryResponse(entries))
}

// ICSAllOversizedAttachments serves GET /api/calendars/ics/oversized-attachments:
// the Export summary pre-flight for the download-all zip (#134, ADR-0041) —
// every owned Calendar ICSAll itself would include, Subscribed Calendars
// excluded the same way (a frozen snapshot is the wrong artifact for one).
func (h *CalendarHandler) ICSAllOversizedAttachments(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	calendars, err := h.calendars.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list calendars")
		return
	}

	var entries []oversizedAttachmentResponse
	for _, calendar := range calendars {
		if calendar.SourceURL != nil {
			continue
		}

		masters, _, err := h.events.ListSeriesByCalendar(r.Context(), userID, calendar.ID)
		if err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list events")
			return
		}
		for _, master := range masters {
			entries = append(entries, oversizedForEvent(master.Attachments, master.Title, master.ID)...)
		}
	}

	httpresponse.JSON(w, http.StatusOK, toExportSummaryResponse(entries))
}
