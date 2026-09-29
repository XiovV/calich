// public_index.go implements PublicIndexService: the derived rendering
// behind /:handle (#325, ADR-0084) — one User's Name and every Public
// Booking Link they hold, unioned across every Workspace they belong to.
// There is no row behind it, in exactly the sense ADR-0083's Task bucket
// and Occurrence are computed rather than stored.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/XiovV/calich/server/internal/repository"
)

// PublicIndexLink is one entry on the index — just enough to list an offer
// and let a visitor reach it: Title, Duration, and the Slug that addresses
// it under the same Handle.
type PublicIndexLink struct {
	Slug            string
	Title           string
	DurationMinutes int
}

// PublicIndex is what /:handle renders. HostName is deliberately empty
// whenever Links is empty: an unknown Handle, a reserved-word Handle, and a
// real Handle whose links are all Private, explicitly Paused, or filtered
// out by PublicBookingService's own Paused rule (ADR-0087, e.g. no SMTP
// configured) must render identically, or the index becomes a second door
// into the User-enumeration hole #324 already closed for the per-link page.
type PublicIndex struct {
	HostName string
	Links    []PublicIndexLink
}

// PublicIndexService serves the public index page (#325, ADR-0084). It
// reuses PublicBookingService's own Paused rule wholesale rather than
// keeping a second copy of it — the issue's own "shares the public route
// namespace and refusal behaviour" against #324.
type PublicIndexService struct {
	users          *repository.UserRepository
	links          *repository.BookingLinkRepository
	publicBookings *PublicBookingService
}

func NewPublicIndexService(users *repository.UserRepository, links *repository.BookingLinkRepository, publicBookings *PublicBookingService) *PublicIndexService {
	return &PublicIndexService{users: users, links: links, publicBookings: publicBookings}
}

// Get resolves handle into the index it renders. Every way resolution can
// fail to name a real, listable User collapses into the same zero-value
// PublicIndex{} rather than an error — an unknown Handle, a reserved-word
// Handle, a User with no Public links, or one whose Public links are all
// Paused — so the caller always answers 200 with the same shape.
func (s *PublicIndexService) Get(ctx context.Context, handle string) (PublicIndex, error) {
	if IsReservedHandle(handle) {
		return PublicIndex{}, nil
	}

	user, err := s.users.GetByHandle(ctx, handle)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return PublicIndex{}, nil
		}
		return PublicIndex{}, fmt.Errorf("get user by handle: %w", err)
	}

	links, err := s.links.ListPublicForUser(ctx, user.ID)
	if err != nil {
		return PublicIndex{}, fmt.Errorf("list public booking links: %w", err)
	}

	visible := make([]PublicIndexLink, 0, len(links))
	for _, link := range links {
		paused, err := s.publicBookings.isPaused(ctx, link)
		if err != nil {
			return PublicIndex{}, err
		}
		if paused {
			continue
		}
		visible = append(visible, PublicIndexLink{Slug: link.Slug, Title: link.Title, DurationMinutes: link.DurationMinutes})
	}

	if len(visible) == 0 {
		return PublicIndex{}, nil
	}

	return PublicIndex{HostName: user.Name, Links: visible}, nil
}
