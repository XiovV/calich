package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/XiovV/calich/server/internal/repository"
)

// ErrInvalidCalendarSetName is returned by CalendarSetService.Create and
// Rename when name is empty.
var ErrInvalidCalendarSetName = errors.New("calendar set name must not be empty")

// CalendarSetService creates, renames, lists and deletes Calendar Sets
// (ADR-0082): a named, private selection of a Workspace's Calendars
// belonging to one User. Private outright — there is no sharing mechanism
// and no Role, so unlike GroupService this performs no
// requireWorkspaceOwnerOrAdmin-style authority check; every method is
// scoped to the caller's own userID and workspaceID by the repository call
// itself, and a Set belonging to someone else surfaces as
// repository.ErrNotFound rather than any distinguishable "forbidden".
type CalendarSetService struct {
	sets      *repository.CalendarSetRepository
	calendars *CalendarService
}

func NewCalendarSetService(sets *repository.CalendarSetRepository, calendars *CalendarService) *CalendarSetService {
	return &CalendarSetService{sets: sets, calendars: calendars}
}

// CalendarSetWithMembers pairs a Calendar Set with its member Calendar ids
// (ADR-0082's REST surface) — List's own shape, since the Set switcher and
// the membership dialog both need a Set's membership the moment they have
// the Set itself, with no second round trip.
type CalendarSetWithMembers struct {
	repository.CalendarSet
	CalendarIDs []string
}

// Create makes a new Calendar Set named name, owned by userID inside
// workspaceID. Membership is added separately.
func (s *CalendarSetService) Create(ctx context.Context, userID, workspaceID int64, name string) (repository.CalendarSet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.CalendarSet{}, ErrInvalidCalendarSetName
	}

	set, err := s.sets.Create(ctx, userID, workspaceID, name)
	if err != nil {
		return repository.CalendarSet{}, fmt.Errorf("create calendar set: %w", err)
	}
	return set, nil
}

// ListForUser returns every Calendar Set userID owns inside workspaceID,
// each with its member Calendar ids.
func (s *CalendarSetService) ListForUser(ctx context.Context, userID, workspaceID int64) ([]CalendarSetWithMembers, error) {
	sets, err := s.sets.ListForUser(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list calendar sets: %w", err)
	}

	result := make([]CalendarSetWithMembers, len(sets))
	for i, set := range sets {
		ids, err := s.sets.ListCalendarIDs(ctx, set.ID)
		if err != nil {
			return nil, fmt.Errorf("list calendar set members: %w", err)
		}
		result[i] = CalendarSetWithMembers{CalendarSet: set, CalendarIDs: ids}
	}
	return result, nil
}

// Rename changes id's name, scoped to userID and workspaceID, and returns
// it with its existing membership intact — unlike a fresh Create, a renamed
// Set is not membership-free, so the response must carry its real
// CalendarIDs rather than an empty placeholder (a client that replaces its
// cached Set wholesale with this response, as calendarSetsStore does, would
// otherwise wipe a populated Set's membership from the UI on every rename).
func (s *CalendarSetService) Rename(ctx context.Context, userID, workspaceID, id int64, name string) (CalendarSetWithMembers, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CalendarSetWithMembers{}, ErrInvalidCalendarSetName
	}

	renamed, err := s.sets.Rename(ctx, id, userID, workspaceID, name)
	if err != nil {
		return CalendarSetWithMembers{}, fmt.Errorf("rename calendar set: %w", err)
	}

	ids, err := s.sets.ListCalendarIDs(ctx, renamed.ID)
	if err != nil {
		return CalendarSetWithMembers{}, fmt.Errorf("list calendar set members: %w", err)
	}
	return CalendarSetWithMembers{CalendarSet: renamed, CalendarIDs: ids}, nil
}

// Delete removes id, scoped to userID and workspaceID. Destroys no Calendar
// and no Event — only the Set and its calendar_set_members rows.
func (s *CalendarSetService) Delete(ctx context.Context, userID, workspaceID, id int64) error {
	if err := s.sets.Delete(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("delete calendar set: %w", err)
	}
	return nil
}

// AddCalendar puts calendarID into id's membership (#302, ADR-0082), scoped
// to userID and workspaceID like every other method here. Validates two
// things independently — that calendarID belongs to workspaceID, and that
// userID has at least Viewer Access to it — and collapses either failure to
// repository.ErrNotFound, the same "can't distinguish refusal from absence"
// posture GetByID already applies to the Set itself. Idempotent: a Calendar
// already in the Set is not an error.
func (s *CalendarSetService) AddCalendar(ctx context.Context, userID, workspaceID, id int64, calendarID string) error {
	if _, err := s.sets.GetByID(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("get calendar set: %w", err)
	}

	access, calendar, err := s.calendars.Access(ctx, userID, calendarID)
	if err != nil {
		return fmt.Errorf("resolve calendar access: %w", err)
	}
	if !access.CanRead() || calendar.WorkspaceID != workspaceID {
		return repository.ErrNotFound
	}

	if err := s.sets.AddCalendar(ctx, id, calendarID); err != nil {
		return fmt.Errorf("add calendar to calendar set: %w", err)
	}
	return nil
}

// RemoveCalendar takes calendarID out of id's membership (#302, ADR-0082),
// scoped like AddCalendar. No fresh Access check: removing a reference the
// caller already curated needs no re-proof that they can still see the
// Calendar, mirroring Delete's own posture toward the Set itself.
func (s *CalendarSetService) RemoveCalendar(ctx context.Context, userID, workspaceID, id int64, calendarID string) error {
	if _, err := s.sets.GetByID(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("get calendar set: %w", err)
	}

	if err := s.sets.RemoveCalendar(ctx, id, calendarID); err != nil {
		return fmt.Errorf("remove calendar from calendar set: %w", err)
	}
	return nil
}
