package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
)

// LoadInvitation resolves eventID + recipientUserID into the Event —
// hydrated with its Organizer's Name/Email and its full current Attendee
// list (ADR-0062), but never its Reminders, so icalendar.InvitationToICal
// renders no VALARM (ADR-0059) — and masterAnchor, the Master's own row when
// eventID names an Override (InvitationToICal's RECURRENCE-ID formatting
// contract), nil otherwise. Called by the outbox Worker's Sender with no
// acting User in view, so it skips every Access check getVisibleEvent would
// run — the invite was already authorized when the Attendee row (and this
// Invitation's outbox row) was written.
//
// ok is false when there is nothing left to send: eventID no longer
// resolves, or recipientUserID is no longer one of its Attendees — either
// removed, or the Event deleted, since the outbox row was queued. Neither
// case is an error; it just means the Worker has nothing to do.
func (s *EventService) LoadInvitation(ctx context.Context, eventID string, recipientUserID int64) (event repository.Event, masterAnchor *repository.Event, ok bool, err error) {
	return s.loadInvitationForAttendee(ctx, eventID, func(a repository.AttendeeWithName) bool {
		return a.UserID != nil && *a.UserID == recipientUserID
	})
}

// LoadInvitationForEmail is LoadInvitation's email-shaped counterpart
// (ADR-0058, #200): the same resolution and hydration, but matching the
// current Attendee list on recipientEmail — already normalized by the
// caller — instead of a user_id, since an email-shaped Attendee has no
// account to key on.
func (s *EventService) LoadInvitationForEmail(ctx context.Context, eventID string, recipientEmail string) (event repository.Event, masterAnchor *repository.Event, ok bool, err error) {
	return s.loadInvitationForAttendee(ctx, eventID, func(a repository.AttendeeWithName) bool {
		return a.UserID == nil && a.Email == recipientEmail
	})
}

// loadInvitationForAttendee is LoadInvitation's and LoadInvitationForEmail's
// shared body: resolve eventID, find the current Attendee row matches
// picks out, and hydrate Organizer/masterAnchor around it.
func (s *EventService) loadInvitationForAttendee(ctx context.Context, eventID string, matches func(repository.AttendeeWithName) bool) (event repository.Event, masterAnchor *repository.Event, ok bool, err error) {
	e, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Event{}, nil, false, nil
		}
		return repository.Event{}, nil, false, fmt.Errorf("get event: %w", err)
	}

	attendees, err := s.attendees.ListByEventID(ctx, eventID)
	if err != nil {
		return repository.Event{}, nil, false, fmt.Errorf("list attendees: %w", err)
	}
	found := false
	for _, a := range attendees {
		if matches(a) {
			found = true
			break
		}
	}
	if !found {
		return repository.Event{}, nil, false, nil
	}
	e.Attendees = attendees

	if e.CreatedBy != nil {
		organizer, err := s.users.GetByID(ctx, *e.CreatedBy)
		if err != nil {
			if !errors.Is(err, repository.ErrNotFound) {
				return repository.Event{}, nil, false, fmt.Errorf("get organizer: %w", err)
			}
		} else {
			e.CreatedByName = organizer.Name
			e.CreatedByEmail = organizer.Email
		}
	}

	if e.ParentID != nil {
		master, err := s.events.GetByID(ctx, *e.ParentID)
		if err != nil {
			return repository.Event{}, nil, false, fmt.Errorf("get master event: %w", err)
		}
		masterAnchor = &master
	}

	return e, masterAnchor, true, nil
}

func isValidResponse(response string) bool {
	switch response {
	case repository.ResponseNeedsAction, repository.ResponseAccepted, repository.ResponseDeclined, repository.ResponseTentative:
		return true
	default:
		return false
	}
}

// SetResponse sets userID's own response to eventID, the only response a
// caller may ever change here — there is no argument naming a different
// target user, so an organizer or Editor managing Attendees can add or
// remove them but can never set their response for them (#161, ADR-0046).
// Returns repository.ErrNotFound if userID isn't an Attendee of eventID.
func (s *EventService) SetResponse(ctx context.Context, userID int64, eventID, response string) (repository.Attendee, error) {
	if !isValidResponse(response) {
		return repository.Attendee{}, ErrInvalidResponse
	}
	if _, err := s.attendees.Get(ctx, eventID, userID); err != nil {
		return repository.Attendee{}, err
	}
	return s.attendees.SetResponse(ctx, eventID, userID, response)
}

// ApplyReply resolves an inbound METHOD:REPLY (icalendar.ParsedReply) and
// writes the Response it names — the reply poller's write path (#202,
// ADR-0059), called with no acting User in view, so it runs no Access check:
// the right to answer for an Attendee row was already established when they
// were invited, the same reasoning loadInvitationForAttendee applies for
// outbound sends. applied is false, with no error, when there is nowhere
// for the reply to go — an unknown UID or an address that isn't an Attendee
// of the resolved row — so the caller logs it and drops it rather than
// guessing (ADR-0059).
func (s *EventService) ApplyReply(ctx context.Context, reply icalendar.ParsedReply) (applied bool, err error) {
	row, err := s.resolveReplyEvent(ctx, reply.UID, reply.RecurrenceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("resolve reply event: %w", err)
	}

	attendees, err := s.attendees.ListByEventID(ctx, row.ID)
	if err != nil {
		return false, fmt.Errorf("list attendees: %w", err)
	}

	for _, a := range attendees {
		if !strings.EqualFold(a.Email, reply.Attendee) {
			continue
		}
		if a.UserID != nil {
			if _, err := s.attendees.SetResponse(ctx, row.ID, *a.UserID, reply.Response); err != nil {
				return false, fmt.Errorf("set response: %w", err)
			}
		} else if _, err := s.attendees.SetResponseByEmail(ctx, row.ID, a.Email, reply.Response); err != nil {
			return false, fmt.Errorf("set response: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// resolveReplyEvent resolves a REPLY's (UID, RecurrenceID) to the Event row
// that carries the Attendee it answers for: the Override row for that
// Occurrence when one exists (findOverrideForOccurrence, shared with
// GetOccurrence's own resolution so the two can't drift apart on what
// counts as a match), else the Master's own row — since an Attendee invited
// to a whole series has only the Master's row to answer on until that
// Occurrence is split off into its own Override.
func (s *EventService) resolveReplyEvent(ctx context.Context, uid string, recurrenceID *time.Time) (repository.Event, error) {
	master, err := s.events.GetByID(ctx, uid)
	if err != nil {
		return repository.Event{}, err
	}
	if recurrenceID == nil {
		return master, nil
	}

	children, err := s.events.ListChildrenByParentIDs(ctx, []string{uid})
	if err != nil {
		return repository.Event{}, fmt.Errorf("list children: %w", err)
	}
	if override, ok := findOverrideForOccurrence(children[uid], *recurrenceID); ok {
		return override, nil
	}
	return master, nil
}
