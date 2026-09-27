package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/XiovV/calich/server/internal/repository"
)

var (
	// ErrInvalidBookingLinkTitle is returned by Create and Update when Title is empty.
	ErrInvalidBookingLinkTitle = errors.New("booking link title must not be empty")

	// ErrInvalidSlug is returned by Create and Update when Slug fails
	// validateSlug — empty, over maxSlugLength, or outside slugPattern's
	// charset (#322, ADR-0084).
	ErrInvalidSlug = errors.New("slug must be 1-39 characters, lowercase letters, digits and single hyphens only, and may not start or end with a hyphen")

	// ErrInvalidDuration is returned by Create and Update when
	// DurationMinutes is outside minDurationMinutes..maxDurationMinutes.
	ErrInvalidDuration = errors.New("duration must be between 1 and 1440 minutes")

	// ErrInvalidVisibility is returned by Create and Update when Visibility
	// isn't one of public/private/paused (ADR-0087).
	ErrInvalidVisibility = errors.New("visibility must be one of public, private, paused")

	// ErrInvalidMinimumNotice is returned by Create and Update when
	// MinimumNoticeMinutes is negative.
	ErrInvalidMinimumNotice = errors.New("minimum notice must not be negative")

	// ErrInvalidBookingHorizon is returned by Create and Update when
	// BookingHorizonDays is under 1.
	ErrInvalidBookingHorizon = errors.New("booking horizon must be at least 1 day")

	// ErrScheduleNotFound is returned by Create and Update when
	// AvailabilityScheduleID doesn't name a Schedule the caller owns.
	ErrScheduleNotFound = errors.New("availability schedule not found")

	// ErrBookIntoCalendarNotFound is returned by Create and Update when
	// BookIntoCalendarID doesn't name a Calendar in the active Workspace
	// the caller owns or can edit (#322's "Book-into is restricted to
	// Calendars the User owns or can edit in that Workspace") —
	// deliberately collapsing "doesn't exist" and "insufficient Access"
	// into one answer, the same posture CalendarSetService.AddCalendar
	// already takes toward a Calendar Set's own membership.
	ErrBookIntoCalendarNotFound = errors.New("book-into calendar not found")

	// ErrCannotRemoveBookIntoCalendar is returned by RemoveConflictCalendar
	// when calendarID is the link's own Book-into Calendar — permanently in
	// the Conflict set and unremovable (ADR-0087).
	ErrCannotRemoveBookIntoCalendar = errors.New("cannot remove the book-into calendar from the conflict set")

	// ErrSlugTaken mirrors repository.ErrSlugTaken so handlers only import
	// the service package's sentinels.
	ErrSlugTaken = repository.ErrSlugTaken
)

// BookingLinkWrite is the Create/Update input (#322): every field Create
// and Update both take, gathered into one value the same way CalendarWrite
// already gathers a Calendar's — validated wholesale by validateWrite
// before either ever reaches the repository.
type BookingLinkWrite struct {
	Title                  string
	Slug                   string
	DurationMinutes        int
	Visibility             string
	AvailabilityScheduleID int64
	BookIntoCalendarID     string
	Location               string
	Description            string
	MinimumNoticeMinutes   int
	BookingHorizonDays     int
	TasksInConflictSet     bool
}

// BookingLinkService creates, lists, updates, duplicates and deletes
// Booking Links (#322, ADR-0084, ADR-0087), and manages each one's Conflict
// set. Scoped to the caller's own (userID, workspaceID) on the same terms
// as TaskListService — every method resolves by (id, userID, workspaceID)
// at the repository layer and surfaces a mismatch as repository.ErrNotFound,
// never a distinguishable "forbidden".
type BookingLinkService struct {
	db           *sql.DB
	links        *repository.BookingLinkRepository
	calendarRepo *repository.CalendarRepository
	calendars    *CalendarService
	schedules    *repository.AvailabilityScheduleRepository
	auth         *AuthService
}

func NewBookingLinkService(db *sql.DB, links *repository.BookingLinkRepository, calendarRepo *repository.CalendarRepository, calendars *CalendarService, schedules *repository.AvailabilityScheduleRepository, auth *AuthService) *BookingLinkService {
	return &BookingLinkService{db: db, links: links, calendarRepo: calendarRepo, calendars: calendars, schedules: schedules, auth: auth}
}

// ListForUser returns every Booking Link userID owns inside workspaceID.
func (s *BookingLinkService) ListForUser(ctx context.Context, userID, workspaceID int64) ([]repository.BookingLink, error) {
	links, err := s.links.ListForUser(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list booking links: %w", err)
	}
	return links, nil
}

// Create makes a new Booking Link owned by userID inside workspaceID. Its
// Conflict set is seeded automatically — every Calendar userID owns in
// workspaceID, plus the Book-into Calendar itself, pinned — since the
// create modal's face never asks for one (#322: Conflict set sits behind
// More options, and a link created without opening it must still be
// usable). Claims a Handle for userID if this turns out to be their first
// Booking Link and they have none yet (ADR-0084).
func (s *BookingLinkService) Create(ctx context.Context, userID, workspaceID int64, write BookingLinkWrite) (repository.BookingLink, error) {
	fields, err := s.validateWrite(ctx, userID, workspaceID, write)
	if err != nil {
		return repository.BookingLink{}, err
	}

	// Read before the transaction opens, not inside it: this app's test
	// database caps the pool at one connection (testdb.go), so a read
	// through s.calendarRepo's own, non-tx-bound *sql.DB while the
	// transaction below already holds that one connection would block
	// forever waiting for a second connection nothing will ever release.
	seedIDs, err := s.seedConflictCalendarIDs(ctx, userID, workspaceID, fields.BookIntoCalendarID)
	if err != nil {
		return repository.BookingLink{}, err
	}

	var created repository.BookingLink
	err = repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txLinks := s.links.WithTx(tx)

		link, err := txLinks.Create(ctx, userID, workspaceID, fields)
		if err != nil {
			return err
		}

		for _, calendarID := range seedIDs {
			if err := txLinks.AddConflictCalendar(ctx, link.ID, calendarID); err != nil {
				return err
			}
		}

		created, err = txLinks.GetByID(ctx, link.ID, userID, workspaceID)
		return err
	})
	if err != nil {
		return repository.BookingLink{}, s.translateWriteError(err, "create booking link")
	}

	s.claimHandleIfNeeded(ctx, userID)

	return created, nil
}

// Update replaces id's fields wholesale, scoped to userID and workspaceID.
// Never touches the Conflict set's Calendar membership — that's
// AddConflictCalendar/RemoveConflictCalendar's job — except to re-pin
// whichever Calendar Book-into now names, in case it just changed.
func (s *BookingLinkService) Update(ctx context.Context, userID, workspaceID, id int64, write BookingLinkWrite) (repository.BookingLink, error) {
	fields, err := s.validateWrite(ctx, userID, workspaceID, write)
	if err != nil {
		return repository.BookingLink{}, err
	}

	var updated repository.BookingLink
	err = repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txLinks := s.links.WithTx(tx)

		if _, err := txLinks.Update(ctx, id, userID, workspaceID, fields); err != nil {
			return err
		}
		if err := txLinks.AddConflictCalendar(ctx, id, fields.BookIntoCalendarID); err != nil {
			return err
		}

		var err error
		updated, err = txLinks.GetByID(ctx, id, userID, workspaceID)
		return err
	})
	if err != nil {
		return repository.BookingLink{}, s.translateWriteError(err, "update booking link")
	}
	return updated, nil
}

// Delete removes id outright, scoped to userID and workspaceID. Its
// Conflict set rows cascade.
func (s *BookingLinkService) Delete(ctx context.Context, userID, workspaceID, id int64) error {
	if err := s.links.Delete(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("delete booking link: %w", err)
	}
	return nil
}

// Duplicate clones id into a new Booking Link (#322), deriving a fresh
// per-User-unique Slug ("<slug>-copy", then "<slug>-copy-2", ...) and Title
// ("<title> (copy)"), and copying its Conflict set and
// Tasks-in-Conflict-set flag verbatim — unlike Create's fresh seed, a
// duplicate preserves whatever customization the original's Conflict set
// already carries. May also claim a Handle, on the same terms as Create.
func (s *BookingLinkService) Duplicate(ctx context.Context, userID, workspaceID, id int64) (repository.BookingLink, error) {
	source, err := s.links.GetByID(ctx, id, userID, workspaceID)
	if err != nil {
		return repository.BookingLink{}, fmt.Errorf("get booking link: %w", err)
	}

	slug, err := s.freeSlug(ctx, userID, truncateSlug(source.Slug+"-copy"))
	if err != nil {
		return repository.BookingLink{}, err
	}

	fields := repository.BookingLinkFields{
		Title:                  source.Title + " (copy)",
		Slug:                   slug,
		DurationMinutes:        source.DurationMinutes,
		Visibility:             source.Visibility,
		AvailabilityScheduleID: source.AvailabilityScheduleID,
		BookIntoCalendarID:     source.BookIntoCalendarID,
		Location:               source.Location,
		Description:            source.Description,
		MinimumNoticeMinutes:   source.MinimumNoticeMinutes,
		BookingHorizonDays:     source.BookingHorizonDays,
		TasksInConflictSet:     source.TasksInConflictSet,
	}

	var duplicated repository.BookingLink
	err = repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txLinks := s.links.WithTx(tx)

		link, err := txLinks.Create(ctx, userID, workspaceID, fields)
		if err != nil {
			return err
		}
		for _, calendarID := range source.ConflictCalendarIDs {
			if err := txLinks.AddConflictCalendar(ctx, link.ID, calendarID); err != nil {
				return err
			}
		}

		duplicated, err = txLinks.GetByID(ctx, link.ID, userID, workspaceID)
		return err
	})
	if err != nil {
		return repository.BookingLink{}, s.translateWriteError(err, "duplicate booking link")
	}

	s.claimHandleIfNeeded(ctx, userID)

	return duplicated, nil
}

// AddConflictCalendar puts calendarID into id's Conflict set, scoped to
// userID and workspaceID. Validates that calendarID belongs to workspaceID
// and userID has at least Viewer Access to it, mirroring
// CalendarSetService.AddCalendar's own check — deriving availability (#323)
// only needs to read a Calendar's Events, not edit them.
func (s *BookingLinkService) AddConflictCalendar(ctx context.Context, userID, workspaceID, id int64, calendarID string) error {
	if _, err := s.links.GetByID(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("get booking link: %w", err)
	}

	access, calendar, err := s.calendars.Access(ctx, userID, calendarID)
	if err != nil {
		return fmt.Errorf("resolve calendar access: %w", err)
	}
	if !access.CanRead() || calendar.WorkspaceID != workspaceID {
		return repository.ErrNotFound
	}

	if err := s.links.AddConflictCalendar(ctx, id, calendarID); err != nil {
		return fmt.Errorf("add conflict calendar: %w", err)
	}
	return nil
}

// RemoveConflictCalendar takes calendarID out of id's Conflict set, scoped
// like AddConflictCalendar. Refuses removing the Book-into Calendar
// (ErrCannotRemoveBookIntoCalendar) — it is permanently in the set and
// unremovable (ADR-0087).
func (s *BookingLinkService) RemoveConflictCalendar(ctx context.Context, userID, workspaceID, id int64, calendarID string) error {
	link, err := s.links.GetByID(ctx, id, userID, workspaceID)
	if err != nil {
		return fmt.Errorf("get booking link: %w", err)
	}
	if link.BookIntoCalendarID == calendarID {
		return ErrCannotRemoveBookIntoCalendar
	}

	if err := s.links.RemoveConflictCalendar(ctx, id, calendarID); err != nil {
		return fmt.Errorf("remove conflict calendar: %w", err)
	}
	return nil
}

// validateWrite checks write against every Create/Update rule and resolves
// its two references (AvailabilityScheduleID, BookIntoCalendarID) against
// what userID actually owns or can reach in workspaceID, before either
// caller ever touches the repository.
func (s *BookingLinkService) validateWrite(ctx context.Context, userID, workspaceID int64, write BookingLinkWrite) (repository.BookingLinkFields, error) {
	title := strings.TrimSpace(write.Title)
	if title == "" {
		return repository.BookingLinkFields{}, ErrInvalidBookingLinkTitle
	}

	slug, err := validateSlug(write.Slug)
	if err != nil {
		return repository.BookingLinkFields{}, err
	}

	if write.DurationMinutes < minDurationMinutes || write.DurationMinutes > maxDurationMinutes {
		return repository.BookingLinkFields{}, ErrInvalidDuration
	}

	visibility := strings.ToLower(strings.TrimSpace(write.Visibility))
	if !validVisibilities[visibility] {
		return repository.BookingLinkFields{}, ErrInvalidVisibility
	}

	if write.MinimumNoticeMinutes < 0 {
		return repository.BookingLinkFields{}, ErrInvalidMinimumNotice
	}
	if write.BookingHorizonDays < 1 {
		return repository.BookingLinkFields{}, ErrInvalidBookingHorizon
	}

	if _, err := s.schedules.GetByID(ctx, write.AvailabilityScheduleID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.BookingLinkFields{}, ErrScheduleNotFound
		}
		return repository.BookingLinkFields{}, fmt.Errorf("get availability schedule: %w", err)
	}

	access, calendar, err := s.calendars.Access(ctx, userID, write.BookIntoCalendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.BookingLinkFields{}, ErrBookIntoCalendarNotFound
		}
		return repository.BookingLinkFields{}, fmt.Errorf("resolve book-into calendar access: %w", err)
	}
	if !access.CanWrite() || calendar.WorkspaceID != workspaceID {
		return repository.BookingLinkFields{}, ErrBookIntoCalendarNotFound
	}

	return repository.BookingLinkFields{
		Title:                  title,
		Slug:                   slug,
		DurationMinutes:        write.DurationMinutes,
		Visibility:             visibility,
		AvailabilityScheduleID: write.AvailabilityScheduleID,
		BookIntoCalendarID:     write.BookIntoCalendarID,
		Location:               write.Location,
		Description:            write.Description,
		MinimumNoticeMinutes:   write.MinimumNoticeMinutes,
		BookingHorizonDays:     write.BookingHorizonDays,
		TasksInConflictSet:     write.TasksInConflictSet,
	}, nil
}

// seedConflictCalendarIDs is Create's default Conflict set (#322, ADR-0087):
// every Calendar userID owns in workspaceID, plus bookIntoCalendarID itself
// — present unconditionally, since Book-into is pinned and unremovable even
// on the rare create where it names a Calendar the caller can edit but
// doesn't own (an Editor Share).
func (s *BookingLinkService) seedConflictCalendarIDs(ctx context.Context, userID, workspaceID int64, bookIntoCalendarID string) ([]string, error) {
	owned, err := s.calendarRepo.ListByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list owned calendars: %w", err)
	}

	ids := make([]string, 0, len(owned)+1)
	seenBookInto := false
	for _, c := range owned {
		ids = append(ids, c.ID)
		if c.ID == bookIntoCalendarID {
			seenBookInto = true
		}
	}
	if !seenBookInto {
		ids = append(ids, bookIntoCalendarID)
	}
	return ids, nil
}

// freeSlug returns the first of base, base-2, base-3, ... not already taken
// by userID, mirroring AuthService.SuggestHandle's own disambiguation loop
// but scoped per-User rather than instance-wide — a Slug is unique per
// User, not instance-wide (ADR-0084).
func (s *BookingLinkService) freeSlug(ctx context.Context, userID int64, base string) (string, error) {
	candidate := base
	for suffix := 2; ; suffix++ {
		_, err := s.links.GetBySlug(ctx, userID, candidate)
		if errors.Is(err, repository.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("check slug availability: %w", err)
		}
		candidate = appendSlugSuffix(base, suffix)
	}
}

// claimHandleIfNeeded auto-claims a Handle for userID the moment their
// Booking Link count first becomes 1 and they have none yet (#322,
// ADR-0084's "creating a first Booking Link claims a Handle"). Best-effort:
// a failure here (an unlucky race against another claim, say) costs userID
// nothing but the auto-claim itself — the Booking Link this call followed
// already committed, and Settings → Account (#321) remains available to
// claim one by hand. Failing the whole create/duplicate over this secondary
// effect would be far more confusing than silently skipping it.
func (s *BookingLinkService) claimHandleIfNeeded(ctx context.Context, userID int64) {
	count, err := s.links.CountForUser(ctx, userID)
	if err != nil || count != 1 {
		return
	}

	user, err := s.auth.GetUser(ctx, userID)
	if err != nil || user.Handle != nil {
		return
	}

	suggestion, err := s.auth.SuggestHandle(ctx, userID)
	if err != nil {
		return
	}
	_, _ = s.auth.UpdateHandle(ctx, userID, suggestion)
}

// translateWriteError maps the sentinels Create/Update/Duplicate's
// transactions can surface into this package's own, so a handler never
// needs to import repository directly for these.
func (s *BookingLinkService) translateWriteError(err error, msg string) error {
	if errors.Is(err, repository.ErrSlugTaken) {
		return ErrSlugTaken
	}
	if errors.Is(err, repository.ErrNotFound) {
		return repository.ErrNotFound
	}
	return fmt.Errorf("%s: %w", msg, err)
}
