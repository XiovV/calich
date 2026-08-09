package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/XiovV/calendar/server/internal/repository"
)

// WorkspaceService creates Workspaces and resolves which ones a User belongs
// to (ADR-0044). Membership/Role management beyond the owning Membership
// Create always establishes is out of scope here — that's #150's broader
// WorkspaceService surface.
type WorkspaceService struct {
	db         *sql.DB
	workspaces *repository.WorkspaceRepository
}

func NewWorkspaceService(db *sql.DB, workspaces *repository.WorkspaceRepository) *WorkspaceService {
	return &WorkspaceService{db: db, workspaces: workspaces}
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

// ListForUser returns every Workspace userID belongs to (ADR-0044) — the
// workspace switcher's data source.
func (s *WorkspaceService) ListForUser(ctx context.Context, userID int64) ([]repository.Workspace, error) {
	workspaces, err := s.workspaces.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspaces, nil
}
