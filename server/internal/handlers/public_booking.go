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

	"github.com/XiovV/calich/server/internal/clientip"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type PublicBookingHandler struct {
	public      *service.PublicBookingService
	rateLimiter *service.PublicBookingRateLimiter
}

func NewPublicBookingHandler(public *service.PublicBookingService, rateLimiter *service.PublicBookingRateLimiter) *PublicBookingHandler {
	return &PublicBookingHandler{public: public, rateLimiter: rateLimiter}
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
