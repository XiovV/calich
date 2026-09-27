// public_booking.go implements PublicBookingHandler: the stranger-facing
// GET surface behind /:handle/:slug (#324, ADR-0084, ADR-0087) — the app's
// first unauthenticated route that reads calendar data, mounted at
// /api/public and reached with no Session, no Workspace header, and no auth
// middleware at all. Rate limited per IP since it's public and reads real
// data (#324's "Public read endpoints are rate limited").
package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/caldavserver"
	"github.com/XiovV/calich/server/internal/clientip"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type PublicBookingHandler struct {
	public      *service.PublicBookingService
	index       *service.PublicIndexService
	rateLimiter *service.PublicBookingRateLimiter
}

func NewPublicBookingHandler(public *service.PublicBookingService, index *service.PublicIndexService, rateLimiter *service.PublicBookingRateLimiter) *PublicBookingHandler {
	return &PublicBookingHandler{public: public, index: index, rateLimiter: rateLimiter}
}

type publicBookingLinkResponse struct {
	HostName           string `json:"hostName"`
	HostTimezone       string `json:"hostTimezone"`
	Title              string `json:"title"`
	DurationMinutes    int    `json:"durationMinutes"`
	Location           string `json:"location"`
	Description        string `json:"description"`
	BookingHorizonDays int    `json:"bookingHorizonDays"`
	Paused             bool   `json:"paused"`
}

func toPublicBookingLinkResponse(l service.PublicBookingLink) publicBookingLinkResponse {
	return publicBookingLinkResponse{
		HostName:           l.HostName,
		HostTimezone:       l.HostTimezone,
		Title:              l.Title,
		DurationMinutes:    l.DurationMinutes,
		Location:           l.Location,
		Description:        l.Description,
		BookingHorizonDays: l.BookingHorizonDays,
		Paused:             l.Paused,
	}
}

// publicBookingErrors renders repository.ErrNotFound identically for an
// unknown Handle, an unknown Slug and a reserved-word Handle — the three
// are indistinguishable by design (ADR-0084), so this is the only "doesn't
// resolve" case this handler ever answers with.
var publicBookingErrors = []errorCase{
	{service.ErrInvalidMonth, badRequest(service.ErrInvalidMonth.Error())},
	{service.ErrPublicBookingRateLimitExceeded, rateLimited("too many requests, please try again later")},
	{repository.ErrNotFound, notFound("booking link not found")},
}

// bookErrors is publicBookingErrors widened with Book's own sentinels
// (#326, ADR-0087): a Paused link (in any of its three senses, ADR-0087),
// a missing/malformed name or email (ErrEmailRequired/ErrEmailTooLong/
// ErrInvalidEmail are service.validateEmail's own, shared with every other
// login-identifier-shaped input in this app), and the slot race itself,
// rendered as a 409 so the frontend can tell it apart from a plain 400 and
// re-fetch slots rather than just re-showing the form.
var bookErrors = alsoHandling(publicBookingErrors,
	errorCase{service.ErrBookingLinkPaused, conflict("booking_link_paused", service.ErrBookingLinkPaused.Error())},
	errorCase{service.ErrInvalidVisitorName, badRequest(service.ErrInvalidVisitorName.Error())},
	errorCase{service.ErrEmailRequired, badRequest(service.ErrEmailRequired.Error())},
	errorCase{service.ErrEmailTooLong, badRequest(service.ErrEmailTooLong.Error())},
	errorCase{service.ErrInvalidEmail, badRequest(service.ErrInvalidEmail.Error())},
	errorCase{service.ErrSlotTaken, conflict("slot_taken", service.ErrSlotTaken.Error())},
)

// checkRateLimit reports whether it already wrote a response (a 429),
// mirroring respondError's own bool-return convention. Every route on this
// handler calls it first, before doing any real work.
func (h *PublicBookingHandler) checkRateLimit(w http.ResponseWriter, r *http.Request) bool {
	ip := clientip.From(r)
	if err := h.rateLimiter.Check(r.Context(), ip); respondError(w, err, publicBookingErrors, "failed to check rate limit") {
		return true
	}
	// Recorded regardless of what the rest of this request finds — an
	// unknown handle or slug still costs a call, mirroring Register's own
	// AuthRateLimiter.RecordRegisterAttempt (every call charges the bucket,
	// not just a successful one), since what's throttled here is how often
	// this page is loaded at all.
	if err := h.rateLimiter.Record(r.Context(), ip); err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to record rate limit attempt")
		return true
	}
	return false
}

// Get serves GET /api/public/{handle}/{slug}: the host and offer details
// the public page renders, regardless of whether the link is Public or
// Private (ADR-0084: "a Private link is reachable by its direct URL") —
// Paused (explicit, no SMTP, or a lost Book-into Access, ADR-0087) is
// reported as a field on a 200 response rather than a distinct status, so
// the page can say plainly that it isn't accepting bookings instead of
// looking like it doesn't exist.
func (h *PublicBookingHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.checkRateLimit(w, r) {
		return
	}

	handle := chi.URLParam(r, "handle")
	slug := chi.URLParam(r, "slug")

	link, err := h.public.Get(r.Context(), handle, slug)
	if respondError(w, err, publicBookingErrors, "failed to get booking link") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toPublicBookingLinkResponse(link))
}

type publicIndexLinkResponse struct {
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	DurationMinutes int    `json:"durationMinutes"`
}

type publicIndexResponse struct {
	HostName string                    `json:"hostName"`
	Links    []publicIndexLinkResponse `json:"links"`
}

func toPublicIndexResponse(index service.PublicIndex) publicIndexResponse {
	links := make([]publicIndexLinkResponse, len(index.Links))
	for i, l := range index.Links {
		links[i] = publicIndexLinkResponse{Slug: l.Slug, Title: l.Title, DurationMinutes: l.DurationMinutes}
	}
	return publicIndexResponse{HostName: index.HostName, Links: links}
}

// Index serves GET /api/public/{handle}: the derived index of an owner's
// Public Booking Links (#325, ADR-0084) — always 200. An unknown Handle, a
// reserved-word Handle, and a Handle whose links are all Private, Paused,
// or filtered out by PublicIndexService's reuse of ADR-0087's Paused rule
// all render the identical empty-HostName, empty-Links shape, so this route
// cannot become a second User-enumeration door beside the one Get/Slots
// above already close.
func (h *PublicBookingHandler) Index(w http.ResponseWriter, r *http.Request) {
	if h.checkRateLimit(w, r) {
		return
	}

	handle := chi.URLParam(r, "handle")

	index, err := h.index.Get(r.Context(), handle)
	if respondError(w, err, nil, "failed to get public index") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toPublicIndexResponse(index))
}

type publicSlotsResponse struct {
	Slots []time.Time `json:"slots"`
}

// Slots serves GET /api/public/{handle}/{slug}/slots?year=&month=: every
// bookable slot start within the given calendar month (#323, #324), derived
// by the exact same server-side logic the authenticated route runs. A
// Paused link (in any of its three senses) always answers an empty list
// rather than an error.
func (h *PublicBookingHandler) Slots(w http.ResponseWriter, r *http.Request) {
	if h.checkRateLimit(w, r) {
		return
	}

	handle := chi.URLParam(r, "handle")
	slug := chi.URLParam(r, "slug")

	year, ok := parseRequiredIntQuery(w, r, "year")
	if !ok {
		return
	}
	month, ok := parseRequiredIntQuery(w, r, "month")
	if !ok {
		return
	}

	slots, err := h.public.Slots(r.Context(), handle, slug, year, time.Month(month), time.Now())
	if respondError(w, err, publicBookingErrors, "failed to derive slots") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, publicSlotsResponse{Slots: slots})
}

// bookRequest is Book's own body — the booking form's two required fields
// (ADR-0087), plus the slot start the visitor picked from Slots' own
// answer.
type bookRequest struct {
	Start        time.Time `json:"start"`
	VisitorName  string    `json:"visitorName"`
	VisitorEmail string    `json:"visitorEmail"`
}

type bookingResponse struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Book serves POST /api/public/{handle}/{slug}/book: confirms req.Start
// into a Busy Event on the Book-into Calendar (#326, ADR-0087). A slot
// already taken by the time this commits — either by another visitor
// racing the same one, or because it was never really bookable — answers
// 409 slot_taken; the frontend re-fetches Slots and asks the visitor to
// pick again.
func (h *PublicBookingHandler) Book(w http.ResponseWriter, r *http.Request) {
	if h.checkRateLimit(w, r) {
		return
	}

	handle := chi.URLParam(r, "handle")
	slug := chi.URLParam(r, "slug")

	req, ok := decodeJSON[bookRequest](w, r)
	if !ok {
		return
	}

	event, err := h.public.Book(r.Context(), handle, slug, service.BookingRequest{
		Start:        req.Start,
		VisitorName:  req.VisitorName,
		VisitorEmail: req.VisitorEmail,
	}, time.Now(), caldavserver.RequestBaseURL(r))
	if respondError(w, err, bookErrors, "failed to create booking") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, bookingResponse{Start: event.Start, End: event.End})
}

// cancelBookingErrors is publicBookingErrors' own sibling for Cancel: unlike
// every other route on this handler, Cancel resolves no (handle, slug), so
// it never renders repository.ErrNotFound — an unresolvable or expired
// token is its own distinct 400, never confused with "booking link not
// found".
var cancelBookingErrors = []errorCase{
	{service.ErrPublicBookingRateLimitExceeded, rateLimited("too many requests, please try again later")},
	{service.ErrInvalidCancelToken, badRequest(service.ErrInvalidCancelToken.Error())},
	{service.ErrBookingAlreadyStarted, conflict("booking_already_started", service.ErrBookingAlreadyStarted.Error())},
}

// cancelBookingRequest is Cancel's own body: the signed link's token, read
// from the query string on the page it opens and posted here rather than
// acted on by a bare GET (#327, ADR-0087) — a mail client or link scanner
// prefetching the GET page never triggers the cancellation itself.
type cancelBookingRequest struct {
	Token string `json:"token"`
}

// Cancel serves POST /api/public/cancel-booking: the signed cancel link's
// own destination (#327, ADR-0087). Deletes the booking's Event, which
// emits the METHOD:CANCEL the Attendee machinery already sends and frees
// the slot immediately, and queues the host's own cancellation notice.
// Idempotent — following the link twice never errors on the second attempt.
func (h *PublicBookingHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if h.checkRateLimit(w, r) {
		return
	}

	req, ok := decodeJSON[cancelBookingRequest](w, r)
	if !ok {
		return
	}

	if err := h.public.Cancel(r.Context(), req.Token, time.Now()); respondError(w, err, cancelBookingErrors, "failed to cancel booking") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, struct{}{})
}
