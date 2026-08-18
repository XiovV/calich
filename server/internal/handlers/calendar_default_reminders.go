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
	"github.com/XiovV/calendar/server/internal/repository"
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

// reminderList is toReminderWire for a response field that is always a list
// rather than an optional one: a User who has never set a default has an
// empty list, and an empty list serializes as [] here, not null. toReminderWire
// returns a nil slice for empty, which is right where the field is omitempty-
// shaped (an Event's reminders) and wrong here, where the client maps over
// whatever arrives.
func reminderList(reminders []repository.Reminder) []reminderWire {
	wire := toReminderWire(reminders)
	if wire == nil {
		return []reminderWire{}
	}
	return wire
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

	httpresponse.JSON(w, http.StatusOK, defaultRemindersResponse{Timed: reminderList(timed), AllDay: reminderList(allDay)})
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

	// Every Event on id now resolves differently for userID (ADR-0064): bump
	// its Calendar's CTag so their devices notice on next sync (#213). The
	// default write itself already committed by this point, so a failure
	// here means the write stands but the client must retry to have their
	// devices pick it up.
	if err := h.events.BumpCalendarChangeSeq(r.Context(), userID, id); err != nil {
		respondError(w, err, defaultRemindersErrorsWithNotFound, "failed to refresh calendar sync state")
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderWire(reminders))
}
