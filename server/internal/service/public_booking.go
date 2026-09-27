package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// PublicBookingLink is what a stranger holding a Booking Link's URL is told
// (#324, ADR-0084, ADR-0087) — deliberately narrower than repository.
// BookingLink: no id, no Book-into Calendar, no Conflict set, nothing that
// isn't shown on the page itself. Visibility itself is never exposed either
// — Paused collapses "explicitly Paused", "no SMTP configured" and "lost
// Book-into Access" into the one answer a visitor ever needs (ADR-0087: "A
// link behaves as Paused ... and Settings says why" — the why is for the
// host, in Settings, not for a stranger).
type PublicBookingLink struct {
	HostName           string
	HostTimezone       string
	Title              string
	DurationMinutes    int
	Location           string
	Description        string
	BookingHorizonDays int
	Paused             bool
}

// PublicBookingService serves the public Booking Link page (#324, ADR-0084,
// ADR-0087) — the app's first unauthenticated surface that touches calendar
// data. Every method resolves (handle, slug) itself rather than taking an
// id, and every failure to resolve is repository.ErrNotFound: an unknown
// Handle, an unknown Slug, and a reserved-word Handle are indistinguishable,
// so the namespace cannot enumerate Users (ADR-0084's Consequences). A
// Private link is not folded into that: it resolves and renders exactly
// like a Public one when reached by its own direct URL — only the derived
// index (#325) omits it.
type PublicBookingService struct {
	users        *repository.UserRepository
	links        *repository.BookingLinkRepository
	schedules    *repository.AvailabilityScheduleRepository
	calendars    *CalendarService
	bookingLinks *BookingLinkService
	// smtpConfigured mirrors config.Config.SMTPConfigured() — publishing a
	// Booking Link requires SMTP (ADR-0087), so every link behaves as
	// Paused when this instance has none, regardless of what it's set to.
	smtpConfigured bool
}

func NewPublicBookingService(users *repository.UserRepository, links *repository.BookingLinkRepository, schedules *repository.AvailabilityScheduleRepository, calendars *CalendarService, bookingLinks *BookingLinkService, smtpConfigured bool) *PublicBookingService {
	return &PublicBookingService{
		users:          users,
		links:          links,
		schedules:      schedules,
		calendars:      calendars,
		bookingLinks:   bookingLinks,
		smtpConfigured: smtpConfigured,
	}
}

// Get resolves (handle, slug) into what the public page renders.
func (s *PublicBookingService) Get(ctx context.Context, handle, slug string) (PublicBookingLink, error) {
	link, user, err := s.resolve(ctx, handle, slug)
	if err != nil {
		return PublicBookingLink{}, err
	}

	schedule, err := s.schedules.GetByID(ctx, link.AvailabilityScheduleID, link.UserID)
	if err != nil {
		return PublicBookingLink{}, fmt.Errorf("get availability schedule: %w", err)
	}

	paused, err := s.isPaused(ctx, link)
	if err != nil {
		return PublicBookingLink{}, err
	}

	return PublicBookingLink{
		HostName:           user.Name,
		HostTimezone:       schedule.Tzid,
		Title:              link.Title,
		DurationMinutes:    link.DurationMinutes,
		Location:           link.Location,
		Description:        link.Description,
		BookingHorizonDays: link.BookingHorizonDays,
		Paused:             paused,
	}, nil
}

// Slots resolves (handle, slug) and derives its slots for (year, month),
// reusing the exact same derivation the authenticated /slots route runs
// (BookingLinkService.DeriveSlotsForLinkAndMonth) — the whole point of #323
// deriving server-side (ADR-0085's "the public page has no Session, so slot
// derivation runs on the backend"). A Paused link (explicitly, no SMTP, or a
// lost Book-into Access) always answers no slots, without distinguishing why
// — ADR-0087's Access re-check happens here, at read time, not only at
// Create/Update.
func (s *PublicBookingService) Slots(ctx context.Context, handle, slug string, year int, month time.Month, now time.Time) ([]time.Time, error) {
	if month < 1 || month > 12 {
		return nil, ErrInvalidMonth
	}

	link, _, err := s.resolve(ctx, handle, slug)
	if err != nil {
		return nil, err
	}

	paused, err := s.isPaused(ctx, link)
	if err != nil {
		return nil, err
	}
	if paused {
		return []time.Time{}, nil
	}

	return s.bookingLinks.DeriveSlotsForLinkAndMonth(ctx, link, year, month, now)
}

// resolve looks up the Booking Link identified by (handle, slug), collapsing
// every way that can fail into repository.ErrNotFound: handle is reserved
// (#321, ADR-0084 — this namespace consumes service.ReservedHandles
// directly rather than keeping a second list), no User claims handle, or
// that User has no Booking Link with slug. Case-insensitivity for handle is
// the users.handle column's own COLLATE NOCASE (ADR-0084, mirroring Email);
// slug is compared as BookingLinkRepository.GetBySlug already does.
func (s *PublicBookingService) resolve(ctx context.Context, handle, slug string) (repository.BookingLink, repository.User, error) {
	if IsReservedHandle(handle) {
		return repository.BookingLink{}, repository.User{}, repository.ErrNotFound
	}

	user, err := s.users.GetByHandle(ctx, handle)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.BookingLink{}, repository.User{}, repository.ErrNotFound
		}
		return repository.BookingLink{}, repository.User{}, fmt.Errorf("get user by handle: %w", err)
	}

	link, err := s.links.GetBySlug(ctx, user.ID, slug)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.BookingLink{}, repository.User{}, repository.ErrNotFound
		}
		return repository.BookingLink{}, repository.User{}, fmt.Errorf("get booking link by slug: %w", err)
	}

	return link, user, nil
}

// isPaused reports whether link behaves as Paused (ADR-0087): its own
// Visibility says so, this instance has no SMTP transport configured, or
// its owner has lost Owner or Editor Access to its Book-into Calendar since
// it was last saved — re-checked here, at read time, rather than trusted
// from create/update time, since a Share can be revoked at any point
// afterward.
func (s *PublicBookingService) isPaused(ctx context.Context, link repository.BookingLink) (bool, error) {
	if link.Visibility == "paused" {
		return true, nil
	}
	if !s.smtpConfigured {
		return true, nil
	}

	access, calendar, err := s.calendars.Access(ctx, link.UserID, link.BookIntoCalendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return true, nil
		}
		return false, fmt.Errorf("resolve book-into calendar access: %w", err)
	}
	if !access.CanWrite() || calendar.WorkspaceID != link.WorkspaceID {
		return true, nil
	}

	return false, nil
}
