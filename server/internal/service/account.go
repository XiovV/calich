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

		ci, err := calendarImpact(ctx, s.shareRepo, c, workspaceNames[c.WorkspaceID], candidatesByWorkspace[c.WorkspaceID])
		if err != nil {
			return DeleteImpact{}, err
		}
		impact.Calendars = append(impact.Calendars, ci)
	}

	return impact, nil
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

	if err := validateDispositions(ctx, calendars, dispositions, userID, s.workspaces.IsMember); err != nil {
		return err
	}

	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := applyDispositions(ctx, tx, s.calendarRepo, userID, dispositions); err != nil {
			return err
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
