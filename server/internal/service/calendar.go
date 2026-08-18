package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/repository"
)

var (
	ErrInvalidColor = errors.New("invalid calendar color")
	ErrInvalidName  = errors.New("calendar name must not be empty")
	// ErrInvalidRole is returned when a Share is granted with a Role other
	// than Viewer or Editor (ADR-0034).
	ErrInvalidRole = errors.New("role must be \"viewer\" or \"editor\"")
	// ErrUserNotFound is returned when Share names an email that doesn't
	// exist on the instance — a Share can only be granted to a User who
	// exists (#100's acceptance criteria).
	ErrUserNotFound = errors.New("user not found")
	// ErrCannotShareWithSelf is returned when Share names the Calendar's
	// own Owner — ownership already grants everything a Share could, and
	// an Owner can never hold a Share row of their own (ADR-0034).
	ErrCannotShareWithSelf = errors.New("cannot share a calendar with its owner")
	// ErrNotWorkspaceMember is returned by Create when the caller isn't a
	// Member of the Workspace they're trying to create a Calendar in (#155,
	// ADR-0045) — a Calendar's Workspace is set from the caller's active
	// Workspace, which they must actually belong to.
	ErrNotWorkspaceMember = errors.New("user is not a member of this workspace")
	// ErrShareTargetNotInWorkspace is returned by Share and ShareWithGroup
	// when the target User or Group doesn't belong to the Calendar's own
	// Workspace (#159, ADR-0045) — a Share, direct or Group, can only ever
	// reach someone who's already inside the Calendar's Workspace.
	ErrShareTargetNotInWorkspace = errors.New("share target does not belong to this workspace")
	// ErrGroupNotFound is returned by ShareWithGroup when groupID doesn't
	// exist.
	ErrGroupNotFound = errors.New("group not found")
)

type CalendarService struct {
	calendars      *repository.CalendarRepository
	shares         *repository.CalendarShareRepository
	users          *repository.UserRepository
	eventReminders *repository.EventReminderRepository
	// defaultReminders and explicitReminders are ADR-0064's Default
	// reminders machinery: defaultReminders holds each User's own timed/
	// all-day default lists per Calendar (GetDefaultReminders,
	// SetDefaultReminders), explicitReminders is cleared alongside it when a
	// Share ends, so a User who regains Access later resolves fresh rather
	// than staying frozen at an old explicit-empty opt-out.
	defaultReminders  *repository.CalendarDefaultReminderRepository
	explicitReminders *repository.EventReminderExplicitRepository
	colorOverrides    *repository.CalendarUserColorRepository
	workspaces        *repository.WorkspaceRepository
	groupShares       *repository.CalendarGroupShareRepository
	groups            *repository.GroupRepository
}

func NewCalendarService(calendars *repository.CalendarRepository, shares *repository.CalendarShareRepository, users *repository.UserRepository, eventReminders *repository.EventReminderRepository, defaultReminders *repository.CalendarDefaultReminderRepository, explicitReminders *repository.EventReminderExplicitRepository, colorOverrides *repository.CalendarUserColorRepository, workspaces *repository.WorkspaceRepository, groupShares *repository.CalendarGroupShareRepository, groups *repository.GroupRepository) *CalendarService {
	return &CalendarService{calendars: calendars, shares: shares, users: users, eventReminders: eventReminders, defaultReminders: defaultReminders, explicitReminders: explicitReminders, colorOverrides: colorOverrides, workspaces: workspaces, groupShares: groupShares, groups: groups}
}

// requireMember refuses userID unless they're a Member of workspaceID (#155,
// ADR-0045) — Create's guard against a caller asserting an active Workspace
// they don't actually belong to.
func (s *CalendarService) requireMember(ctx context.Context, userID, workspaceID int64) error {
	if _, err := s.workspaces.GetMember(ctx, workspaceID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotWorkspaceMember
		}
		return fmt.Errorf("get workspace member: %w", err)
	}
	return nil
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

func (s *CalendarService) Create(ctx context.Context, userID, workspaceID int64, id string, write CalendarWrite) (repository.Calendar, error) {
	write.Name = strings.TrimSpace(write.Name)
	if write.Name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	color, ok := NormalizeColor(write.Color)
	if !ok {
		return repository.Calendar{}, ErrInvalidColor
	}
	write.Color = color

	if err := s.requireMember(ctx, userID, workspaceID); err != nil {
		return repository.Calendar{}, err
	}

	calendar, err := s.calendars.Create(ctx, userID, workspaceID, id, write.fields())
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
	// IsOwner is un-clamped ownership (calendar.UserID == caller), unlike
	// Access.IsOwner() which is false for the Owner of a Subscribed
	// Calendar once ResolveAccess clamps their Access to Viewer (#111,
	// ADR-0034). It's the only correct basis for gating Calendar
	// management — rename, recolour, delete, share, refresh, Subscription
	// editing — so that clamp doesn't cost an Owner control of their own
	// Calendar.
	IsOwner bool
	// OwnerName is the Calendar's Owner's display Name — a non-Owner may not
	// call ListShares to derive it themselves (#111).
	OwnerName string
	// ShareCount is how many Shares the Calendar carries — what tells a
	// caller whether more than one person would be notified (#111).
	ShareCount int
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
	// Sorted by Calendar id — not creation time — so a batch of Calendars
	// that all still lack a colour override get auto-assigned one in
	// Calendar-id order (ADR-0057): a stable tiebreak, independent of the
	// query's own ordering.
	sort.Slice(shared, func(i, j int) bool { return shared[i].ID < shared[j].ID })

	result := make([]CalendarWithAccess, 0, len(owned)+len(shared))
	for _, c := range owned {
		color, err := s.resolveDisplayColor(ctx, userID, c)
		if err != nil {
			return nil, err
		}
		isOwner, ownerName, shareCount, err := s.OwnershipMeta(ctx, userID, c)
		if err != nil {
			return nil, err
		}
		result = append(result, CalendarWithAccess{Calendar: c, Access: ResolveAccess(userID, c, nil), Color: color, IsOwner: isOwner, OwnerName: ownerName, ShareCount: shareCount})
	}
	for _, c := range shared {
		role := c.Role
		color, err := s.resolveDisplayColor(ctx, userID, c.Calendar)
		if err != nil {
			return nil, err
		}
		isOwner, ownerName, shareCount, err := s.OwnershipMeta(ctx, userID, c.Calendar)
		if err != nil {
			return nil, err
		}
		result = append(result, CalendarWithAccess{Calendar: c.Calendar, Access: ResolveAccess(userID, c.Calendar, &role), Color: color, IsOwner: isOwner, OwnerName: ownerName, ShareCount: shareCount})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// ListAccessibleInWorkspace returns every Calendar userID has any Access to
// within workspaceID — owned and shared alike, scoped to the active
// Workspace (#155, ADR-0045) — the workspace-scoped sibling of
// ListAccessible. This is what the web app's Calendar List endpoint calls,
// unlike CalDAV's home-set and other cross-workspace callers, which keep
// calling ListAccessible.
func (s *CalendarService) ListAccessibleInWorkspace(ctx context.Context, userID, workspaceID int64) ([]CalendarWithAccess, error) {
	owned, err := s.calendars.ListByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list owned calendars: %w", err)
	}
	shared, err := s.calendars.ListSharedWithUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list shared calendars: %w", err)
	}
	// See ListAccessible's identical sort: Calendar-id order makes a batch
	// auto-assignment deterministic (ADR-0057).
	sort.Slice(shared, func(i, j int) bool { return shared[i].ID < shared[j].ID })

	result := make([]CalendarWithAccess, 0, len(owned)+len(shared))
	for _, c := range owned {
		color, err := s.resolveDisplayColor(ctx, userID, c)
		if err != nil {
			return nil, err
		}
		isOwner, ownerName, shareCount, err := s.OwnershipMeta(ctx, userID, c)
		if err != nil {
			return nil, err
		}
		result = append(result, CalendarWithAccess{Calendar: c, Access: ResolveAccess(userID, c, nil), Color: color, IsOwner: isOwner, OwnerName: ownerName, ShareCount: shareCount})
	}
	for _, c := range shared {
		role := c.Role
		color, err := s.resolveDisplayColor(ctx, userID, c.Calendar)
		if err != nil {
			return nil, err
		}
		isOwner, ownerName, shareCount, err := s.OwnershipMeta(ctx, userID, c.Calendar)
		if err != nil {
			return nil, err
		}
		result = append(result, CalendarWithAccess{Calendar: c.Calendar, Access: ResolveAccess(userID, c.Calendar, &role), Color: color, IsOwner: isOwner, OwnerName: ownerName, ShareCount: shareCount})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// resolveDisplayColor computes DisplayColor(user, calendar) (ADR-0038,
// amended by ADR-0057): the User's own override on calendar if they've set
// one; the Calendar's own stored colour if they own it; otherwise a colour
// auto-assigned and persisted right now, so the same call that first renders
// a shared Calendar to a User also fixes its colour for good. The single
// place both the REST Calendar responses and CalDAV's calendar-color
// property resolve the value they show, so a Subscribed Calendar's
// publisher-tracking (ADR-0032) — which only ever touches the stored
// colour — is transparently overridden by a User's own choice without
// either mechanism knowing about the other.
func (s *CalendarService) resolveDisplayColor(ctx context.Context, userID int64, calendar repository.Calendar) (string, error) {
	override, err := s.colorOverrides.Get(ctx, userID, calendar.ID)
	if err == nil {
		return override, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return "", fmt.Errorf("get calendar color override: %w", err)
	}
	if calendar.UserID == userID {
		return calendar.Color, nil
	}

	used, err := s.usedDisplayColors(ctx, userID, calendar.WorkspaceID)
	if err != nil {
		return "", err
	}
	color := pickFreeColor(used)
	if err := s.colorOverrides.Upsert(ctx, userID, calendar.ID, color); err != nil {
		return "", fmt.Errorf("auto-assign calendar color: %w", err)
	}
	return color, nil
}

// usedDisplayColors returns the resolved DisplayColor (not each Calendar's
// raw stored colour) of every Calendar userID can currently see in
// workspaceID — owned (which already includes a Subscribed or Linked
// Calendar, both owned by the User who added them) and shared-in alike —
// the set pickFreeColor must avoid colliding with (ADR-0057). A shared
// Calendar with no override of its own yet contributes nothing: it hasn't
// been resolved, so there's nothing to compare against, and it gets its own
// turn through resolveDisplayColor — in Calendar-id order across a batch,
// see ListAccessible and ListAccessibleInWorkspace.
func (s *CalendarService) usedDisplayColors(ctx context.Context, userID, workspaceID int64) ([]string, error) {
	owned, err := s.calendars.ListByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list owned calendars: %w", err)
	}
	shared, err := s.calendars.ListSharedWithUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list shared calendars: %w", err)
	}

	used := make([]string, 0, len(owned)+len(shared))
	for _, c := range owned {
		used = append(used, c.Color)
	}
	for _, c := range shared {
		override, err := s.colorOverrides.Get(ctx, userID, c.ID)
		if err == nil {
			used = append(used, override)
			continue
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("get calendar color override: %w", err)
		}
	}
	return used, nil
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
	isOwner, ownerName, shareCount, err := s.OwnershipMeta(ctx, userID, calendar)
	if err != nil {
		return CalendarWithAccess{}, err
	}
	return CalendarWithAccess{Calendar: calendar, Access: access, Color: color, IsOwner: isOwner, OwnerName: ownerName, ShareCount: shareCount}, nil
}

// OwnershipMeta resolves calendar's un-clamped ownership answer for userID
// (calendar.UserID == userID — never Access.IsOwner(), which a Subscription
// clamps to false, #111), the Calendar's Owner's Name, and its Share
// count — direct and Group Shares summed (ADR-0045), since both count
// towards "would more than one person be notified" (#111). Every REST
// Calendar response resolves these here, so a caller never has to re-derive
// ownership from Access, which is deliberately lossy about it (ADR-0034).
func (s *CalendarService) OwnershipMeta(ctx context.Context, userID int64, calendar repository.Calendar) (isOwner bool, ownerName string, shareCount int, err error) {
	owner, err := s.users.GetByID(ctx, calendar.UserID)
	if err != nil {
		return false, "", 0, fmt.Errorf("get calendar owner: %w", err)
	}
	directCount, err := s.shares.CountByCalendar(ctx, calendar.ID)
	if err != nil {
		return false, "", 0, fmt.Errorf("count calendar shares: %w", err)
	}
	groupCount, err := s.groupShares.CountByCalendar(ctx, calendar.ID)
	if err != nil {
		return false, "", 0, fmt.Errorf("count calendar group shares: %w", err)
	}
	return calendar.UserID == userID, owner.Name, directCount + groupCount, nil
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
// the Calendar itself — a CanRead gate, not requireOwner. Unlike Update,
// this never touches calendars.color and so is unaffected by a Subscribed
// Calendar's publisher-tracking (ADR-0032): the override wins for this User
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

// GetDefaultReminders returns userID's own Default reminders on calendarID
// (ADR-0064), split into the timed and all-day lists — empty, not an error,
// if userID has never set either. Same Access bar as SetDefaultReminders.
func (s *CalendarService) GetDefaultReminders(ctx context.Context, userID int64, calendarID string) (timed, allDay []repository.Reminder, err error) {
	if err := s.requireRead(ctx, userID, calendarID); err != nil {
		return nil, nil, err
	}
	return s.defaultReminders.ListByCalendarID(ctx, userID, calendarID)
}

// SetDefaultReminders replaces userID's own Default reminders on
// calendarID's timed or all-day list (whichever allDay names) wholesale
// (ADR-0064) — any User with at least Viewer Access may call this, mirroring
// SetColorOverride: a Default reminder is per-User state beside the Calendar
// rather than a write to it, so the same CanRead gate applies, unaffected by
// a Source's read-only clamp. Never touches the other list or another
// User's rows.
func (s *CalendarService) SetDefaultReminders(ctx context.Context, userID int64, calendarID string, allDay bool, reminders []repository.Reminder) ([]repository.Reminder, error) {
	if err := validateReminders(reminders); err != nil {
		return nil, err
	}
	if err := s.requireRead(ctx, userID, calendarID); err != nil {
		return nil, err
	}
	if err := s.defaultReminders.ReplaceByCalendarID(ctx, userID, calendarID, allDay, reminders); err != nil {
		return nil, fmt.Errorf("set calendar default reminders: %w", err)
	}
	return reminders, nil
}

// Access resolves userID's Access to id's Calendar (ADR-0034, ADR-0045) —
// the single place every Calendar and Event permission check goes through.
// Unlike Get, it fetches id regardless of who owns it, since the point is to
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

		// max(direct share role, any group share role for a group userID
		// currently belongs to) (ADR-0045) — resolved fresh on every call,
		// never a snapshot, so a Group membership change changes Access
		// immediately with no Calendar or Share row touched.
		groupRole, err := s.groupShares.BestRoleForUser(ctx, id, userID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return AccessNone, repository.Calendar{}, fmt.Errorf("get calendar group share: %w", err)
		}
		if err == nil && roleAccess(groupRole) > roleAccess(derefRole(role)) {
			role = &groupRole
		}
	}

	return ResolveAccess(userID, calendar, role), calendar, nil
}

// CalendarMeta is a Calendar's Name and Color, display-only — the shape
// AttendeeCalendarMetaByIDs resolves for a caller with no Access of their
// own to look one up any other way.
type CalendarMeta struct {
	Name  string
	Color string
}

// AttendeeCalendarMetaByIDs resolves every one of ids' Name and Color for a
// caller with no Access to any of them, in one query — safe only because
// every caller of this reaches it through EventService's attachCalendarMeta,
// which only calls it for Calendars backing Events the caller already has
// independent visibility into (an Attendee invite with no Calendar Access
// at all, ADR-0046); this performs no visibility check of its own, unlike
// Access above. A missing id is simply absent from the result.
func (s *CalendarService) AttendeeCalendarMetaByIDs(ctx context.Context, ids []string) (map[string]CalendarMeta, error) {
	calendars, err := s.calendars.ListByIDsAny(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[string]CalendarMeta, len(calendars))
	for _, c := range calendars {
		result[c.ID] = CalendarMeta{Name: c.Name, Color: c.Color}
	}
	return result, nil
}

// OwnerID resolves calendarID's Owner id, access-unchecked like
// AttendeeCalendarMetaByIDs — a Source's own Reminders (a Subscription's
// kept alarms, a Linked Calendar) still scope to the Calendar's Owner
// (ADR-0064), and every caller reaching this has already established its
// own Access to calendarID some other way.
func (s *CalendarService) OwnerID(ctx context.Context, calendarID string) (int64, error) {
	calendar, err := s.calendars.GetByIDAny(ctx, calendarID)
	if err != nil {
		return 0, err
	}
	return calendar.UserID, nil
}

// derefRole returns "" for a nil role, the empty string roleAccess already
// maps to AccessNone — the zero-value comparand Access's direct-vs-group
// max in Access needs without a nil-check at every call site.
func derefRole(role *string) string {
	if role == nil {
		return ""
	}
	return *role
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

// Share grants calendarID a Share to the User named by email, or changes an
// existing Share's role if they already have one (ADR-0034, ADR-0047). Only
// calendarID's Owner may call this. Returns the target's display Name
// alongside the Share, since a caller building a response has only the typed
// Email to go on otherwise.
func (s *CalendarService) Share(ctx context.Context, ownerID int64, calendarID, email, role string) (repository.CalendarShare, string, error) {
	if !isValidRole(role) {
		return repository.CalendarShare{}, "", ErrInvalidRole
	}

	calendar, err := s.requireOwner(ctx, ownerID, calendarID)
	if err != nil {
		return repository.CalendarShare{}, "", err
	}

	target, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.CalendarShare{}, "", ErrUserNotFound
		}
		return repository.CalendarShare{}, "", fmt.Errorf("look up user: %w", err)
	}
	// A Disabled User is hidden from the share picker (ADR-0037) — from the
	// Owner's perspective they don't exist to share with, same as an email
	// that was never registered.
	if target.IsDisabled {
		return repository.CalendarShare{}, "", ErrUserNotFound
	}
	if target.ID == calendar.UserID {
		return repository.CalendarShare{}, "", ErrCannotShareWithSelf
	}

	// A Share, direct or Group, can only ever reach someone already inside
	// the Calendar's own Workspace (#159, ADR-0045).
	if _, err := s.workspaces.GetMember(ctx, calendar.WorkspaceID, target.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.CalendarShare{}, "", ErrShareTargetNotInWorkspace
		}
		return repository.CalendarShare{}, "", fmt.Errorf("get workspace member: %w", err)
	}

	share, err := s.shares.Upsert(ctx, calendarID, target.ID, role)
	if err != nil {
		return repository.CalendarShare{}, "", fmt.Errorf("upsert share: %w", err)
	}
	return share, target.Name, nil
}

// RevokeShare removes targetUserID's Share on calendarID. Only calendarID's
// Owner may call this. targetUserID's own Reminders on calendarID's Events,
// their Default reminders and explicit-opt-out markers on calendarID itself,
// and their colour override, are all cleared with it — a Reminder, default,
// or colour with no Access behind it would otherwise linger, invisible,
// until targetUserID was ever shared with it again (ADR-0064's and
// ADR-0038's acceptance criteria).
func (s *CalendarService) RevokeShare(ctx context.Context, ownerID int64, calendarID string, targetUserID int64) error {
	if _, err := s.requireOwner(ctx, ownerID, calendarID); err != nil {
		return err
	}
	if err := s.shares.Delete(ctx, calendarID, targetUserID); err != nil {
		return err
	}
	return s.clearUserCalendarState(ctx, targetUserID, calendarID)
}

// ListShares returns every Share on calendarID, each carrying the Name and
// Email it was granted to — an Owner's "who has Access to my Calendar, and with
// what Role" listing (ADR-0034). Only calendarID's Owner may call this.
func (s *CalendarService) ListShares(ctx context.Context, ownerID int64, calendarID string) ([]repository.CalendarShareWithUser, error) {
	if _, err := s.requireOwner(ctx, ownerID, calendarID); err != nil {
		return nil, err
	}
	return s.shares.ListByCalendarWithUser(ctx, calendarID)
}

// ShareWithGroup grants calendarID a Share to groupID with role, or changes
// an existing Group Share's role if groupID already has one (ADR-0045).
// Only calendarID's Owner may call this. groupID must belong to calendarID's
// own Workspace.
func (s *CalendarService) ShareWithGroup(ctx context.Context, ownerID int64, calendarID string, groupID int64, role string) (repository.CalendarGroupShareWithGroupName, error) {
	if !isValidRole(role) {
		return repository.CalendarGroupShareWithGroupName{}, ErrInvalidRole
	}

	calendar, err := s.requireOwner(ctx, ownerID, calendarID)
	if err != nil {
		return repository.CalendarGroupShareWithGroupName{}, err
	}

	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.CalendarGroupShareWithGroupName{}, ErrGroupNotFound
		}
		return repository.CalendarGroupShareWithGroupName{}, fmt.Errorf("look up group: %w", err)
	}
	if group.WorkspaceID != calendar.WorkspaceID {
		return repository.CalendarGroupShareWithGroupName{}, ErrShareTargetNotInWorkspace
	}

	share, err := s.groupShares.Upsert(ctx, calendarID, group.ID, role)
	if err != nil {
		return repository.CalendarGroupShareWithGroupName{}, fmt.Errorf("upsert group share: %w", err)
	}
	return repository.CalendarGroupShareWithGroupName{CalendarGroupShare: share, GroupName: group.Name}, nil
}

// RevokeGroupShare removes groupID's Share on calendarID. Only calendarID's
// Owner may call this. Unlike RevokeShare, there are no per-User overrides
// to clear here: a Group Share grants no User-specific row of its own, and
// every Group Member's own overrides live keyed to their user id, untouched
// by a change to the Group's Share.
func (s *CalendarService) RevokeGroupShare(ctx context.Context, ownerID int64, calendarID string, groupID int64) error {
	if _, err := s.requireOwner(ctx, ownerID, calendarID); err != nil {
		return err
	}
	return s.groupShares.Delete(ctx, calendarID, groupID)
}

// ListGroupShares returns every Group Share on calendarID, each carrying the
// Name of the Group it was granted to — the Group-Share sibling of
// ListShares (ADR-0045). Only calendarID's Owner may call this.
func (s *CalendarService) ListGroupShares(ctx context.Context, ownerID int64, calendarID string) ([]repository.CalendarGroupShareWithGroupName, error) {
	if _, err := s.requireOwner(ctx, ownerID, calendarID); err != nil {
		return nil, err
	}
	return s.groupShares.ListByCalendarWithGroupName(ctx, calendarID)
}

// ShareTargets returns every User and Group the share dialog may offer as a
// target for calendarID — every Member and every Group of calendarID's own
// Workspace, excluding the Calendar's own Owner (#159, ADR-0045). Only
// calendarID's Owner may call this, mirroring ListShares.
func (s *CalendarService) ShareTargets(ctx context.Context, ownerID int64, calendarID string) ([]repository.WorkspaceMemberWithUser, []repository.Group, error) {
	calendar, err := s.requireOwner(ctx, ownerID, calendarID)
	if err != nil {
		return nil, nil, err
	}

	members, err := s.workspaces.ListMembersWithUser(ctx, calendar.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("list workspace members: %w", err)
	}
	users := make([]repository.WorkspaceMemberWithUser, 0, len(members))
	for _, m := range members {
		if m.UserID == calendar.UserID {
			continue
		}
		users = append(users, m)
	}

	groups, err := s.groups.ListByWorkspace(ctx, calendar.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("list workspace groups: %w", err)
	}

	return users, groups, nil
}

// LeaveShare removes userID's own Share on calendarID — a User renouncing
// their Access without involving the Owner (ADR-0034). Unlike RevokeShare,
// this needs no ownership check: it only ever removes the caller's own
// Share row, so there's nothing else to authorize. Returns
// repository.ErrNotFound if userID holds no Share on calendarID (including
// when userID is the Owner, who never has one). userID's own Reminders on
// calendarID's Events, their Default reminders and explicit-opt-out markers
// on calendarID itself, and their colour override, are all cleared with it,
// mirroring RevokeShare (ADR-0064, ADR-0038).
func (s *CalendarService) LeaveShare(ctx context.Context, userID int64, calendarID string) error {
	if err := s.shares.Delete(ctx, calendarID, userID); err != nil {
		return err
	}
	return s.clearUserCalendarState(ctx, userID, calendarID)
}

// clearUserCalendarState clears every per-User-per-Calendar row a Share's
// end leaves behind — Reminders on the Calendar's Events, Default reminders
// and explicit-opt-out markers on the Calendar itself, and the colour
// override — RevokeShare's and LeaveShare's shared tail (ADR-0064,
// ADR-0038): a Reminder, default, or colour with no Access behind it would
// otherwise linger, invisible, until userID was ever shared with calendarID
// again.
func (s *CalendarService) clearUserCalendarState(ctx context.Context, userID int64, calendarID string) error {
	if err := s.eventReminders.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		return fmt.Errorf("clear reminders: %w", err)
	}
	if err := s.defaultReminders.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		return fmt.Errorf("clear default reminders: %w", err)
	}
	if err := s.explicitReminders.DeleteByUserAndCalendar(ctx, userID, calendarID); err != nil {
		return fmt.Errorf("clear explicit reminder markers: %w", err)
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
func (s *CalendarService) EnsureDefaults(ctx context.Context, userID, workspaceID int64) error {
	existing, err := s.List(ctx, userID)
	if err != nil {
		return fmt.Errorf("list calendars: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	// Checked once here rather than left to each Create call below — every
	// default calendar shares one workspaceID, so there's nothing more to
	// learn by re-resolving membership three times over.
	if err := s.requireMember(ctx, userID, workspaceID); err != nil {
		return err
	}

	for _, d := range defaultCalendars {
		if _, err := s.calendars.Create(ctx, userID, workspaceID, uuid.NewString(), CalendarWrite{Name: d.name, Color: d.color}.fields()); err != nil {
			return fmt.Errorf("create default calendar %q: %w", d.name, err)
		}
	}

	return nil
}
