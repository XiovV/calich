package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/XiovV/calendar/server/internal/repository"
)

// Delete dispositions for a self-deleted User's owned Calendars (ADR-0037,
// ADR-0044) — there is no default, since guessing wrong either silently
// destroys a shared Calendar or hands its data to someone with no business
// holding it.
const (
	DispositionTransfer = "transfer"
	DispositionDelete   = "delete"
)

var (
	// ErrInvalidDisposition is returned when a CalendarDisposition names
	// neither DispositionTransfer nor DispositionDelete — there is no
	// default (ADR-0037).
	ErrInvalidDisposition = errors.New(`disposition must be "transfer" or "delete"`)
	// ErrTransferTargetRequired is returned when a CalendarDisposition is
	// DispositionTransfer with no TransferTo.
	ErrTransferTargetRequired = errors.New("transfer_to is required when disposition is \"transfer\"")
	// ErrCannotTransferToSelf is returned when TransferTo names the User
	// being deleted — their Calendars can't be reassigned to themselves,
	// since they're about to stop existing.
	ErrCannotTransferToSelf = errors.New("cannot transfer a calendar to the account being deleted")
	// ErrTransferTargetNotWorkspaceMember is returned when TransferTo
	// doesn't belong to the Calendar's own Workspace — a Calendar's Owner
	// must belong to its Workspace (ADR-0044, ADR-0045), so a transfer
	// target from anywhere else, or that doesn't exist at all, is refused
	// the same way.
	ErrTransferTargetNotWorkspaceMember = errors.New("transfer target must be a member of the calendar's workspace")
	// ErrCalendarNotOwned is returned when a CalendarDisposition names a
	// Calendar the deleting User doesn't own.
	ErrCalendarNotOwned = errors.New("calendar is not owned by you")
	// ErrDuplicateDisposition is returned when the same Calendar appears
	// more than once across the dispositions Delete was given.
	ErrDuplicateDisposition = errors.New("a calendar cannot appear more than once")
	// ErrMissingDisposition is returned when Delete wasn't given a
	// disposition for every Calendar the User owns — self-Delete requires an
	// explicit transfer-or-delete choice for each one (ADR-0044), so an
	// account can't be deleted with a Calendar silently uncovered.
	ErrMissingDisposition = errors.New("every owned calendar needs a disposition")
	// ErrSoleWorkspaceOwner is returned by SetDisabled (when disabling) and
	// Delete when the User is the sole Owner of a Workspace that still has
	// other Members in it (ADR-0044) — a Workspace must never be left
	// without anyone able to manage it. Resolved by transferring Ownership
	// or removing the other Members first. Callers checking for this case
	// should use errors.Is, since SetDisabled and Delete actually return it
	// wrapped in a *SoleWorkspaceOwnerError naming which Workspace(s) block.
	ErrSoleWorkspaceOwner = errors.New("cannot proceed while you are the sole owner of a workspace with other members")
)

// SoleWorkspaceOwnerError wraps ErrSoleWorkspaceOwner with the names of the
// Workspace(s) blocking the request — a User is the sole Owner of each one
// while it still has other Members in it, so an actionable message can name
// exactly which Workspace(s) need Ownership transferred, or their other
// Members removed, before the request can go through.
type SoleWorkspaceOwnerError struct {
	WorkspaceNames []string
}

func (e *SoleWorkspaceOwnerError) Error() string {
	return fmt.Sprintf(
		"you are the sole owner of %s with other members: %s. Transfer ownership or remove the other members first.",
		pluralizeWorkspace(len(e.WorkspaceNames)), strings.Join(e.WorkspaceNames, ", "),
	)
}

func pluralizeWorkspace(count int) string {
	if count == 1 {
		return "a workspace"
	}
	return fmt.Sprintf("%d workspaces", count)
}

func (e *SoleWorkspaceOwnerError) Unwrap() error {
	return ErrSoleWorkspaceOwner
}

func workspaceNames(workspaces []repository.Workspace) []string {
	names := make([]string, len(workspaces))
	for i, w := range workspaces {
		names[i] = w.Name
	}
	return names
}

// AccountService is self-service account lifecycle (ADR-0044): a User
// disabling or deleting their own account, and nobody else's — the instance-
// wide Admin this replaces (ADR-0037) is retired entirely, along with its
// account-list UI and every guard that existed only to keep it staffed.
type AccountService struct {
	db            *sql.DB
	users         *repository.UserRepository
	sessions      *repository.SessionRepository
	calendarRepo  *repository.CalendarRepository
	shareRepo     *repository.CalendarShareRepository
	workspaceRepo *repository.WorkspaceRepository
	workspaces    *WorkspaceService
}

func NewAccountService(db *sql.DB, users *repository.UserRepository, sessions *repository.SessionRepository, calendarRepo *repository.CalendarRepository, shareRepo *repository.CalendarShareRepository, workspaceRepo *repository.WorkspaceRepository, workspaces *WorkspaceService) *AccountService {
	return &AccountService{db: db, users: users, sessions: sessions, calendarRepo: calendarRepo, shareRepo: shareRepo, workspaceRepo: workspaceRepo, workspaces: workspaces}
}

// SetDisabled disables or re-activates the caller's own account (ADR-0044).
// Disabling is refused while the caller is the sole Owner of a Workspace
// that still has other Members in it (ErrSoleWorkspaceOwner) — re-activating
// never trips that guard, since it never removes anyone's ability to manage
// a Workspace. Disabling also deletes every live Session so the change takes
// effect immediately; everything the User owns — Calendars, Events, Shares —
// is left untouched, since Disable is a property of the account, never of
// the data.
func (s *AccountService) SetDisabled(ctx context.Context, userID int64, isDisabled bool) (repository.User, error) {
	if isDisabled {
		blocking, err := s.workspaces.NonEmptyOwnedWorkspaces(ctx, userID)
		if err != nil {
			return repository.User{}, err
		}
		if len(blocking) > 0 {
			return repository.User{}, &SoleWorkspaceOwnerError{WorkspaceNames: workspaceNames(blocking)}
		}
	}

	user, err := s.users.SetDisabled(ctx, userID, isDisabled)
	if err != nil {
		return repository.User{}, fmt.Errorf("set disabled: %w", err)
	}

	if isDisabled {
		if err := s.sessions.DeleteAllForUser(ctx, userID); err != nil {
			return repository.User{}, fmt.Errorf("invalidate sessions: %w", err)
		}
	}

	return user, nil
}

// TransferCandidate is one other Member of a Calendar's Workspace — a valid
// transfer target for that Calendar, since a Calendar's Owner must belong to
// its own Workspace (ADR-0044, ADR-0045).
type TransferCandidate struct {
	ID   int64
	Name string
}

// CalendarImpact is one Calendar the deleting User owns: which Workspace it
// belongs to, how many Users hold a Share on it and would therefore lose
// Access under DispositionDelete, and who it could be transferred to
// instead.
type CalendarImpact struct {
	ID                 string
	Name               string
	WorkspaceID        int64
	WorkspaceName      string
	ShareCount         int
	TransferCandidates []TransferCandidate
}

// DeleteImpact is what DeleteImpact reports before the caller commits to
// deleting their own account (ADR-0044): every Calendar they own, across
// every Workspace they belong to.
type DeleteImpact struct {
	Calendars []CalendarImpact
}

// DeleteImpact reports what deleting the caller's own account would affect,
// without writing anything — the preview a self-Delete UI shows so a
// transfer-or-delete choice for each owned Calendar can be made with the
// cost, and the available transfer targets, already in view.
func (s *AccountService) DeleteImpact(ctx context.Context, userID int64) (DeleteImpact, error) {
	calendars, err := s.calendarRepo.ListByUser(ctx, userID)
	if err != nil {
		return DeleteImpact{}, fmt.Errorf("list owned calendars: %w", err)
	}

	workspaceNames := map[int64]string{}
	candidatesByWorkspace := map[int64][]TransferCandidate{}

	impact := DeleteImpact{Calendars: make([]CalendarImpact, 0, len(calendars))}
	for _, c := range calendars {
		if _, ok := workspaceNames[c.WorkspaceID]; !ok {
			workspace, err := s.workspaces.GetByID(ctx, c.WorkspaceID)
			if err != nil {
				return DeleteImpact{}, fmt.Errorf("get workspace %d: %w", c.WorkspaceID, err)
			}
			workspaceNames[c.WorkspaceID] = workspace.Name

			members, err := s.workspaces.ListMembers(ctx, userID, c.WorkspaceID)
			if err != nil {
				return DeleteImpact{}, fmt.Errorf("list members of workspace %d: %w", c.WorkspaceID, err)
			}
			candidates := make([]TransferCandidate, 0, len(members))
			for _, m := range members {
				if m.UserID == userID {
					continue
				}
				member, err := s.users.GetByID(ctx, m.UserID)
				if err != nil {
					return DeleteImpact{}, fmt.Errorf("get member %d: %w", m.UserID, err)
				}
				candidates = append(candidates, TransferCandidate{ID: member.ID, Name: member.Name})
			}
			candidatesByWorkspace[c.WorkspaceID] = candidates
		}

		shares, err := s.shareRepo.ListByCalendarWithUser(ctx, c.ID)
		if err != nil {
			return DeleteImpact{}, fmt.Errorf("list shares for calendar %s: %w", c.ID, err)
		}

		impact.Calendars = append(impact.Calendars, CalendarImpact{
			ID:                 c.ID,
			Name:               c.Name,
			WorkspaceID:        c.WorkspaceID,
			WorkspaceName:      workspaceNames[c.WorkspaceID],
			ShareCount:         len(shares),
			TransferCandidates: candidatesByWorkspace[c.WorkspaceID],
		})
	}

	return impact, nil
}

// CalendarDisposition is one owned Calendar's transfer-or-delete choice for
// Delete (ADR-0044) — self-Delete requires one per Calendar the caller owns,
// unlike the retired Admin-driven Delete, which took a single disposition
// for the whole account.
type CalendarDisposition struct {
	CalendarID  string
	Disposition string // DispositionTransfer or DispositionDelete
	// TransferTo is required, and must name a current Member of the
	// Calendar's own Workspace, when Disposition is DispositionTransfer.
	TransferTo *int64
}

// Delete removes the caller's own account outright (ADR-0044), requiring an
// explicit disposition for every Calendar they own, across every Workspace
// they belong to — there is no default, since a missing or ambiguous
// disposition would otherwise take a shared Calendar, including Events other
// people wrote, out from under everyone with a Share, silently, at the exact
// moment nobody is paying attention:
//
//   - DispositionTransfer reassigns the Calendar to TransferTo, keeping its
//     Events (including ones other people wrote) and existing Shares exactly
//     as they were. TransferTo must belong to the Calendar's own Workspace.
//   - DispositionDelete removes the Calendar, and with it its Events, via
//     the existing cascade.
//
// Refused with ErrSoleWorkspaceOwner while the caller is the sole Owner of a
// Workspace that still has other Members in it. Once every disposition is
// applied, every Workspace the caller solely owns (necessarily a solo one —
// the guard above already proved no other Member depends on it) is retired
// too, since workspaces.owner_user_id carries no ON DELETE behaviour.
// Shares granted *to* the caller are removed as a side effect of deleting
// their account (cascade on calendar_shares.user_id).
func (s *AccountService) Delete(ctx context.Context, userID int64, dispositions []CalendarDisposition) error {
	blocking, err := s.workspaces.NonEmptyOwnedWorkspaces(ctx, userID)
	if err != nil {
		return err
	}
	if len(blocking) > 0 {
		return &SoleWorkspaceOwnerError{WorkspaceNames: workspaceNames(blocking)}
	}

	calendars, err := s.calendarRepo.ListByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list owned calendars: %w", err)
	}
	byID := make(map[string]repository.Calendar, len(calendars))
	for _, c := range calendars {
		byID[c.ID] = c
	}

	seen := make(map[string]bool, len(dispositions))
	for _, d := range dispositions {
		if seen[d.CalendarID] {
			return ErrDuplicateDisposition
		}
		seen[d.CalendarID] = true

		calendar, ok := byID[d.CalendarID]
		if !ok {
			return ErrCalendarNotOwned
		}

		switch d.Disposition {
		case DispositionTransfer:
			if d.TransferTo == nil {
				return ErrTransferTargetRequired
			}
			if *d.TransferTo == userID {
				return ErrCannotTransferToSelf
			}
			isMember, err := s.workspaces.IsMember(ctx, calendar.WorkspaceID, *d.TransferTo)
			if err != nil {
				return err
			}
			if !isMember {
				return ErrTransferTargetNotWorkspaceMember
			}
		case DispositionDelete:
		default:
			return ErrInvalidDisposition
		}
	}
	if len(seen) != len(calendars) {
		return ErrMissingDisposition
	}

	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txCalendars := s.calendarRepo.WithTx(tx)
		for _, d := range dispositions {
			switch d.Disposition {
			case DispositionTransfer:
				if err := txCalendars.TransferOwnershipOne(ctx, userID, d.CalendarID, *d.TransferTo); err != nil {
					return fmt.Errorf("transfer calendar %s: %w", d.CalendarID, err)
				}
			case DispositionDelete:
				if err := txCalendars.Delete(ctx, userID, d.CalendarID); err != nil {
					return fmt.Errorf("delete calendar %s: %w", d.CalendarID, err)
				}
			}
		}

		txWorkspaces := s.workspaceRepo.WithTx(tx)
		ownedWorkspaces, err := txWorkspaces.ListForUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("list owned workspaces: %w", err)
		}
		for _, w := range ownedWorkspaces {
			if w.OwnerUserID != userID {
				continue
			}
			if err := txWorkspaces.Delete(ctx, w.ID); err != nil {
				return fmt.Errorf("delete workspace %d: %w", w.ID, err)
			}
		}

		if err := s.users.WithTx(tx).Delete(ctx, userID); err != nil {
			return fmt.Errorf("delete user: %w", err)
		}

		return nil
	})
}
