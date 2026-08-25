package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// WorkspaceService creates Workspaces, resolves which ones a User belongs to,
// and issues/reissues Workspace Invites (ADR-0044, #154). Accepting an invite
// lives on AuthService instead, mirroring the AccountService/AuthService
// split ADR-0042 already uses for the account-level Invite this replaces.
type WorkspaceService struct {
	db           *sql.DB
	workspaces   *repository.WorkspaceRepository
	invites      *repository.WorkspaceInviteRepository
	calendarRepo *repository.CalendarRepository
	shareRepo    *repository.CalendarShareRepository
}

func NewWorkspaceService(db *sql.DB, workspaces *repository.WorkspaceRepository, invites *repository.WorkspaceInviteRepository, calendarRepo *repository.CalendarRepository, shareRepo *repository.CalendarShareRepository) *WorkspaceService {
	return &WorkspaceService{db: db, workspaces: workspaces, invites: invites, calendarRepo: calendarRepo, shareRepo: shareRepo}
}

// CreateForOwner creates a new Workspace named name and, atomically, adds
// ownerUserID as its Owner Member (ADR-0044) — used by Bootstrap, which
// never runs concurrently with itself (it's a single startup-time call in
// main.go). Register's equivalent, racier call instead uses
// createForOwnerTx directly, folded into its own single all-or-nothing
// transaction — see AuthService.Register.
func (s *WorkspaceService) CreateForOwner(ctx context.Context, ownerUserID int64, name string) (repository.Workspace, error) {
	var workspace repository.Workspace

	err := repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		workspace, err = s.createForOwnerTx(ctx, tx, ownerUserID, name)
		return err
	})
	if err != nil {
		return repository.Workspace{}, err
	}

	return workspace, nil
}

// createForOwnerTx is CreateForOwner's body, bound to a transaction the
// caller already holds open rather than opening its own — the seam
// AuthService.Register uses so its first-account count check and the
// resulting User/Workspace/Membership writes commit as one unit (closing
// the TOCTOU window a separate transaction per step would leave between
// counting existing Users and creating a new one).
func (s *WorkspaceService) createForOwnerTx(ctx context.Context, tx *sql.Tx, ownerUserID int64, name string) (repository.Workspace, error) {
	txWorkspaces := s.workspaces.WithTx(tx)

	workspace, err := txWorkspaces.Create(ctx, name, ownerUserID)
	if err != nil {
		return repository.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}

	if err := txWorkspaces.AddMember(ctx, workspace.ID, ownerUserID, repository.WorkspaceRoleOwner); err != nil {
		return repository.Workspace{}, fmt.Errorf("add owner membership: %w", err)
	}

	return workspace, nil
}

// WithTx runs fn inside a single transaction opened on the same database
// this WorkspaceService writes to — the seam AuthService.Register uses to
// fold its first-account count check and the resulting User/Workspace/
// Membership writes into one all-or-nothing unit, without needing its own
// database handle.
func (s *WorkspaceService) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return repository.WithTx(ctx, s.db, fn)
}

// GetByID returns the Workspace identified by id. A non-existent id surfaces
// as the unwrapped repository.ErrNotFound, matching ListForUser's other
// callers that check for it directly (e.g. PreviewWorkspaceInvite).
func (s *WorkspaceService) GetByID(ctx context.Context, id int64) (repository.Workspace, error) {
	workspace, err := s.workspaces.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Workspace{}, err
		}
		return repository.Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return workspace, nil
}

// AddMemberInTx adds userID to workspaceID with role, bound to a transaction
// the caller already holds open — the seam AuthService's Workspace-invite
// accept paths use to fold creating (or resolving) the User, adding their
// Membership, and consuming the invite into one all-or-nothing unit.
func (s *WorkspaceService) AddMemberInTx(ctx context.Context, tx *sql.Tx, workspaceID, userID int64, role string) error {
	return s.workspaces.WithTx(tx).AddMember(ctx, workspaceID, userID, role)
}

// IsMember reports whether userID belongs to workspaceID — the check
// httpauth.RequireWorkspace runs before trusting a caller's claimed active
// Workspace (#155, ADR-0045).
func (s *WorkspaceService) IsMember(ctx context.Context, workspaceID, userID int64) (bool, error) {
	if _, err := s.workspaces.GetMember(ctx, workspaceID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("get workspace member: %w", err)
	}
	return true, nil
}

// ListForUser returns every Workspace userID belongs to (ADR-0044) — the
// workspace switcher's data source.
func (s *WorkspaceService) ListForUser(ctx context.Context, userID int64) ([]repository.Workspace, error) {
	workspaces, err := s.workspaces.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspaces, nil
}

// NonEmptyOwnedWorkspaces returns every Workspace userID is the sole Owner of
// that still has other Members in it — the guard blocking self-Disable and
// self-Delete (ADR-0044, AccountService): a Workspace is never left without
// anyone able to manage it. A Workspace where userID is Owner but the only
// Member (a solo Workspace) is never included — there's nobody else
// depending on them there. An empty result means the guard doesn't block.
func (s *WorkspaceService) NonEmptyOwnedWorkspaces(ctx context.Context, userID int64) ([]repository.Workspace, error) {
	workspaces, err := s.workspaces.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}

	var blocking []repository.Workspace
	for _, w := range workspaces {
		if w.OwnerUserID != userID {
			continue
		}
		count, err := s.workspaces.CountMembers(ctx, w.ID)
		if err != nil {
			return nil, fmt.Errorf("count members of workspace %d: %w", w.ID, err)
		}
		if count > 1 {
			blocking = append(blocking, w)
		}
	}

	return blocking, nil
}

var (
	// ErrInvalidWorkspaceRole is returned by SetMemberRole when role isn't
	// "admin" or "member" — the Owner grant/revoke-Admin operation never
	// assigns "owner" itself (ownership transfer is a separate operation,
	// out of scope here).
	ErrInvalidWorkspaceRole = errors.New(`role must be "admin" or "member"`)

	// ErrCannotChangeOwnerRole is returned by SetMemberRole when the target
	// currently holds Owner — Ownership only moves via a dedicated transfer
	// operation, never by role assignment.
	ErrCannotChangeOwnerRole = errors.New("cannot change the workspace owner's role")

	// ErrCannotRemoveOwner is returned by RemoveMember when the target holds
	// Owner — a Workspace always has exactly one Owner, who leaves only via
	// ownership transfer, never plain removal.
	ErrCannotRemoveOwner = errors.New("cannot remove the workspace owner")

	// ErrAdminCannotRemoveAdmin is returned by RemoveMember when an Admin
	// actor targets another Admin — "only the Owner can remove/demote an
	// Admin" (ADR-0044), so membership management can't be used to seize or
	// lock out control of the Workspace.
	ErrAdminCannotRemoveAdmin = errors.New("only the workspace owner can remove an admin")

	// ErrInvalidWorkspaceName is returned by UpdateSettings when name is
	// empty.
	ErrInvalidWorkspaceName = errors.New("workspace name must not be empty")

	// ErrInvalidSharePrivacy is returned by UpdateSettings when
	// defaultSharePrivacy isn't a recognized repository.DefaultSharePrivacy*
	// value.
	ErrInvalidSharePrivacy = errors.New(`default_share_privacy must be "private" or "workspace"`)
)

// ListMembers returns every WorkspaceMember of workspaceID (#156), callable
// by any Member — the member-management list's data source. actorUserID who
// isn't a Member gets the same repository.ErrNotFound a non-existent
// workspaceID would.
func (s *WorkspaceService) ListMembers(ctx context.Context, actorUserID, workspaceID int64) ([]repository.WorkspaceMember, error) {
	if _, err := s.workspaces.GetMember(ctx, workspaceID, actorUserID); err != nil {
		return nil, err
	}

	members, err := s.workspaces.ListMembers(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}
	return members, nil
}

// ListMembersWithUser returns every enabled Member of workspaceID joined
// against their Name (#156, #165), callable by any Member — the
// member-management list's data source. actorUserID who isn't a Member gets
// the same repository.ErrNotFound a non-existent workspaceID would.
func (s *WorkspaceService) ListMembersWithUser(ctx context.Context, actorUserID, workspaceID int64) ([]repository.WorkspaceMemberWithUser, error) {
	if _, err := s.workspaces.GetMember(ctx, workspaceID, actorUserID); err != nil {
		return nil, err
	}

	members, err := s.workspaces.ListMembersWithUser(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}
	return members, nil
}

// SetMemberRole grants or revokes the Admin Role on targetUserID within
// workspaceID (#156, ADR-0044) — Owner-only; an Admin actor gets the same
// repository.ErrNotFound a non-Member would, mirroring
// requireOwnerOrAdmin's not-found-not-forbidden convention. The target
// can't already be the Owner — Ownership moves only via a dedicated
// transfer, never through this call.
func (s *WorkspaceService) SetMemberRole(ctx context.Context, actorUserID, workspaceID, targetUserID int64, role string) (repository.WorkspaceMember, error) {
	if role != repository.WorkspaceRoleAdmin && role != repository.WorkspaceRoleMember {
		return repository.WorkspaceMember{}, ErrInvalidWorkspaceRole
	}

	actor, err := s.workspaces.GetMember(ctx, workspaceID, actorUserID)
	if err != nil {
		return repository.WorkspaceMember{}, err
	}
	if actor.Role != repository.WorkspaceRoleOwner {
		return repository.WorkspaceMember{}, repository.ErrNotFound
	}

	target, err := s.workspaces.GetMember(ctx, workspaceID, targetUserID)
	if err != nil {
		return repository.WorkspaceMember{}, err
	}
	if target.Role == repository.WorkspaceRoleOwner {
		return repository.WorkspaceMember{}, ErrCannotChangeOwnerRole
	}

	member, err := s.workspaces.SetMemberRole(ctx, workspaceID, targetUserID, role)
	if err != nil {
		return repository.WorkspaceMember{}, fmt.Errorf("set workspace member role: %w", err)
	}
	return member, nil
}

// checkRemovalAuthority refuses actorUserID's attempt to remove targetUserID
// from workspaceID the same way RemoveMember does (#156, ADR-0044): actor
// must hold Owner or Admin, the Owner can never be removed, and an Admin
// actor is refused against another Admin target. Shared by RemoveMember and
// RemoveMemberImpact so a preview can never show impact for a removal that
// RemoveMember itself would refuse.
func (s *WorkspaceService) checkRemovalAuthority(ctx context.Context, actorUserID, workspaceID, targetUserID int64) error {
	actor, err := s.workspaces.GetMember(ctx, workspaceID, actorUserID)
	if err != nil {
		return err
	}
	if actor.Role != repository.WorkspaceRoleOwner && actor.Role != repository.WorkspaceRoleAdmin {
		return repository.ErrNotFound
	}

	target, err := s.workspaces.GetMember(ctx, workspaceID, targetUserID)
	if err != nil {
		return err
	}
	if target.Role == repository.WorkspaceRoleOwner {
		return ErrCannotRemoveOwner
	}
	if actor.Role == repository.WorkspaceRoleAdmin && target.Role == repository.WorkspaceRoleAdmin {
		return ErrAdminCannotRemoveAdmin
	}
	return nil
}

// RemoveMemberImpact reports which Calendars targetUserID owns within
// workspaceID, how many Users would lose Access to each under
// DispositionDelete, and who each could be transferred to instead (#160) —
// the preview a removal-confirmation UI shows before RemoveMember is called,
// mirroring AccountService.DeleteImpact's shape. Refused the same way
// RemoveMember itself would refuse the underlying removal.
func (s *WorkspaceService) RemoveMemberImpact(ctx context.Context, actorUserID, workspaceID, targetUserID int64) (RemoveMemberImpact, error) {
	if err := s.checkRemovalAuthority(ctx, actorUserID, workspaceID, targetUserID); err != nil {
		return RemoveMemberImpact{}, err
	}

	calendars, err := s.calendarRepo.ListByUserAndWorkspace(ctx, targetUserID, workspaceID)
	if err != nil {
		return RemoveMemberImpact{}, fmt.Errorf("list owned calendars: %w", err)
	}

	if len(calendars) == 0 {
		return RemoveMemberImpact{Calendars: []CalendarImpact{}}, nil
	}

	workspace, err := s.workspaces.GetByID(ctx, workspaceID)
	if err != nil {
		return RemoveMemberImpact{}, fmt.Errorf("get workspace %d: %w", workspaceID, err)
	}

	candidates, err := s.transferCandidates(ctx, workspaceID, targetUserID)
	if err != nil {
		return RemoveMemberImpact{}, err
	}

	impact := RemoveMemberImpact{Calendars: make([]CalendarImpact, 0, len(calendars))}
	for _, c := range calendars {
		ci, err := calendarImpact(ctx, s.shareRepo, c, workspace.Name, candidates)
		if err != nil {
			return RemoveMemberImpact{}, err
		}
		impact.Calendars = append(impact.Calendars, ci)
	}

	return impact, nil
}

// transferCandidates lists every other enabled Member of workspaceID —
// RemoveMemberImpact's valid transfer targets, since a Calendar's Owner must
// belong to its own Workspace (ADR-0044, ADR-0045).
func (s *WorkspaceService) transferCandidates(ctx context.Context, workspaceID, excludeUserID int64) ([]TransferCandidate, error) {
	members, err := s.workspaces.ListMembersWithUser(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}

	candidates := make([]TransferCandidate, 0, len(members))
	for _, m := range members {
		if m.UserID == excludeUserID {
			continue
		}
		candidates = append(candidates, TransferCandidate{ID: m.UserID, Name: m.Name})
	}
	return candidates, nil
}

// RemoveMemberImpact is what the method of the same name reports before
// targetUserID is removed from a Workspace (#160): every Calendar they own
// within it.
type RemoveMemberImpact struct {
	Calendars []CalendarImpact
}

// RemoveMember ends targetUserID's Membership in workspaceID (#156, #160,
// ADR-0044), callable by the Owner or an Admin. The Owner can never be
// removed (only a dedicated ownership transfer moves that Role away), and
// an Admin actor is refused against another Admin target — only the Owner
// can remove/demote an Admin.
//
// Removal requires an explicit transfer-or-delete disposition for every
// Calendar targetUserID owns *within workspaceID* — there is no default,
// since a missing or ambiguous disposition would otherwise take a shared
// Calendar out from under everyone with a Share the moment its owner leaves.
// Calendars targetUserID owns in other Workspaces are untouched. Dispositions
// and the Membership removal itself commit atomically: either all of it
// happens, or none of it does.
func (s *WorkspaceService) RemoveMember(ctx context.Context, actorUserID, workspaceID, targetUserID int64, dispositions []CalendarDisposition) error {
	if err := s.checkRemovalAuthority(ctx, actorUserID, workspaceID, targetUserID); err != nil {
		return err
	}

	calendars, err := s.calendarRepo.ListByUserAndWorkspace(ctx, targetUserID, workspaceID)
	if err != nil {
		return fmt.Errorf("list owned calendars: %w", err)
	}

	if err := validateDispositions(ctx, calendars, dispositions, targetUserID, s.IsMember); err != nil {
		return err
	}

	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := applyDispositions(ctx, tx, s.calendarRepo, targetUserID, dispositions); err != nil {
			return err
		}

		if err := s.workspaces.WithTx(tx).RemoveMember(ctx, workspaceID, targetUserID); err != nil {
			return fmt.Errorf("remove workspace member: %w", err)
		}
		return nil
	})
}

// UpdateSettings renames workspaceID and sets its default-share-privacy
// setting (#156, ADR-0044) — callable by its Owner or Admin. The stored
// value isn't consumed by anything yet; a later Calendar/Group ticket
// (ADR-0045) reads it as the default privacy a newly shared Calendar gets.
func (s *WorkspaceService) UpdateSettings(ctx context.Context, actorUserID, workspaceID int64, name, defaultSharePrivacy string) (repository.Workspace, error) {
	if err := s.requireOwnerOrAdmin(ctx, actorUserID, workspaceID); err != nil {
		return repository.Workspace{}, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return repository.Workspace{}, ErrInvalidWorkspaceName
	}

	if defaultSharePrivacy != repository.DefaultSharePrivacyPrivate && defaultSharePrivacy != repository.DefaultSharePrivacyWorkspace {
		return repository.Workspace{}, ErrInvalidSharePrivacy
	}

	workspace, err := s.workspaces.UpdateSettings(ctx, workspaceID, name, defaultSharePrivacy)
	if err != nil {
		return repository.Workspace{}, fmt.Errorf("update workspace settings: %w", err)
	}
	return workspace, nil
}

// requireOwnerOrAdmin refuses actorUserID unless they hold Owner or Admin on
// workspaceID — issuing and reissuing a Workspace Invite are Admin-and-above
// operations (ADR-0044). Delegates to requireWorkspaceOwnerOrAdmin, the same
// check GroupService uses for its own Owner/Admin-gated operations
// (ADR-0045).
func (s *WorkspaceService) requireOwnerOrAdmin(ctx context.Context, actorUserID, workspaceID int64) error {
	return requireWorkspaceOwnerOrAdmin(ctx, s.workspaces, actorUserID, workspaceID)
}

// requireWorkspaceOwnerOrAdmin refuses actorUserID unless they hold Owner or
// Admin on workspaceID. A caller who isn't even a Member gets the same
// repository.ErrNotFound a wrong-role Owner/Admin check would, mirroring
// CalendarService.requireOwner's not-found-not-forbidden convention: nothing
// distinguishes "this workspace doesn't exist" from "you have no authority
// over it". Shared by WorkspaceService and GroupService, the two services
// that gate operations on Workspace Role.
func requireWorkspaceOwnerOrAdmin(ctx context.Context, workspaces *repository.WorkspaceRepository, actorUserID, workspaceID int64) error {
	member, err := workspaces.GetMember(ctx, workspaceID, actorUserID)
	if err != nil {
		return err
	}
	if member.Role != repository.WorkspaceRoleOwner && member.Role != repository.WorkspaceRoleAdmin {
		return repository.ErrNotFound
	}
	return nil
}

// WorkspaceInviteResult is a WorkspaceInvite alongside the plaintext token
// (ADR-0044) — mirrors AccountService.InviteResult: the token exists only at
// issuance, since only its hash is ever stored.
type WorkspaceInviteResult struct {
	Invite repository.WorkspaceInvite
	Token  string
}

// CreateInvite issues a single-use, 7-day Workspace Invite for email,
// callable only by workspaceID's Owner or Admin (ADR-0044). Fails with
// repository.ErrWorkspaceInviteExists if workspaceID already has an
// outstanding invite for that email — the caller should call ReissueInvite
// on it instead of creating a competing one, mirroring how
// AccountService.CreateInvite and ReissueInvite are separate operations for
// the account-level Invite this replaces.
func (s *WorkspaceService) CreateInvite(ctx context.Context, actorUserID, workspaceID int64, email string) (WorkspaceInviteResult, error) {
	if err := s.requireOwnerOrAdmin(ctx, actorUserID, workspaceID); err != nil {
		return WorkspaceInviteResult{}, err
	}

	// validateEmail folds to lowercase (ADR-0058, #196) and rejects the
	// name-addr forms and colon/whitespace #243 hardened Register/UpdateEmail
	// against — an invite's email flows unvalidated into users.email via
	// AuthService.AcceptWorkspaceInviteNewAccount, so this path needs the
	// same rules. The stored, folded value is later matched against a
	// stored, already-folded users.email by plain Go string equality
	// (AuthService.AcceptWorkspaceInviteExisting), and against a
	// registrant's own folded input when the invite creates a brand-new
	// account.
	email, err := validateEmail(email)
	if err != nil {
		return WorkspaceInviteResult{}, err
	}

	token, hash, err := newOpaqueToken()
	if err != nil {
		return WorkspaceInviteResult{}, fmt.Errorf("generate invite token: %w", err)
	}

	invite, err := s.invites.Create(ctx, workspaceID, email, hash, time.Now().Add(inviteTokenTTL))
	if err != nil {
		return WorkspaceInviteResult{}, err
	}

	return WorkspaceInviteResult{Invite: invite, Token: token}, nil
}

// ReissueInvite overwrites inviteID's outstanding Workspace Invite with a
// fresh token and a reset 7-day expiry (ADR-0044), invalidating whichever
// token came before it — callable only by the invite's Workspace's Owner or
// Admin.
func (s *WorkspaceService) ReissueInvite(ctx context.Context, actorUserID, inviteID int64) (WorkspaceInviteResult, error) {
	invite, err := s.invites.GetByID(ctx, inviteID)
	if err != nil {
		return WorkspaceInviteResult{}, err
	}

	if err := s.requireOwnerOrAdmin(ctx, actorUserID, invite.WorkspaceID); err != nil {
		return WorkspaceInviteResult{}, err
	}

	token, hash, err := newOpaqueToken()
	if err != nil {
		return WorkspaceInviteResult{}, fmt.Errorf("generate invite token: %w", err)
	}

	invite, err = s.invites.SetTokenHash(ctx, inviteID, hash, time.Now().Add(inviteTokenTTL))
	if err != nil {
		return WorkspaceInviteResult{}, fmt.Errorf("reissue workspace invite: %w", err)
	}

	return WorkspaceInviteResult{Invite: invite, Token: token}, nil
}

// ListInvites returns every outstanding Workspace Invite for workspaceID
// (#165), callable only by its Owner or Admin — the member-management
// screen's outstanding-invites list, shown alongside active Members.
func (s *WorkspaceService) ListInvites(ctx context.Context, actorUserID, workspaceID int64) ([]repository.WorkspaceInvite, error) {
	if err := s.requireOwnerOrAdmin(ctx, actorUserID, workspaceID); err != nil {
		return nil, err
	}

	invites, err := s.invites.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace invites: %w", err)
	}
	return invites, nil
}

// CancelInvite withdraws inviteID outright (#165), callable only by the
// invite's Workspace's Owner or Admin — the invited address's link stops
// working immediately, mirroring how a reissued invite already invalidates
// whichever token came before it.
func (s *WorkspaceService) CancelInvite(ctx context.Context, actorUserID, inviteID int64) error {
	invite, err := s.invites.GetByID(ctx, inviteID)
	if err != nil {
		return err
	}

	if err := s.requireOwnerOrAdmin(ctx, actorUserID, invite.WorkspaceID); err != nil {
		return err
	}

	if err := s.invites.Delete(ctx, inviteID); err != nil {
		return fmt.Errorf("cancel workspace invite: %w", err)
	}
	return nil
}
