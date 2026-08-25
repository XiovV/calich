// reminders.go implements EventHandler's Reminder endpoints (ADR-0064): a
// User's own Reminders on one Event, on their own read and write path
// rather than the Event create/update payload — same posture as the
// deleted /reminder-override endpoints, but for a plain personal Reminder
// list rather than a modifier over a shared one.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

var remindersErrors = []errorCase{
	{service.ErrInvalidReminderChannel, badRequest("reminder channel must be \"notification\" or \"email\"")},
	{service.ErrCalendarNotFound, notFound("event not found")},
	{repository.ErrNotFound, notFound("event not found")},
}

// GetReminders serves GET /api/events/{id}/reminders: the caller's own
// Reminders on the Event — empty, not an error, if they've never set any.
// Anyone who can see the Event may call this — Owner, Editor, Viewer, or a
// User-backed Attendee with no Calendar Access at all (#211).
func (h *EventHandler) GetReminders(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	reminders, err := h.events.GetReminders(r.Context(), userID, id)
	if respondError(w, err, remindersErrors, "failed to get reminders") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderWire(reminders))
}

type setRemindersRequest struct {
	Reminders []reminderWire `json:"reminders"`
}

// SetReminders serves PUT /api/events/{id}/reminders: replaces the caller's
// own Reminders on the Event wholesale, never touching another User's rows
// on the same Event (ADR-0064). Not an Event write, so it is not gated on
// write Access to the Event — the same caller set as GetReminders may call
// this (#211).
func (h *EventHandler) SetReminders(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	req, ok := decodeJSON[setRemindersRequest](w, r)
	if !ok {
		return
	}

	reminders, err := h.events.SetReminders(r.Context(), userID, id, fromReminderWire(req.Reminders))
	if respondError(w, err, remindersErrors, "failed to set reminders") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderWire(reminders))
}
