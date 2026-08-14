// calendar_default_reminders.go implements CalendarHandler's Default
// reminders endpoints (ADR-0064): a User's own timed/all-day default
// Reminder lists on one Calendar, on their own read/write path beside the
// Calendar's other fields — same posture as the colour override folded into
// Update, but as its own endpoint since a Default reminders write carries
// two independent lists rather than one value.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/service"
)

var defaultRemindersErrors = []errorCase{
	{service.ErrInvalidReminderChannel, badRequest("reminder channel must be \"notification\" or \"email\"")},
}

var defaultRemindersErrorsWithNotFound = alsoHandling(defaultRemindersErrors, calendarNotFoundErrors...)

// defaultRemindersResponse is GetDefaultReminders' wire shape — both of the
// caller's own default lists, so the Calendar edit modal can render its two
// Reminder sections from one fetch.
type defaultRemindersResponse struct {
	Timed  []reminderWire `json:"timed"`
	AllDay []reminderWire `json:"allDay"`
}

// GetDefaultReminders serves GET /api/calendars/{id}/default-reminders: the
// caller's own timed and all-day default Reminder lists — empty, not an
// error, if they've never set either.
func (h *CalendarHandler) GetDefaultReminders(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	timed, allDay, err := h.calendars.GetDefaultReminders(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to get default reminders") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, defaultRemindersResponse{Timed: toReminderWire(timed), AllDay: toReminderWire(allDay)})
}

type setDefaultRemindersRequest struct {
	AllDay    bool           `json:"allDay"`
	Reminders []reminderWire `json:"reminders"`
}

// SetDefaultReminders serves PUT /api/calendars/{id}/default-reminders:
// replaces the caller's own default Reminder list — timed or all-day,
// whichever the request names — wholesale, never touching the other list or
// another User's rows on the same Calendar (ADR-0064).
func (h *CalendarHandler) SetDefaultReminders(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req setDefaultRemindersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	reminders, err := h.calendars.SetDefaultReminders(r.Context(), userID, id, req.AllDay, fromReminderWire(req.Reminders))
	if respondError(w, err, defaultRemindersErrorsWithNotFound, "failed to set default reminders") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderWire(reminders))
}
