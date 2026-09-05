// connection_calendars.go implements the Calendar picker's HTTP surface
// (#286): GET lists everything a Connection's account can see, POST creates
// a Linked Calendar for each one the User confirmed. Both live on
// CalendarHandler rather than ConnectionHandler — a picked row becomes an
// ordinary Calendar, and reusing toCalendarResponse/respondWithOwnership
// here is what keeps a Linked Calendar's wire shape identical to every other
// Calendar's.
package handlers

import (
	"net/http"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/service"
)

var connectionCalendarsErrors = alsoHandling([]errorCase{
	{service.ErrGoogleNotConfigured, notFound("google provider is not configured on this instance")},
	{service.ErrConnectionNotFound, notFound("connection not found")},
	{service.ErrConnectionCalendarsUnavailable, badRequest("this connection has no usable google access token — reconnect and try again")},
	{service.ErrGoogleCalendarListFailed, badRequest("failed to reach google for this connection's calendars")},
}, workspaceMembershipErrors...)

type pickerCalendarResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Selected bool   `json:"selected"`
	Writable bool   `json:"writable"`
}

func toPickerCalendarResponse(c service.PickerCalendar) pickerCalendarResponse {
	return pickerCalendarResponse{
		ID:       c.ExternalID,
		Name:     c.Name,
		Color:    c.Color,
		Selected: c.Selected,
		Writable: c.Writable,
	}
}

// ListConnectionCalendars serves GET /api/connections/{id}/calendars (#286):
// the Calendar picker's read side — everything the account can see, with
// Google's own selected flag and accessRole surfaced for the picker to
// render before import.
func (h *CalendarHandler) ListConnectionCalendars(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	calendars, err := h.connections.ListCalendars(r.Context(), userID, id)
	if respondError(w, err, connectionCalendarsErrors, "failed to list this connection's calendars") {
		return
	}

	responses := make([]pickerCalendarResponse, len(calendars))
	for i, c := range calendars {
		responses[i] = toPickerCalendarResponse(c)
	}
	httpresponse.JSON(w, http.StatusOK, responses)
}

type importConnectionCalendarsRequest struct {
	CalendarIDs []string `json:"calendarIds"`
}

// ImportConnectionCalendars serves POST /api/connections/{id}/calendars
// (#286): the Calendar picker's write side — confirming turns every
// requested id into a Linked Calendar in the caller's active Workspace.
func (h *CalendarHandler) ImportConnectionCalendars(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[importConnectionCalendarsRequest](w, r)
	if !ok {
		return
	}

	calendars, err := h.connections.ImportCalendars(r.Context(), userID, workspaceID, id, req.CalendarIDs)
	if respondError(w, err, connectionCalendarsErrors, "failed to import calendars") {
		return
	}

	responses := make([]calendarResponse, len(calendars))
	for i, calendar := range calendars {
		isOwner, ownerName, shareCount, err := h.calendars.OwnershipMeta(r.Context(), userID, calendar)
		if err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to import calendars")
			return
		}
		responses[i] = toCalendarResponse(calendar, isOwner, ownerName, shareCount)
	}
	httpresponse.JSON(w, http.StatusCreated, responses)
}
