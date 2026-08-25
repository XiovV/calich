// subscribe.go implements POST /api/calendars/subscribe (#83): a single
// endpoint mirroring the Import handler's dryRun convention exactly —
// dryRun=1 previews a URL (fetches, parses, proposes name/color, no
// writes), dryRun=0 (the default) commits it, creating a Subscribed
// Calendar and writing its Events synchronously.
package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/service"
)

var subscribeErrors = []errorCase{
	{service.ErrSubscribeInvalidURL, badRequest("subscription URL is invalid")},
	{service.ErrSubscribeFetchFailed, badRequest("failed to fetch the calendar feed")},
	{service.ErrSubscribeAuthFailed, badRequest("the calendar feed rejected the credentials")},
	{service.ErrSubscribeNotFound, badRequest("the calendar feed was not found")},
	{service.ErrSubscribeTooLarge, badRequest("the calendar feed exceeds the 10MB import limit")},
	{service.ErrSubscribeUnparseable, badRequest("the calendar feed could not be parsed as iCalendar")},
	{service.ErrInvalidTitle, badRequest("event title must not be empty")},
	{service.ErrInvalidTimeRange, badRequest("event end must be after start")},
	{service.ErrInvalidRecurrenceRule, badRequest("recurrence rule is invalid")},
}

var subscribeCommitErrors = alsoHandling(alsoHandling(subscribeErrors, calendarWriteErrors...), workspaceMembershipErrors...)

type subscribeRequest struct {
	URL   string `json:"url"`
	Name  string `json:"name"`
	Color string `json:"color"`
	// KeepAlarms is the Subscription's opt-in to honouring the feed's
	// VALARMs on both Channels (including Email), off by default (#87,
	// ADR-0032). Ignored on a preview, which never writes Events.
	KeepAlarms bool `json:"keepAlarms"`
}

type subscribePreviewResponse struct {
	Name       string     `json:"name"`
	Color      string     `json:"color"`
	EventCount int        `json:"eventCount"`
	RangeStart *time.Time `json:"rangeStart,omitempty"`
	RangeEnd   *time.Time `json:"rangeEnd,omitempty"`
}

// Subscribe serves POST /api/calendars/subscribe?dryRun=1|0: dryRun=1
// previews req.URL without writing anything; dryRun=0 fetches it again and
// commits — creating the Calendar with req.URL as its source and importing
// every series the feed holds (#83).
func (h *CalendarHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	// Resolved for both branches, even though only the commit path below
	// actually needs it — cheap, and keeps this handler's shape simple
	// (#155, ADR-0045).
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	dryRun, ok := parseDryRun(r.URL.Query().Get("dryRun"))
	if !ok {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", `dryRun must be "0" or "1"`)
		return
	}

	req, ok := decodeJSON[subscribeRequest](w, r)
	if !ok {
		return
	}

	if dryRun {
		preview, err := h.subscriptions.Preview(r.Context(), userID, req.URL)
		if respondError(w, err, subscribeErrors, "failed to preview subscription") {
			return
		}

		httpresponse.JSON(w, http.StatusOK, subscribePreviewResponse{
			Name:       preview.Name,
			Color:      preview.Color,
			EventCount: preview.EventCount,
			RangeStart: preview.RangeStart,
			RangeEnd:   preview.RangeEnd,
		})
		return
	}

	calendar, err := h.subscriptions.Subscribe(r.Context(), userID, workspaceID, req.URL, req.Name, req.Color, req.KeepAlarms)
	if respondError(w, err, subscribeCommitErrors, "failed to subscribe to calendar") {
		return
	}

	h.respondWithOwnership(w, r, http.StatusCreated, userID, calendar, "failed to subscribe to calendar")
}

var subscriptionRefreshErrors = []errorCase{
	{service.ErrRefreshNotSubscribed, badRequest("calendar is not a subscribed calendar")},
	{service.ErrSubscribeFetchFailed, badRequest("failed to fetch the calendar feed")},
	{service.ErrSubscribeAuthFailed, badRequest("the calendar feed rejected the credentials")},
	{service.ErrSubscribeNotFound, badRequest("the calendar feed was not found")},
	{service.ErrSubscribeTooLarge, badRequest("the calendar feed exceeds the 10MB import limit")},
	{service.ErrSubscribeUnparseable, badRequest("the calendar feed could not be parsed as iCalendar")},
	{service.ErrInvalidTitle, badRequest("event title must not be empty")},
	{service.ErrInvalidTimeRange, badRequest("event end must be after start")},
	{service.ErrInvalidRecurrenceRule, badRequest("recurrence rule is invalid")},
}

var subscriptionRefreshErrorsWithNotFound = alsoHandling(subscriptionRefreshErrors, calendarNotFoundErrors...)

type subscriptionRefreshResponse struct {
	NotModified bool `json:"notModified"`
	Created     int  `json:"created"`
	Updated     int  `json:"updated"`
	Tombstoned  int  `json:"tombstoned"`
	Unparseable int  `json:"unparseable"`
	NoOp        int  `json:"noOp"`
}

// Refresh serves POST /api/calendars/{id}/refresh (#85): a manual trigger
// for the on-demand equivalent of ADR-0033's Refresh, always forced — it
// bypasses the conditional-GET/content-hash short-circuit so the action is
// never a visible no-op even when the server believes the feed is
// unchanged.
func (h *CalendarHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	result, err := h.subscriptions.Refresh(r.Context(), userID, id, true)
	if respondError(w, err, subscriptionRefreshErrorsWithNotFound, "failed to refresh subscription") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, subscriptionRefreshResponse{
		NotModified: result.NotModified,
		Created:     result.Created,
		Updated:     result.Updated,
		Tombstoned:  result.Tombstoned,
		Unparseable: result.Unparseable,
		NoOp:        result.NoOp,
	})
}
