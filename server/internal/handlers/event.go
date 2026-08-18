package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/attachmentstore"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type EventHandler struct {
	events      *service.EventService
	attachments *attachmentstore.Store
}

func NewEventHandler(events *service.EventService, attachments *attachmentstore.Store) *EventHandler {
	return &EventHandler{events: events, attachments: attachments}
}

// dateOnlyLayout is the wire format for an all-day Event's start/end: a
// timezone-free date, e.g. "2026-08-04" (ADR-0017). A timed Event's start/end
// use the default time.Time RFC3339 encoding instead.
const dateOnlyLayout = "2006-01-02"

// parseEventTime parses a wire start/end string, branching on allDay so an
// all-day date-only string is never run through timezone-aware RFC3339
// parsing (ADR-0017).
func parseEventTime(raw string, allDay bool) (time.Time, error) {
	if allDay {
		return time.ParseInLocation(dateOnlyLayout, raw, time.UTC)
	}
	return time.Parse(time.RFC3339, raw)
}

// formatEventTime is parseEventTime's inverse for wire responses. A timed
// Event is always emitted as a canonical UTC "…Z" instant — never the offset
// it happened to be parsed with — so the client can only ever render off the
// Viewer zone or the sibling tzid, never a stray wire offset (ADR-0019).
func formatEventTime(t time.Time, allDay bool) string {
	if allDay {
		return t.UTC().Format(dateOnlyLayout)
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// reminderWire is a Reminder's wire shape (ADR-0020), shared by requests and
// responses — a Reminder has no other representation.
type reminderWire struct {
	OffsetMinutes int    `json:"offsetMinutes"`
	Channel       string `json:"channel"`
}

func toReminderWire(reminders []repository.Reminder) []reminderWire {
	if len(reminders) == 0 {
		return nil
	}
	wire := make([]reminderWire, len(reminders))
	for i, r := range reminders {
		wire[i] = reminderWire{OffsetMinutes: r.OffsetMinutes, Channel: r.Channel}
	}
	return wire
}

func fromReminderWire(wire []reminderWire) []repository.Reminder {
	if len(wire) == 0 {
		return nil
	}
	reminders := make([]repository.Reminder, len(wire))
	for i, w := range wire {
		reminders[i] = repository.Reminder{OffsetMinutes: w.OffsetMinutes, Channel: w.Channel}
	}
	return reminders
}

// attachmentWire is an Attachment's wire shape (#132, ADR-0040), shared by
// the Event response's embedded list and the upload endpoint's own
// response — an Attachment has no other representation.
type attachmentWire struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	// UploadedBy and UploadedByName are the Attachment's uploader, for
	// display only — never consulted for authorization, which resolves
	// through the Event's Calendar instead (ADR-0034). Both absent when the
	// uploader's account has been deleted.
	UploadedBy     *int64    `json:"uploadedBy,omitempty"`
	UploadedByName string    `json:"uploadedByName,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

func toAttachmentResponse(a repository.Attachment) attachmentWire {
	return attachmentWire{
		ID:             a.ID,
		Filename:       a.Filename,
		ContentType:    a.ContentType,
		SizeBytes:      a.SizeBytes,
		UploadedBy:     a.UploadedBy,
		UploadedByName: a.UploadedByName,
		CreatedAt:      a.CreatedAt,
	}
}

func toAttachmentResponses(attachments []repository.Attachment) []attachmentWire {
	if len(attachments) == 0 {
		return nil
	}
	wire := make([]attachmentWire, len(attachments))
	for i, a := range attachments {
		wire[i] = toAttachmentResponse(a)
	}
	return wire
}

// eventResponse is an Event's wire shape. Start and End are strings so their
// format can branch on AllDay (ADR-0017); toEventResponse is the only thing
// that builds one, and does that formatting. Field order here is the emitted
// JSON's field order.
type eventResponse struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendarId"`
	Title      string `json:"title"`
	Start      string `json:"start"`
	End        string `json:"end"`
	// AllDay flags this Event as occupying whole dates rather than a time
	// range — see ADR-0017 and CONTEXT.md's All-day Event.
	AllDay bool   `json:"allDay,omitempty"`
	Rrule  string `json:"rrule,omitempty"`
	// ParentID and RecurrenceID are present only on an Override — a standalone
	// Event that replaces one Occurrence of its parent's series (ADR-0016).
	ParentID     *string    `json:"parentId,omitempty"`
	RecurrenceID *time.Time `json:"recurrenceId,omitempty"`
	// Exdates lists a Master's cancelled Occurrence starts (Exceptions).
	// Always absent on an Override.
	Exdates []time.Time `json:"exdates,omitempty"`
	// Tzid is the Event's Anchor zone: a named IANA zone, "Etc/UTC" for an
	// absolute instant, or nil for a Floating Event. See ADR-0019.
	Tzid *string `json:"tzid,omitempty"`
	// Reminders is this Event's Reminders (ADR-0020). Absent/empty means no
	// Reminders.
	Reminders []reminderWire `json:"reminders,omitempty"`
	// Description and Location are free-text fields on an Event (#61).
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	// URL is the Event's optional link (ADR-0063), stored exactly as
	// submitted — never validated or rewritten.
	URL string `json:"url,omitempty"`
	// Color is this Event's own color override — absent means "inherit the
	// Calendar's color" (ADR-0043).
	Color *string `json:"color,omitempty"`
	// CreatedBy and CreatedByName are the Event's creator, for display
	// only — never consulted for authorization, which resolves through
	// CalendarID instead (ADR-0034, #118). Both absent when the creator's
	// account has been deleted or the Event predates this column.
	CreatedBy     *int64 `json:"createdBy,omitempty"`
	CreatedByName string `json:"createdByName,omitempty"`
	// Attachments is this Event's Attachments (#132, ADR-0040) — present
	// only on a Master; an Override never carries its own.
	Attachments []attachmentWire `json:"attachments,omitempty"`
	// CalendarName and CalendarColor are CalendarID's Name and resolved
	// display Color (ADR-0046) — display only, so a caller who can see this
	// Event only as an Attendee (with no Calendar Access, and thus no
	// GET /api/calendars row for CalendarID) still has something to render
	// for its Calendar.
	CalendarName  string `json:"calendarName,omitempty"`
	CalendarColor string `json:"calendarColor,omitempty"`
	// AttendeeCount is this Event's number of Attendees (ADR-0046, #193) —
	// present so a caller deciding whether to render an Event expanded knows
	// synchronously, without a separate round-trip to the Attendees endpoint.
	AttendeeCount int `json:"attendeeCount,omitempty"`
}

func toEventResponse(e repository.Event) eventResponse {
	return eventResponse{
		ID:            e.ID,
		CalendarID:    e.CalendarID,
		Title:         e.Title,
		Start:         formatEventTime(e.Start, e.AllDay),
		End:           formatEventTime(e.End, e.AllDay),
		AllDay:        e.AllDay,
		Rrule:         e.Rrule,
		ParentID:      e.ParentID,
		RecurrenceID:  e.RecurrenceID,
		Exdates:       e.Exdates,
		Tzid:          e.Tzid,
		Reminders:     toReminderWire(e.Reminders),
		Description:   e.Description,
		Location:      e.Location,
		URL:           e.URL,
		Color:         e.Color,
		CreatedBy:     e.CreatedBy,
		CreatedByName: e.CreatedByName,
		Attachments:   toAttachmentResponses(e.Attachments),
		CalendarName:  e.CalendarName,
		CalendarColor: e.CalendarColor,
		AttendeeCount: e.AttendeeCount,
	}
}

// eventWriteErrors is the rendering shared by the create and update paths,
// which validate the same fields the same way.
var eventWriteErrors = []errorCase{
	{service.ErrInvalidTitle, badRequest("title must not be empty")},
	{service.ErrInvalidTimeRange, badRequest("end must be after start")},
	{service.ErrInvalidRecurrenceRule, badRequest("recurrence rule is invalid")},
	{service.ErrInvalidReminderChannel, badRequest("reminder channel must be \"notification\" or \"email\"")},
	{service.ErrInvalidEventColor, badRequest("color must be a valid hex color (#RGB, #RRGGBB, or #RRGGBBAA)")},
	{service.ErrCalendarNotFound, badRequest("calendar not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

// On create, a missing parent is named as such — parentId is a body field the
// client chose. On the exception and reparent paths it is the URL's event, so
// those render it as a plain "event not found" 404 instead.
// The Attendee-at-create cases (#187, ADR-0055, #200/ADR-0058) mirror
// addAttendeeErrors/addGroupAttendeeErrors exactly — a create carrying
// Attendees can fail for the same reasons AddAttendee/AddGroupAttendee/
// AddAttendeeByEmail do, just rendered from the same endpoint as the rest of
// Create's validation.
var createEventErrors = alsoHandling(eventWriteErrors,
	errorCase{service.ErrInvalidOverride, badRequest("an override must not have its own recurrence rule, and requires a recurrence id")},
	errorCase{service.ErrParentIsOverride, badRequest("parent event must be a master, not an override")},
	errorCase{service.ErrParentNotFound, badRequest("parent event not found")},
	errorCase{service.ErrUserNotFound, badRequest("user not found")},
	errorCase{service.ErrAttendeeTargetNotInWorkspace, badRequest("attendee target does not belong to this workspace")},
	errorCase{service.ErrGroupNotFound, badRequest("group not found")},
	errorCase{repository.ErrAlreadyAttendee, conflict("already_attendee", "already invited to this event")},
	errorCase{service.ErrInvalidEmail, badRequest("email must be a valid address and must not contain whitespace or a colon")},
	errorCase{service.ErrAttendeeIsOrganizer, badRequest("cannot invite the event's organizer as an attendee")},
	errorCase{service.ErrAttendeeEmailInvitesUnavailable, badRequest("email invitations are not available on this instance")},
)

// respondInviteRateLimitError renders a *service.InviteRateLimitError as a
// 429 naming the actor's own configured hourly ceiling — err.Error()
// already includes it (service/event.go), same reason
// respondSoleWorkspaceOwnerError in account.go can't use a fixed table of
// precomputed response bodies. Checked before createEventErrors/
// addAttendeeErrors/addGroupAttendeeErrors at every entry point that can
// queue a brand-new Invitation (#204, ADR-0058) — Create, AddAttendee (both
// the userId and email branches, via addAttendeeErrors' shared caller), and
// AddGroupAttendee. Never returned by RemoveAttendee or Update, which only
// ever queue lifecycle mail (a re-send or a CANCEL), uncapped by design.
func respondInviteRateLimitError(w http.ResponseWriter, err error) bool {
	var rateLimitErr *service.InviteRateLimitError
	if !errors.As(err, &rateLimitErr) {
		return false
	}
	httpresponse.Error(w, http.StatusTooManyRequests, "invite_rate_limit_exceeded", rateLimitErr.Error())
	return true
}

var updateEventErrors = alsoHandling(eventWriteErrors,
	errorCase{service.ErrInvalidOverride, badRequest("an override must not have its own recurrence rule")},
	errorCase{repository.ErrNotFound, notFound("event not found")},
)

var addExceptionErrors = []errorCase{
	{service.ErrParentNotFound, notFound("event not found")},
	{service.ErrParentIsOverride, badRequest("parent event must be a master, not an override")},
	{service.ErrParentNotRecurring, badRequest("parent event does not recur")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

var reparentErrors = []errorCase{
	{service.ErrParentNotFound, notFound("event not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

var eventNotFoundErrors = []errorCase{
	{repository.ErrNotFound, notFound("event not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

func (h *EventHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	from, ok := parseOptionalTimeParam(w, r, "from")
	if !ok {
		return
	}
	to, ok := parseOptionalTimeParam(w, r, "to")
	if !ok {
		return
	}

	events, err := h.events.List(r.Context(), userID, from, to)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list events")
		return
	}

	response := make([]eventResponse, len(events))
	for i, e := range events {
		response[i] = toEventResponse(e)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

func parseOptionalTimeParam(w http.ResponseWriter, r *http.Request, name string) (*time.Time, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, true
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", name+" must be an RFC3339 timestamp")
		return nil, false
	}
	return &parsed, true
}

// createEventRequest is the POST /api/events body. Start and End are strings
// so their format can branch on AllDay (ADR-0017) — parseEventTimes turns them
// into instants once the body has decoded.
type createEventRequest struct {
	ID           string     `json:"id"`
	CalendarID   string     `json:"calendarId"`
	Title        string     `json:"title"`
	Start        string     `json:"start"`
	End          string     `json:"end"`
	AllDay       bool       `json:"allDay,omitempty"`
	Rrule        string     `json:"rrule"`
	ParentID     *string    `json:"parentId,omitempty"`
	RecurrenceID *time.Time `json:"recurrenceId,omitempty"`
	Tzid         *string    `json:"tzid,omitempty"`
	Description  string     `json:"description,omitempty"`
	Location     string     `json:"location,omitempty"`
	// URL is the Event's optional link (ADR-0063), stored exactly as
	// submitted — never validated or rewritten.
	URL string `json:"url,omitempty"`
	// Color is this Event's own color override — absent/null means "inherit
	// the Calendar's color" (ADR-0043).
	Color *string `json:"color,omitempty"`
	// CopyRemindersFrom, when set, copies every User's Reminder rows from
	// that Event onto the one this create mints (ADR-0064) — used only by
	// the "This and following" split's new Master, naming the old Master's
	// id. An Override (ParentID set) always copies from its own Parent
	// instead; this field is for the one create that mints a standalone
	// Master with no Parent relationship of its own to infer it from.
	CopyRemindersFrom *string `json:"copyRemindersFrom,omitempty"`
	// AttendeeUserIds, AttendeeGroupIds, and AttendeeEmails name Attendees to
	// invite as part of this create (#187, ADR-0055, #200/ADR-0058) —
	// createEventRequest's alone; update carries no Attendee fields of its
	// own, since an existing Event's invites go through AddAttendee/
	// AddGroupAttendee/AddAttendeeByEmail instead.
	AttendeeUserIds  []int64  `json:"attendeeUserIds,omitempty"`
	AttendeeGroupIds []int64  `json:"attendeeGroupIds,omitempty"`
	AttendeeEmails   []string `json:"attendeeEmails,omitempty"`
}

// parseEventTimes converts a decoded body's start/end strings into instants,
// branching on allDay (ADR-0017). Shared by create and update, which carry the
// same three fields.
func parseEventTimes(rawStart, rawEnd string, allDay bool) (start, end time.Time, err error) {
	start, err = parseEventTime(rawStart, allDay)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start: %w", err)
	}
	end, err = parseEventTime(rawEnd, allDay)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end: %w", err)
	}
	return start, end, nil
}

func (h *EventHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req createEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	// Checked before the id, matching the order when start/end were parsed
	// inside UnmarshalJSON — i.e. during Decode, above.
	start, end, err := parseEventTimes(req.Start, req.End, req.AllDay)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a valid UUID")
		return
	}

	event, err := h.events.Create(r.Context(), userID, req.ID, service.EventWrite{
		CalendarID:        req.CalendarID,
		Title:             req.Title,
		Start:             start,
		End:               end,
		AllDay:            req.AllDay,
		Rrule:             req.Rrule,
		ParentID:          req.ParentID,
		RecurrenceID:      req.RecurrenceID,
		Tzid:              req.Tzid,
		Description:       req.Description,
		Location:          req.Location,
		URL:               req.URL,
		Color:             req.Color,
		CopyRemindersFrom: req.CopyRemindersFrom,
		AttendeeUserIDs:   req.AttendeeUserIds,
		AttendeeGroupIDs:  req.AttendeeGroupIds,
		AttendeeEmails:    req.AttendeeEmails,
	})
	if respondInviteRateLimitError(w, err) {
		return
	}
	if respondError(w, err, createEventErrors, "failed to create event") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toEventResponse(event))
}

func (h *EventHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	event, err := h.events.Get(r.Context(), userID, id)
	if respondError(w, err, eventNotFoundErrors, "failed to load event") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toEventResponse(event))
}

// updateEventRequest is the PATCH /api/events/{id} body. Start and End are
// strings for the same reason as createEventRequest's (ADR-0017).
type updateEventRequest struct {
	CalendarID  string  `json:"calendarId"`
	Title       string  `json:"title"`
	Start       string  `json:"start"`
	End         string  `json:"end"`
	AllDay      bool    `json:"allDay,omitempty"`
	Rrule       string  `json:"rrule"`
	Tzid        *string `json:"tzid,omitempty"`
	Description string  `json:"description,omitempty"`
	Location    string  `json:"location,omitempty"`
	// URL is the Event's optional link (ADR-0063), stored exactly as
	// submitted — never validated or rewritten.
	URL string `json:"url,omitempty"`
	// Color is this Event's own color override — absent/null means "inherit
	// the Calendar's color" (ADR-0043).
	Color *string `json:"color,omitempty"`
}

func (h *EventHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req updateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	start, end, err := parseEventTimes(req.Start, req.End, req.AllDay)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	event, err := h.events.Update(r.Context(), userID, id, service.EventWrite{
		CalendarID:  req.CalendarID,
		Title:       req.Title,
		Start:       start,
		End:         end,
		AllDay:      req.AllDay,
		Rrule:       req.Rrule,
		Tzid:        req.Tzid,
		Description: req.Description,
		Location:    req.Location,
		URL:         req.URL,
		Color:       req.Color,
	})
	if respondError(w, err, updateEventErrors, "failed to update event") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toEventResponse(event))
}

func (h *EventHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	err := h.events.Delete(r.Context(), userID, id)
	if respondError(w, err, eventNotFoundErrors, "failed to delete event") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type createExceptionRequest struct {
	OccurrenceStart time.Time `json:"occurrenceStart"`
}

// AddException cancels a single Occurrence of a recurring master (deleting
// "this event" on a recurring Occurrence), storing it as an iCalendar EXDATE
// (ADR-0016).
func (h *EventHandler) AddException(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req createExceptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	err := h.events.AddException(r.Context(), userID, id, req.OccurrenceStart)
	if respondError(w, err, addExceptionErrors, "failed to add exception") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type reparentRequest struct {
	NewParentID string    `json:"newParentId"`
	FromStart   time.Time `json:"fromStart"`
}

// Reparent moves every Override/Exception of the named event at-or-after
// fromStart to belong to newParentId instead — the "this and following" split
// reparenting overrides/exceptions at the boundary (ADR-0016).
func (h *EventHandler) Reparent(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req reparentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	err := h.events.ReparentFrom(r.Context(), userID, id, req.NewParentID, req.FromStart)
	if respondError(w, err, reparentErrors, "failed to reparent series") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// attendeeWire is an Attendee's wire shape (#161, ADR-0046), shared by the
// list, invite, and response endpoints — an Attendee has no other
// representation. UserID is nil for an email-shaped Attendee (#200,
// ADR-0058) — a client renders it by Email instead, marked as not a Member,
// since there's no account behind it.
type attendeeWire struct {
	UserID   *int64 `json:"userId"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Response string `json:"response"`
}

func toAttendeeResponse(a repository.AttendeeWithName) attendeeWire {
	w := attendeeWire{UserID: a.UserID, Name: a.Name, Response: a.Response}
	if a.UserID == nil {
		// Email is the only identifier an email-shaped Attendee has
		// (#200, ADR-0058), so it belongs on the wire. For a User-backed
		// row it's the Email a.UserID's own account holds — Name already
		// identifies them here, and exposing their real address to every
		// other Attendee of the Event (which needs no Calendar Access,
		// ADR-0046) is a wider grant than #200 asked for.
		w.Email = a.Email
	}
	return w
}

// attendeeVisibilityErrors renders EventService.ListAttendees' not-visible
// answer as a 404 — the caller has neither Calendar Access nor an Attendee
// invite of their own to eventID.
var attendeeVisibilityErrors = []errorCase{
	{repository.ErrNotFound, notFound("event not found")},
}

// addAttendeeErrors is eventNotFoundErrors (a caller with no Editor Access
// sees the same "event not found"/"read-only" answers writing an Event
// already does) plus the Attendee-specific failure modes (#161, ADR-0046)
// and the typed-address ones inviteEmail can fail with (#200, ADR-0058).
var addAttendeeErrors = alsoHandling(eventNotFoundErrors,
	errorCase{service.ErrUserNotFound, badRequest("user not found")},
	errorCase{service.ErrAttendeeTargetNotInWorkspace, badRequest("attendee target does not belong to this workspace")},
	errorCase{repository.ErrAlreadyAttendee, conflict("already_attendee", "already invited to this event")},
	errorCase{service.ErrInvalidEmail, badRequest("email must be a valid address and must not contain whitespace or a colon")},
	errorCase{service.ErrAttendeeIsOrganizer, badRequest("cannot invite the event's organizer as an attendee")},
	errorCase{service.ErrAttendeeEmailInvitesUnavailable, badRequest("email invitations are not available on this instance")},
)

var setResponseErrors = []errorCase{
	{service.ErrInvalidResponse, badRequest("response must be \"needs-action\", \"accepted\", \"declined\", or \"tentative\"")},
	{repository.ErrNotFound, notFound("you are not an attendee of this event")},
}

// ListAttendees serves GET /api/events/{id}/attendees: every Attendee of
// id, open to anyone who can see id at all — existing Calendar Access, or
// being an Attendee themselves (#161, ADR-0046).
func (h *EventHandler) ListAttendees(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	attendees, err := h.events.ListAttendees(r.Context(), userID, id)
	if respondError(w, err, attendeeVisibilityErrors, "failed to list attendees") {
		return
	}

	response := make([]attendeeWire, len(attendees))
	for i, a := range attendees {
		response[i] = toAttendeeResponse(a)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

// addAttendeeRequest carries exactly one of UserID or Email (#200,
// ADR-0058) — the picker's existing Member/Group target, or a typed
// address. Neither or both set is a plain invalid_request 400, not one of
// addAttendeeErrors' cases, since it names no target at all to check.
type addAttendeeRequest struct {
	UserID *int64  `json:"userId,omitempty"`
	Email  *string `json:"email,omitempty"`
}

// AddAttendee serves POST /api/events/{id}/attendees: invites userId or
// email as an Attendee of id, callable only by an Editor of id's Calendar
// (#161, ADR-0046, #200/ADR-0058).
func (h *EventHandler) AddAttendee(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req addAttendeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	if (req.UserID == nil) == (req.Email == nil) {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "exactly one of userId or email must be set")
		return
	}

	if req.Email != nil {
		attendee, err := h.events.AddAttendeeByEmail(r.Context(), userID, id, *req.Email)
		if respondInviteRateLimitError(w, err) {
			return
		}
		if respondError(w, err, addAttendeeErrors, "failed to add attendee") {
			return
		}
		httpresponse.JSON(w, http.StatusCreated, toAttendeeResponse(attendee))
		return
	}

	attendee, err := h.events.AddAttendee(r.Context(), userID, id, *req.UserID)
	if respondInviteRateLimitError(w, err) {
		return
	}
	if respondError(w, err, addAttendeeErrors, "failed to add attendee") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, attendeeWire{UserID: &attendee.UserID, Response: attendee.Response})
}

// addGroupAttendeeErrors is eventNotFoundErrors plus the Group-invite
// failure modes (#162, ADR-0046) — the Group-target analog of
// addAttendeeErrors.
var addGroupAttendeeErrors = alsoHandling(eventNotFoundErrors,
	errorCase{service.ErrGroupNotFound, badRequest("group not found")},
	errorCase{service.ErrAttendeeTargetNotInWorkspace, badRequest("attendee target does not belong to this workspace")},
)

type addGroupAttendeeRequest struct {
	GroupID int64 `json:"groupId"`
}

// AddGroupAttendee serves POST /api/events/{id}/attendees/group: invites
// every current member of req.GroupID as an Attendee of id, a one-time
// snapshot expansion rather than a dynamic Group Share (#162, ADR-0046).
// Callable only by an Editor of id's Calendar, same as AddAttendee.
func (h *EventHandler) AddGroupAttendee(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req addGroupAttendeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	attendees, err := h.events.AddGroupAttendee(r.Context(), userID, id, req.GroupID)
	if respondInviteRateLimitError(w, err) {
		return
	}
	if respondError(w, err, addGroupAttendeeErrors, "failed to add group attendees") {
		return
	}

	response := make([]attendeeWire, len(attendees))
	for i, a := range attendees {
		response[i] = attendeeWire{UserID: &a.UserID, Response: a.Response}
	}

	httpresponse.JSON(w, http.StatusCreated, response)
}

// RemoveAttendee serves DELETE /api/events/{id}/attendees/{userId}: revokes
// userId's Attendee invite to id, callable only by an Editor of id's
// Calendar (#161, ADR-0046).
func (h *EventHandler) RemoveAttendee(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	targetUserID, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "userId must be a valid integer")
		return
	}

	if err := h.events.RemoveAttendee(r.Context(), userID, id, targetUserID); respondError(w, err, eventNotFoundErrors, "failed to remove attendee") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveAttendeeByEmail serves DELETE /api/events/{id}/attendees/email/{email}:
// revokes email's Attendee invite to id (#200, ADR-0058) — the email-shaped
// counterpart to RemoveAttendee, same caller requirement. email travels
// percent-encoded in the path (an address contains '@' and possibly '+');
// chi.URLParam returns it verbatim rather than decoded, since chi routes
// off r.URL.RawPath whenever it's present, so it's unescaped by hand here.
func (h *EventHandler) RemoveAttendeeByEmail(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")
	targetEmail, err := url.PathUnescape(chi.URLParam(r, "email"))
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "email must be a valid URL-encoded path segment")
		return
	}

	if err := h.events.RemoveAttendeeByEmail(r.Context(), userID, id, targetEmail); respondError(w, err, eventNotFoundErrors, "failed to remove attendee") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type setAttendeeResponseRequest struct {
	Response string `json:"response"`
}

// SetAttendeeResponse serves PUT /api/events/{id}/attendees/response: sets
// the caller's own response to id, the only response this endpoint can ever
// change — there is no way to name a different target user, so an
// organizer or Editor can never set someone else's response through it
// (#161, ADR-0046).
func (h *EventHandler) SetAttendeeResponse(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req setAttendeeResponseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	attendee, err := h.events.SetResponse(r.Context(), userID, id, req.Response)
	if respondError(w, err, setResponseErrors, "failed to set response") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, attendeeWire{UserID: &attendee.UserID, Response: attendee.Response})
}
