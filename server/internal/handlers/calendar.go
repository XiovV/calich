package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/attachmentstore"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type CalendarHandler struct {
	calendars     *service.CalendarService
	events        *service.EventService
	imports       *service.ImportService
	subscriptions *service.SubscribeService
	attachments   *attachmentstore.Store
}

func NewCalendarHandler(calendars *service.CalendarService, events *service.EventService, imports *service.ImportService, subscriptions *service.SubscribeService, attachments *attachmentstore.Store) *CalendarHandler {
	return &CalendarHandler{calendars: calendars, events: events, imports: imports, subscriptions: subscriptions, attachments: attachments}
}

type calendarResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	// SourceURL is set only on a Subscribed Calendar (#83, ADR-0032), and
	// always masked — every display surface must hide a URL's password.
	SourceURL *string `json:"sourceUrl,omitempty"`
	// LastSyncedAt is when a Refresh last completed successfully against
	// SourceURL, whether or not it changed anything — nil until the first
	// Refresh runs, and always nil for an ordinary Calendar (#85, ADR-0033).
	LastSyncedAt *time.Time `json:"lastSyncedAt,omitempty"`
	// ErrorClass is "needs_attention" or "retrying" when the Subscription's
	// last Refresh attempt failed, nil while healthy or for an ordinary
	// Calendar — the sidebar's broken-Subscription indicator reads both this
	// and ErrorMessage (#86, ADR-0033).
	ErrorClass *string `json:"errorClass,omitempty"`
	// ErrorMessage is the human-readable reason behind ErrorClass, nil while
	// healthy.
	ErrorMessage *string `json:"errorMessage,omitempty"`
	// KeepAlarms is a Subscribed Calendar's opt-in to honouring the feed's
	// VALARMs on both Channels, off by default and meaningless on an
	// ordinary Calendar (#87, ADR-0032).
	KeepAlarms bool `json:"keepAlarms"`
	// Access is the caller's resolved Access to this Calendar (ADR-0034):
	// "owner", "editor", or "viewer" — never "none", since a Calendar the
	// caller has no Access to is never listed at all. Drives what the web
	// app lets the caller do with a Calendar it did not create.
	Access string `json:"access"`
	// IsOwner is un-clamped ownership (ADR-0034) — true for the Owner even
	// of a Subscribed Calendar, whose Access is clamped to "viewer". The
	// only correct basis for gating Calendar management: rename, recolour,
	// download, delete, Subscription editing, refresh, and sharing (#111).
	IsOwner bool `json:"isOwner"`
	// OwnerName is the Calendar's Owner's display Name — a
	// non-Owner may not list a Calendar's Shares to derive it themselves
	// (#111).
	OwnerName string `json:"ownerName"`
	// ShareCount is how many Shares the Calendar carries — whether more
	// than one person would be notified (#111).
	ShareCount int `json:"shareCount"`
}

func toCalendarResponse(c repository.Calendar, isOwner bool, ownerName string, shareCount int) calendarResponse {
	// respondWithOwnership's callers (Create, Update, Subscribe) always hand
	// back the caller's own Calendar, so Access is Owner — except a just-
	// Subscribed Calendar, whose SourceURL clamps it to Viewer even for its
	// Owner (ADR-0032), same as ResolveAccess applies for every other caller.
	access := service.AccessOwner
	if c.SourceURL != nil {
		access = service.AccessViewer
	}
	response := calendarResponse{ID: c.ID, Name: c.Name, Color: c.Color, LastSyncedAt: c.LastSyncedAt, ErrorClass: c.ErrorClass, ErrorMessage: c.ErrorMessage, KeepAlarms: c.KeepAlarms, Access: access.String(), IsOwner: isOwner, OwnerName: ownerName, ShareCount: shareCount}
	if c.SourceURL != nil {
		masked := service.MaskURL(*c.SourceURL)
		response.SourceURL = &masked
	}
	return response
}

func toCalendarWithAccessResponse(c service.CalendarWithAccess) calendarResponse {
	response := toCalendarResponse(c.Calendar, c.IsOwner, c.OwnerName, c.ShareCount)
	response.Access = c.Access.String()
	// c.Color is the caller's resolved display colour (ADR-0038) — their
	// own override if they've set one, otherwise the Calendar's own,
	// already copied in by toCalendarResponse above. Overwriting it here
	// is what makes a shared Calendar show green to one caller and blue to
	// another.
	response.Color = c.Color
	return response
}

// respondWithOwnership writes calendar as a calendarResponse carrying
// userID's ownership metadata (#111) — the shared tail of Create, Update,
// and Subscribe, which all hand back a freshly written Calendar the caller
// owns. Writes its own internal_error response and returns false if
// resolving that metadata fails, so callers can just `return` on false.
func (h *CalendarHandler) respondWithOwnership(w http.ResponseWriter, r *http.Request, status int, userID int64, calendar repository.Calendar, failMsg string) bool {
	isOwner, ownerName, shareCount, err := h.calendars.OwnershipMeta(r.Context(), userID, calendar)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", failMsg)
		return false
	}
	httpresponse.JSON(w, status, toCalendarResponse(calendar, isOwner, ownerName, shareCount))
	return true
}

// calendarWriteErrors is the rendering shared by the create and update paths,
// which validate the same two fields.
var calendarWriteErrors = []errorCase{
	{service.ErrInvalidName, badRequest("name must not be empty")},
	{service.ErrInvalidColor, badRequest("color must be a valid hex color (#RGB, #RRGGBB, or #RRGGBBAA)")},
}

var calendarNotFoundErrors = []errorCase{
	{repository.ErrNotFound, notFound("calendar not found")},
}

// workspaceMembershipErrors renders service.ErrNotWorkspaceMember as a 403 —
// the caller's active Workspace, resolved from the X-Workspace-Id header,
// named a Workspace they don't belong to (#155, ADR-0045).
var workspaceMembershipErrors = []errorCase{
	{service.ErrNotWorkspaceMember, forbidden("not a member of this workspace")},
}

var createCalendarErrors = alsoHandling(calendarWriteErrors, workspaceMembershipErrors...)

func (h *CalendarHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	calendars, err := h.calendars.ListAccessibleInWorkspace(r.Context(), userID, workspaceID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list calendars")
		return
	}

	response := make([]calendarResponse, len(calendars))
	for i, c := range calendars {
		response[i] = toCalendarWithAccessResponse(c)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

type createCalendarRequest struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *CalendarHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	req, ok := decodeJSON[createCalendarRequest](w, r)
	if !ok {
		return
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a valid UUID")
		return
	}

	calendar, err := h.calendars.Create(r.Context(), userID, workspaceID, req.ID, service.CalendarWrite{Name: req.Name, Color: req.Color})
	if respondError(w, err, createCalendarErrors, "failed to create calendar") {
		return
	}

	h.respondWithOwnership(w, r, http.StatusCreated, userID, calendar, "failed to create calendar")
}

func (h *CalendarHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	result, err := h.calendars.AccessWithColor(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to load calendar") {
		return
	}
	if !result.Access.CanRead() {
		httpresponse.Error(w, http.StatusNotFound, "not_found", "calendar not found")
		return
	}

	httpresponse.JSON(w, http.StatusOK, toCalendarWithAccessResponse(result))
}

type updateCalendarRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	// KeepAlarms is a Subscribed Calendar's KeepAlarms toggle (#87,
	// ADR-0032). A pointer so an ordinary name/color edit — which never
	// sends this field — can't accidentally turn a Subscription's alarms
	// off; only a request that names it explicitly reaches
	// SubscribeService.UpdateKeepAlarms.
	KeepAlarms *bool `json:"keepAlarms,omitempty"`
	// URL edits a Subscribed Calendar's feed URL (#88, ADR-0032) — a
	// pointer for the same reason as KeepAlarms: an ordinary edit never
	// sends it, so it can't accidentally clobber a Subscription's URL with
	// an empty string.
	URL *string `json:"url,omitempty"`
}

var updateKeepAlarmsErrors = []errorCase{
	{service.ErrRefreshNotSubscribed, badRequest("calendar is not a subscribed calendar")},
}

var updateKeepAlarmsErrorsWithNotFound = alsoHandling(updateKeepAlarmsErrors, calendarNotFoundErrors...)

var updateSourceURLErrors = []errorCase{
	{service.ErrRefreshNotSubscribed, badRequest("calendar is not a subscribed calendar")},
	{service.ErrSubscribeInvalidURL, badRequest("subscription URL is invalid")},
}

var updateSourceURLErrorsWithNotFound = alsoHandling(updateSourceURLErrors, calendarNotFoundErrors...)

// calendarFieldForbiddenErrors renders CalendarService.Update's
// ErrCalendarFieldForbidden (#272) — a non-Owner's write that reaches beyond
// their own Color, which stays Owner-only management (ADR-0034).
var calendarFieldForbiddenErrors = []errorCase{
	{service.ErrCalendarFieldForbidden, forbidden("only the calendar's owner may change its name, subscription url, or feed alarms setting")},
}

var updateCalendarErrors = alsoHandling(alsoHandling(calendarWriteErrors, calendarNotFoundErrors...), calendarFieldForbiddenErrors...)

// Update writes id: CalendarService.Update decides, and enforces, which
// fields the caller may actually touch (#272) — a non-Owner reaching beyond
// their own Color comes back as ErrCalendarFieldForbidden, rendered as a 403
// by updateCalendarErrors, with no field-permission logic left here. What
// remains is purely routing to the right write path's follow-up: the
// returned Calendar's own UserID says whether CalendarService.Update took
// the Owner path (updateOwner, which still has Subscription URL/KeepAlarms
// edits of its own to layer in) or the colour-override path
// (updateColorOverride, whose write is already done).
func (h *CalendarHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	req, ok := decodeJSON[updateCalendarRequest](w, r)
	if !ok {
		return
	}

	calendar, err := h.calendars.Update(r.Context(), userID, id, service.CalendarWrite{Name: req.Name, Color: req.Color}, req.URL != nil, req.KeepAlarms != nil)
	if respondError(w, err, updateCalendarErrors, "failed to update calendar") {
		return
	}

	if calendar.UserID != userID {
		h.updateColorOverride(w, r, userID, id)
		return
	}
	h.updateOwner(w, r, userID, id, req, calendar)
}

// updateOwner is Update's Owner write path: CalendarService.Update has
// already renamed/recoloured id by the time this runs, so all that's left is
// layering in a Subscription's URL/KeepAlarms edits (#87, #88, ADR-0032) —
// each its own service call, since neither lives on CalendarWrite — before
// responding with the fully updated Calendar.
func (h *CalendarHandler) updateOwner(w http.ResponseWriter, r *http.Request, userID int64, id string, req updateCalendarRequest, calendar repository.Calendar) {
	var err error
	if req.URL != nil {
		calendar, err = h.subscriptions.UpdateSourceURL(r.Context(), userID, id, *req.URL)
		if respondError(w, err, updateSourceURLErrorsWithNotFound, "failed to update subscription url") {
			return
		}
	}

	if req.KeepAlarms != nil {
		calendar, err = h.subscriptions.UpdateKeepAlarms(r.Context(), userID, id, *req.KeepAlarms)
		if respondError(w, err, updateKeepAlarmsErrorsWithNotFound, "failed to update keep alarms") {
			return
		}
	}

	h.respondWithOwnership(w, r, http.StatusOK, userID, calendar, "failed to update calendar")
}

// updateColorOverride is Update's non-Owner write path: CalendarService.Update
// has already written (or cleared) userID's own colour override on id by the
// time this runs, so all that's left is resolving and returning the
// caller's own view of id (ADR-0038) — never the raw Calendar
// CalendarService.Update handed back, which still carries the Owner's own
// stored colour rather than userID's override.
func (h *CalendarHandler) updateColorOverride(w http.ResponseWriter, r *http.Request, userID int64, id string) {
	result, err := h.calendars.AccessWithColor(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to update calendar") {
		return
	}
	httpresponse.JSON(w, http.StatusOK, toCalendarWithAccessResponse(result))
}

func (h *CalendarHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	err := h.calendars.Delete(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to delete calendar") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ErrCalendarEmpty means a Calendar holds no Events, so CalendarToICal built
// a VCALENDAR with no VEVENT/VTIMEZONE children — go-ical's Encode refuses
// to render that. The per-Calendar download (ICS) treats it as a rejection;
// the bulk export (ICSAll) passes allowEmpty so it still gets an entry, via
// icalendar.EncodeEmpty (#92).
var ErrCalendarEmpty = errors.New("calendar has no events")

// icsForCalendar builds one Calendar's whole export VCALENDAR: every series
// it holds, unbounded window (#76). allowEmpty distinguishes the two
// callers: ICS rejects an empty Calendar (ErrCalendarEmpty) since handing
// someone a file with nothing in it isn't useful, while ICSAll must never
// fail a whole export over one empty owned Calendar and so asks for the
// childless entry instead (#92).
func (h *CalendarHandler) icsForCalendar(ctx context.Context, userID int64, calendar repository.Calendar, allowEmpty bool) ([]byte, error) {
	masters, overridesByParent, err := h.events.ListSeriesByCalendar(ctx, userID, calendar.ID)
	if err != nil {
		return nil, err
	}

	// What the encode omitted was already disclosed by the Export summary
	// pre-flight the user confirmed (omittedForCalendar, ADR-0041).
	cal, _, err := icalendar.CalendarToICal(calendar.Name, calendar.Color, masters, overridesByParent, calendarFileTarget(h.attachments))
	if err != nil {
		return nil, err
	}

	if len(cal.Children) == 0 {
		if !allowEmpty {
			return nil, ErrCalendarEmpty
		}
		return icalendar.EncodeEmpty(cal)
	}

	return icalendar.Encode(cal)
}

// icsErrors renders ICS's own failure beyond the calendarNotFoundErrors and
// SourceURL checks it does inline.
var icsErrors = []errorCase{
	{ErrCalendarEmpty, conflict("calendar_empty", "calendar has no events to download")},
}

// ICS serves GET /api/calendars/{id}/ics: every series in the Calendar,
// unbounded window, as one VCALENDAR carrying X-WR-CALNAME/
// X-APPLE-CALENDAR-COLOR so the name and color survive export (#76). A
// Subscribed Calendar offers no per-Calendar download (#90, ADR-0032): a
// frozen snapshot is the wrong artifact for a Subscription, whose Events a
// Refresh will overwrite anyway — the URL in the sidebar is what actually
// carries it elsewhere.
func (h *CalendarHandler) ICS(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id := chi.URLParam(r, "id")

	calendar, err := h.calendars.Get(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to load calendar") {
		return
	}
	if calendar.SourceURL != nil {
		httpresponse.Error(w, http.StatusForbidden, "forbidden", "subscribed calendars have no per-calendar download; use the subscription URL instead")
		return
	}

	body, err := h.icsForCalendar(r.Context(), userID, calendar, false)
	if respondError(w, err, icsErrors, "failed to build calendar object") {
		return
	}

	httpresponse.ICS(w, sanitizeICSFilename(calendar.Name, "calendar")+".ics", body)
}

// subscriptionsZipEntry is the filename ICSAll gives the archive's
// plain-text Subscriptions listing (buildSubscriptionsListing) — the thing
// that actually restores a Subscription elsewhere, since a frozen .ics
// snapshot is the wrong artifact for something a Refresh will keep
// overwriting (#90, ADR-0032). Omitted entirely when the caller owns no
// Subscribed Calendars.
const subscriptionsZipEntry = "subscriptions.txt"

// writeZipEntry creates a name entry in zw carrying body — the create/write
// pair ICSAll repeats once per .ics file and once for the Subscriptions
// listing.
func writeZipEntry(zw *zip.Writer, name string, body []byte) error {
	entry, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = entry.Write(body)
	return err
}

// buildSubscriptionsListing renders one "Name: URL" line per Subscribed
// Calendar in calendars, URL masked via service.MaskURL — every surface
// that renders a Subscription URL must hide its password (ADR-0032), export
// included since an archive is a file that travels.
func buildSubscriptionsListing(calendars []repository.Calendar) []byte {
	var buf bytes.Buffer
	for _, calendar := range calendars {
		if calendar.SourceURL == nil {
			continue
		}
		fmt.Fprintf(&buf, "%s: %s\n", calendar.Name, service.MaskURL(*calendar.SourceURL))
	}
	return buf.Bytes()
}

// ICSAll serves GET /api/calendars/ics: every Calendar the caller owns,
// zipped, one .ics entry per Calendar named after it (#76). Subscribed
// Calendars are excluded from the .ics entries — a frozen snapshot is the
// wrong artifact for a Subscription — and listed instead, name and masked
// URL, in a subscriptionsZipEntry text entry (#90, ADR-0032). An account
// with no owned Calendars still produces a valid (possibly .ics-entry-free)
// archive.
func (h *CalendarHandler) ICSAll(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	calendars, err := h.calendars.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list calendars")
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	usedNames := make(map[string]int, len(calendars))
	for _, calendar := range calendars {
		if calendar.SourceURL != nil {
			continue
		}

		body, err := h.icsForCalendar(r.Context(), userID, calendar, true)
		if err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build calendar object")
			return
		}

		entryName := uniqueZipEntryName(usedNames, sanitizeICSFilename(calendar.Name, "calendar")+".ics")
		if err := writeZipEntry(zw, entryName, body); err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build archive")
			return
		}
	}

	if listing := buildSubscriptionsListing(calendars); len(listing) > 0 {
		if err := writeZipEntry(zw, subscriptionsZipEntry, listing); err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build archive")
			return
		}
	}

	if err := zw.Close(); err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build archive")
		return
	}

	httpresponse.Zip(w, "calendars.zip", buf.Bytes())
}
