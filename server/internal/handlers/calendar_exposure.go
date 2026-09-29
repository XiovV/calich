// calendar_exposure.go implements CalendarHandler's Exposure endpoint
// (ADR-0080): a User's own choice about whether a Calendar appears in their
// own CalDAV home-set, on its own read/write path beside the Calendar's
// other fields — same posture as the colour override folded into Update and
// Default reminders' own endpoint (open to any User with Access, not
// Owner-only), kept separate from Update because Exposure has no
// "the Calendar's own" value for an Owner's write to land on instead: every
// principal, Owner included, only ever writes their own row.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
)

type setExposureRequest struct {
	Exposed bool `json:"exposed"`
}

type exposureResponse struct {
	Exposed bool `json:"exposed"`
}

// SetExposure serves PUT /api/calendars/{id}/exposure: sets the caller's own
// Exposure choice on id — whether id appears in their own CalDAV home-set.
func (h *CalendarHandler) SetExposure(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	req, ok := decodeJSON[setExposureRequest](w, r)
	if !ok {
		return
	}

	if err := h.calendars.SetExposure(r.Context(), userID, id, req.Exposed); respondError(w, err, calendarNotFoundErrors, "failed to update calendar exposure") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, exposureResponse{Exposed: req.Exposed})
}
