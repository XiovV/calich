package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calendar/server/internal/repository"
)

var (
	ErrInvalidColor = errors.New("invalid calendar color")
	ErrInvalidName  = errors.New("calendar name must not be empty")
	// ErrInvalidRole is returned when a Share is granted with a Role other
	// than Viewer or Editor (ADR-0034).
	ErrInvalidRole = errors.New("role must be \"viewer\" or \"editor\"")
	// ErrUserNotFound is returned when Share names a username that doesn't
	// exist on the instance — a Share can only be granted to a User who
	// exists (#100's acceptance criteria).
	ErrUserNotFound = errors.New("user not found")
	// ErrCannotShareWithSelf is returned when Share names the Calendar's
	// own Owner — ownership already grants everything a Share could, and
	// an Owner can never hold a Share row of their own (ADR-0034).
	ErrCannotShareWithSelf = errors.New("cannot share a calendar with its owner")
)

type CalendarService struct {
	calendars         *repository.CalendarRepository
	shares            *repository.CalendarShareRepository
	users             *repository.UserRepository
	reminderOverrides *repository.ReminderOverrideRepository
	colorOverrides    *repository.CalendarUserColorRepository
}

func NewCalendarService(calendars *repository.CalendarRepository, shares *repository.CalendarShareRepository, users *repository.UserRepository, reminderOverrides *repository.ReminderOverrideRepository, colorOverrides *repository.CalendarUserColorRepository) *CalendarService {
	return &CalendarService{calendars: calendars, shares: shares, users: users, reminderOverrides: reminderOverrides, colorOverrides: colorOverrides}
}

// CalendarWrite is a Calendar's writable fields, gathered into one value the
// same way EventWrite already gathers an event's — so Create and Update take
// one argument each instead of separately threading every field.
type CalendarWrite struct {
	Name  string
	Color string
	// SourceURL is non-nil only when Create is subscribing to an external
	// feed (#83, ADR-0032) — never set by the plain create/update paths.
	SourceURL *string
	// KeepAlarms is set only alongside SourceURL, at Subscribe time (#87,
	// ADR-0032) — a later change goes through UpdateKeepAlarms instead,
	// since Update ignores this field just like SourceURL.
	KeepAlarms bool
	// FeedName and FeedColor are set only alongside SourceURL too, at
	// Subscribe time (#88, ADR-0032) — a later change goes through
	// RecordRefreshSuccess instead, since Update ignores these fields just
	// like SourceURL.
	FeedName, FeedColor *string
}

// fields projects the write onto the columns the repository stores.
func (w CalendarWrite) fields() repository.CalendarFields {
	return repository.CalendarFields{
		Name:       w.Name,
		Color:      w.Color,
		SourceURL:  w.SourceURL,
		KeepAlarms: w.KeepAlarms,
		FeedName:   w.FeedName,
		FeedColor:  w.FeedColor,
	}
}

func (s *CalendarService) Create(ctx context.Context, userID int64, id string, write CalendarWrite) (repository.Calendar, error) {
	write.Name = strings.TrimSpace(write.Name)
	if write.Name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	color, ok := NormalizeColor(write.Color)
	if !ok {
		return repository.Calendar{}, ErrInvalidColor
	}
	write.Color = color

	calendar, err := s.calendars.Create(ctx, userID, id, write.fields())
	if err != nil {
		return repository.Calendar{}, fmt.Errorf("create calendar: %w", err)
	}
	return calendar, nil
}

// List returns userID's owned Calendars only — for callers that name
// Calendars purely by ownership (Owner-only management, ADR-0034). A caller
// that wants everything userID has any Access to — owned and shared alike,
// including CalDAV's home-set (ADR-0035) — should call ListAccessible
// instead.
func (s *CalendarService) List(ctx context.Context, userID int64) ([]repository.Calendar, error) {
	calendars, err := s.calendars.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	return calendars, nil
}

// CalendarWithAccess pairs a Calendar with the caller's resolved Access to
// it (ADR-0034) and their resolved display colour (ADR-0038) — what
// ListAccessible returns, so a caller doesn't have to resolve either a
// second time per row.
type CalendarWithAccess struct {
	repository.Calendar
	Access Access
	// Color is the caller's resolved display colour (ADR-0038's
	// DisplayColor): their own override on this Calendar if they've set
	// one, otherwise the embedded Calendar's own Color. Deliberately named
	// to shadow the embedded field — c.Color means "what this caller
	// sees"; c.Calendar.Color remains reachable for a caller that
	// specifically wants the Owner's raw stored value.
	Color string
}

// ListAccessible returns every Calendar userID has any Access to — owned
// and shared alike (ADR-0034) — each paired with its resolved Access and
// resolved display colour, ordered by creation time. This is what a caller
// showing "everything I can see" — the Calendar list endpoint, Event
// listing — should call, unlike List's owned-only view.
func (s *CalendarService) ListAccessible(ctx context.Context, userID int64) ([]CalendarWithAccess, error) {
	owned, err := s.calendars.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list owned calendars: %w", err)
	}
	shared, err := s.calendars.ListSharedWithUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list shared calendars: %w", err)
	}

	result := make([]CalendarWithAccess, 0, len(owned)+len(shared))
	for _, c := range owned {
		color, err := s.resolveDisplayColor(ctx, userID, c)
		if err != nil {
			return nil, err
		}
		result = append(result, CalendarWithAccess{Calendar: c, Access: ResolveAccess(userID, c, nil), Color: color})
	}
	for _, c := range shared {
		role := c.Role
		color, err := s.resolveDisplayColor(ctx, userID, c.Calendar)
		if err != nil {
			return nil, err
		}
		result = append(result, CalendarWithAccess{Calendar: c.Calendar, Access: ResolveAccess(userID, c.Calendar, &role), Color: color})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// resolveDisplayColor computes DisplayColor(user, calendar) (ADR-0038): the
// User's own override on calendar if they've set one, otherwise the
// Calendar's own stored colour. The single place both the REST Calendar
// responses and CalDAV's calendar-color property resolve the value they
// show, so a Subscribed Calendar's publisher-tracking (ADR-0032) — which
// only ever touches the stored colour — is transparently overridden by a
// User's own choice without either mechanism knowing about the other.
func (s *CalendarService) resolveDisplayColor(ctx context.Context, userID int64, calendar repository.Calendar) (string, error) {
	override, err := s.colorOverrides.Get(ctx, userID, calendar.ID)
	if err == nil {
		return override, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return "", fmt.Errorf("get calendar color override: %w", err)
	}
	return calendar.Color, nil
}

// AccessWithColor resolves userID's Access to id's Calendar together with
// their resolved display colour (ADR-0038) — the REST single-Calendar fetch
// path, which must show the caller's own colour rather than always the
// Owner's raw stored value.
func (s *CalendarService) AccessWithColor(ctx context.Context, userID int64, id string) (CalendarWithAccess, error) {
	access, calendar, err := s.Access(ctx, userID, id)
	if err != nil {
		return CalendarWithAccess{}, err
	}
	color, err := s.resolveDisplayColor(ctx, userID, calendar)
	if err != nil {
		return CalendarWithAccess{}, err
	}
	return CalendarWithAccess{Calendar: calendar, Access: access, Color: color}, nil
}

// requireRead resolves calendarID and refuses it unless userID has at least
// Viewer Access — the CanRead counterpart to requireOwner, for operations
// like a colour override that are open to any Access level rather than
// Owner-only.
func (s *CalendarService) requireRead(ctx context.Context, userID int64, calendarID string) error {
	access, _, err := s.Access(ctx, userID, calendarID)
	if err != nil {
		return err
	}
	if !access.CanRead() {
		return repository.ErrNotFound
	}
	return nil
}

// SetColorOverride sets userID's personal colour override on calendarID
// (ADR-0038): any User with at least Viewer Access may call this, since a
// colour override is a personal display preference rather than a write to
// the Calendar itself — mirroring EventService.SetReminderOverride's
// CanRead gate, not requireOwner. Unlike Update, this never touches
// calendars.color and so is unaffected by a Subscribed Calendar's
// publisher-tracking (ADR-0032): the override wins for this User
// regardless of what the feed sends.
func (s *CalendarService) SetColorOverride(ctx context.Context, userID int64, calendarID, color string) (string, error) {
	if err := s.requireRead(ctx, userID, calendarID); err != nil {
		return "", err
	}

	normalized, ok := NormalizeColor(color)
	if !ok {
		return "", ErrInvalidColor
	}

	if err := s.colorOverrides.Upsert(ctx, userID, calendarID, normalized); err != nil {
		return "", fmt.Errorf("set calendar color override: %w", err)
	}
	return normalized, nil
}

// ClearColorOverride removes userID's personal colour override on
// calendarID (ADR-0038) — the User falling back to the Calendar's own
// colour. Requires the same Access as SetColorOverride, for symmetry.
func (s *CalendarService) ClearColorOverride(ctx context.Context, userID int64, calendarID string) error {
	if err := s.requireRead(ctx, userID, calendarID); err != nil {
		return err
	}

	if err := s.colorOverrides.Delete(ctx, userID, calendarID); err != nil {
		return fmt.Errorf("clear calendar color override: %w", err)
	}
	return nil
}

// Access resolves userID's Access to id's Calendar (ADR-0034) — the single
// place every Calendar and Event permission check goes through. Unlike
// Get, it fetches id regardless of who owns it, since the point is to
// compute the answer rather than assume it.
func (s *CalendarService) Access(ctx context.Context, userID int64, id string) (Access, repository.Calendar, error) {
	calendar, err := s.calendars.GetByIDAny(ctx, id)
	if err != nil {
		return AccessNone, repository.Calendar{}, err
	}

	var role *string
	if calendar.UserID != userID {
		share, err := s.shares.Get(ctx, id, userID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return AccessNone, repository.Calendar{}, fmt.Errorf("get calendar share: %w", err)
		}
		if err == nil {
			role = &share.Role
		}
	}

	return ResolveAccess(userID, calendar, role), calendar, nil
}

// requireOwner resolves id and refuses it unless userID owns it —
// re-sharing, revoking, and listing who has Access are Owner-only
// management operations no Role, however permissive, grants (ADR-0034,
// CONTEXT.md's Owner entry), mirroring the not-found-not-forbidden
// convention Update and Delete already apply via their own
// ownership-filtered queries.
func (s *CalendarService) requireOwner(ctx context.Context, userID int64, id string) (repository.Calendar, error) {
	calendar, err := s.calendars.GetByIDAny(ctx, id)
	if err != nil {
		return repository.Calendar{}, err
	}
	if calendar.UserID != userID {
		return repository.Calendar{}, repository.ErrNotFound
	}
	return calendar, nil
}

func isValidRole(role string) bool {
	return role == repository.RoleViewer || role == repository.RoleEditor
}

// Share grants calendarID a Share to username with role, or changes an
// existing Share's role if username already has one (ADR-0034). Only
// calendarID's Owner may call this.
func (s *CalendarService) Share(ctx context.Context, ownerID int64, calendarID, username, role string) (repository.CalendarShare, error) {
	if !isValidRole(role) {
		return repository.CalendarShare{}, ErrInvalidRole
	}

	calendar, err := s.requireOwner(ctx, ownerID, calendarID)
	if err != nil {
		return repository.CalendarShare{}, err
	}

	target, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.CalendarShare{}, ErrUserNotFound
		}
		return repository.CalendarShare{}, fmt.Errorf("look up user: %w", err)
	}
	// A Disabled User is hidden from the share picker (ADR-0037) — from the
	// Owner's perspective they don't exist to share with, same as a username
	// that was never registered.
	if target.IsDisabled {
		return repository.CalendarShare{}, ErrUserNotFound
	}
	if target.ID == calendar.UserID {
		return repository.CalendarShare{}, ErrCannotShareWithSelf
	}

	share, err := s.shares.Upsert(ctx, calendarID, target.ID, role)
	if err != nil {
		return repository.CalendarShare{}, fmt.Errorf("upsert share: %w", err)
	}
	return share, nil
}

// RevokeShare removes targetUserID's Share on calendarID. Only calendarID's
// Owner may call this. targetUserID's Reminder overrides on calendarID's
// Events, and their colour override on calendarID itself, are cleared with
// it — an override with no Access behind it would otherwise linger,
// invisible, until targetUserID was ever shared with it again (ADR-0036's
// and ADR-0038's acceptance criteria).
func (s *CalendarService) RevokeShare(ctx context.Context, ownerID int64, calendarID string, targetUserID int64) error {
	if _, err := s.requireOwner(ctx, ownerID, calendarID); err != nil {
		return err
	}
	if err := s.shares.Delete(ctx, calendarID, targetUserID); err != nil {
		return err
	}
	if err := s.reminderOverrides.DeleteByUserAndCalendar(ctx, targetUserID, calendarID); err != nil {
		return fmt.Errorf("clear reminder overrides: %w", err)
	}
	if err := s.colorOverrides.Delete(ctx, targetUserID, calendarID); err != nil {
		return fmt.Errorf("clear calendar color override: %w", err)
	}
	return nil
}

// ListShares returns every Share on calendarID, each carrying the Username
// it was granted to — an Owner's "who has Access to my Calendar, and with
// what Role" listing (ADR-0034). Only calendarID's Owner may call this.
func (s *CalendarService) ListShares(ctx context.Context, ownerID int64, calendarID string) ([]repository.CalendarShareWithUsername, error) {
	if _, err := s.requireOwner(ctx, ownerID, calendarID); err != nil {
		return nil, err
	}
	return s.shares.ListByCalendarWithUsername(ctx, calendarID)
}

// LeaveShare removes userID's own Share on calendarID — a User renouncing
// their Access without involving the Owner (ADR-0034). Unlike RevokeShare,
// this needs no ownership check: it only ever removes the caller's own
// Share row, so there's nothing else to authorize. Returns
// repository.ErrNotFound if userID holds no Share on calendarID (including
// when userID is the Owner, who never has one). userID's Reminder overrides
// on calendarID's Events, and their colour override on calendarID itself,
// are cleared with it, mirroring RevokeShare (ADR-0036, ADR-0038).
func (s *CalendarService) LeaveShare(ctx context.Context, userID int64, calendarID string) error {
	if err := s.shares.Delete(ctx, calendarID, userID); err != nil {
		return err
	}
	if err := s.reminderOverrides.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		return fmt.Errorf("clear reminder overrides: %w", err)
	}
	if err := s.colorOverrides.Delete(ctx, userID, calendarID); err != nil {
		return fmt.Errorf("clear calendar color override: %w", err)
	}
	return nil
}

// Get returns id if userID can read it. Callers that also need the resolved
// Access value itself — not just whether it clears the CanRead bar — should
// call Access directly instead, rather than resolving it here and
// discarding it.
func (s *CalendarService) Get(ctx context.Context, userID int64, id string) (repository.Calendar, error) {
	access, calendar, err := s.Access(ctx, userID, id)
	if err != nil {
		return repository.Calendar{}, err
	}
	if !access.CanRead() {
		return repository.Calendar{}, repository.ErrNotFound
	}
	return calendar, nil
}

// Update and Delete are Owner-only management operations (rename, recolour,
// delete — CONTEXT.md's Owner entry) that stay scoped by
// CalendarRepository's own `WHERE user_id = ?`, deliberately not routed
// through Access: Access answers what a User may do with a Calendar's
// Events and clamps a Subscribed Calendar to read-only even for its Owner,
// but that Owner must still be able to manage the Calendar itself (rename
// their own feed, turn KeepAlarms on and off, ...). ADR-0034 checks these
// separately from Role for exactly that reason, and the repository's
// unclamped ownership filter already expresses it — a second, service-level
// ownership check here would only duplicate it.
func (s *CalendarService) Update(ctx context.Context, userID int64, id string, write CalendarWrite) (repository.Calendar, error) {
	write.Name = strings.TrimSpace(write.Name)
	if write.Name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	color, ok := NormalizeColor(write.Color)
	if !ok {
		return repository.Calendar{}, ErrInvalidColor
	}
	write.Color = color

	return s.calendars.Update(ctx, userID, id, write.fields())
}

func (s *CalendarService) Delete(ctx context.Context, userID int64, id string) error {
	return s.calendars.Delete(ctx, userID, id)
}

// RecordRefreshSuccess records a successful Refresh's outcome on id's
// Calendar (#85, #86, ADR-0033): the response validators (or content hash)
// to send back on the next conditional GET, when it completed, when the
// poller should attempt it next, and the publisher's stated cadence if
// observed. Always resets the failure/backoff state.
func (s *CalendarService) RecordRefreshSuccess(ctx context.Context, userID int64, id string, success repository.RefreshSuccess) error {
	return s.calendars.RecordRefreshSuccess(ctx, userID, id, success)
}

// RecordRefreshFailure records a failed Refresh attempt on id's Calendar
// (#86, ADR-0033): the classified reason, the new consecutive-failure count,
// and when to retry. Never disables or deletes the Calendar.
func (s *CalendarService) RecordRefreshFailure(ctx context.Context, userID int64, id string, failure repository.RefreshFailure) error {
	return s.calendars.RecordRefreshFailure(ctx, userID, id, failure)
}

// ScheduleNextRefresh sets a brand new Subscription's first due time (#86,
// ADR-0033), before any Refresh has run against it.
func (s *CalendarService) ScheduleNextRefresh(ctx context.Context, userID int64, id string, nextRefreshAt time.Time) error {
	return s.calendars.ScheduleNextRefresh(ctx, userID, id, nextRefreshAt)
}

// UpdateKeepAlarms changes id's keep_alarms setting alone (#87, ADR-0032).
// SubscribeService.UpdateKeepAlarms is the caller that actually enforces
// the Subscribed-Calendar-only rule and cascades the reminder cleanup a
// turn-off requires; this method is the plain column write underneath it.
func (s *CalendarService) UpdateKeepAlarms(ctx context.Context, userID int64, id string, keepAlarms bool) (repository.Calendar, error) {
	return s.calendars.UpdateKeepAlarms(ctx, userID, id, keepAlarms)
}

// UpdateSourceURL changes id's source_url alone, resetting the
// conditional-GET validators earned from the old URL (#88, ADR-0032).
// SubscribeService.UpdateSourceURL is the caller that enforces the
// Subscribed-Calendar-only rule and normalizes/validates the URL; this
// method is the plain column write underneath it.
func (s *CalendarService) UpdateSourceURL(ctx context.Context, userID int64, id, url string) (repository.Calendar, error) {
	return s.calendars.UpdateSourceURL(ctx, userID, id, url)
}

// ListDueForRefresh returns every Subscribed Calendar, across every user,
// whose next_refresh_at has come due — the background poller's read path
// (#86, ADR-0033).
func (s *CalendarService) ListDueForRefresh(ctx context.Context, now time.Time) ([]repository.Calendar, error) {
	return s.calendars.ListDueForRefresh(ctx, now)
}

type defaultCalendar struct {
	name  string
	color string
}

// These seed hexes are independent of the frontend's Swatch hexes
// (calendarColors.ts) — the two lists are allowed to drift (ADR-0029).
var defaultCalendars = []defaultCalendar{
	{name: "Personal", color: "#12809CFF"}, // peacock
	{name: "Work", color: "#E2483DFF"},     // tomato
	{name: "Family", color: "#6B9071FF"},   // sage
}

// EnsureDefaults seeds a user with the default Personal/Work/Family
// calendars if they don't have any calendars yet. It's a no-op once the user
// has at least one calendar, so it's safe to call on every startup rather
// than only on a freshly created user — though callers that only want to
// seed a genuinely fresh install should still gate the call on that signal
// (see AuthService.Bootstrap's created return value), since a user who
// deletes all their calendars would otherwise have them silently resurrected.
func (s *CalendarService) EnsureDefaults(ctx context.Context, userID int64) error {
	existing, err := s.List(ctx, userID)
	if err != nil {
		return fmt.Errorf("list calendars: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	for _, d := range defaultCalendars {
		if _, err := s.Create(ctx, userID, uuid.NewString(), CalendarWrite{Name: d.name, Color: d.color}); err != nil {
			return fmt.Errorf("create default calendar %q: %w", d.name, err)
		}
	}

	return nil
}
