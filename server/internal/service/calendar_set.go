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
	sets *repository.CalendarSetRepository
}

func NewCalendarSetService(sets *repository.CalendarSetRepository) *CalendarSetService {
	return &CalendarSetService{sets: sets}
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

// ListForUser returns every Calendar Set userID owns inside workspaceID.
func (s *CalendarSetService) ListForUser(ctx context.Context, userID, workspaceID int64) ([]repository.CalendarSet, error) {
	sets, err := s.sets.ListForUser(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list calendar sets: %w", err)
	}
	return sets, nil
}

// Rename changes id's name, scoped to userID and workspaceID.
func (s *CalendarSetService) Rename(ctx context.Context, userID, workspaceID, id int64, name string) (repository.CalendarSet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.CalendarSet{}, ErrInvalidCalendarSetName
	}

	renamed, err := s.sets.Rename(ctx, id, userID, workspaceID, name)
	if err != nil {
		return repository.CalendarSet{}, fmt.Errorf("rename calendar set: %w", err)
	}
	return renamed, nil
}

// Delete removes id, scoped to userID and workspaceID. Destroys no Calendar
// and no Event — only the Set and its calendar_set_members rows.
func (s *CalendarSetService) Delete(ctx context.Context, userID, workspaceID, id int64) error {
	if err := s.sets.Delete(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("delete calendar set: %w", err)
	}
	return nil
}
