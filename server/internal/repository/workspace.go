package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Workspace is the account-management and billing boundary above User
// (ADR-0044): a named container with an Owner and an opaque
// subscription_status.
type Workspace struct {
	ID                 int64
	Name               string
	OwnerUserID        int64
	SubscriptionStatus string
	CreatedAt          time.Time
}

// WorkspaceRole is the Role a WorkspaceMember row grants over a Workspace's
// membership and settings (ADR-0044) — never over Calendar Access.
const (
	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleAdmin  = "admin"
	WorkspaceRoleMember = "member"
)

// WorkspaceMember is one User's Membership in one Workspace, carrying their
// Workspace Role (ADR-0044).
type WorkspaceMember struct {
	WorkspaceID int64
	UserID      int64
	Role        string
	CreatedAt   time.Time
}

const workspaceColumns = `id, name, owner_user_id, subscription_status, created_at`

type WorkspaceRepository struct {
	db DBTX
}

func NewWorkspaceRepository(db *sql.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018) — Create
// needs this to pair a new workspaces row with the owner's
// workspace_members row in one transaction.
func (r *WorkspaceRepository) WithTx(tx *sql.Tx) *WorkspaceRepository {
	return &WorkspaceRepository{db: tx}
}

// Create inserts a new Workspace owned by ownerUserID, defaulting
// subscription_status to 'none' (ADR-0044 leaves the billing/pricing model
// itself out of scope).
func (r *WorkspaceRepository) Create(ctx context.Context, name string, ownerUserID int64) (Workspace, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO workspaces (name, owner_user_id) VALUES (?, ?)`,
		name, ownerUserID,
	)
	if err != nil {
		return Workspace{}, fmt.Errorf("insert workspace: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Workspace{}, fmt.Errorf("get inserted workspace id: %w", err)
	}

	return r.GetByID(ctx, id)
}

func (r *WorkspaceRepository) GetByID(ctx context.Context, id int64) (Workspace, error) {
	return r.scanWorkspace(r.db.QueryRowContext(ctx,
		`SELECT `+workspaceColumns+` FROM workspaces WHERE id = ?`, id,
	))
}

// ListForUser returns every Workspace userID belongs to, ordered by when
// their Membership was created — the workspace switcher's data source
// (ADR-0044).
func (r *WorkspaceRepository) ListForUser(ctx context.Context, userID int64) ([]Workspace, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT w.id, w.name, w.owner_user_id, w.subscription_status, w.created_at
		 FROM workspaces w
		 JOIN workspace_members m ON m.workspace_id = w.id
		 WHERE m.user_id = ?
		 ORDER BY m.created_at, w.id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspaces for user: %w", err)
	}
	defer rows.Close()

	workspaces := []Workspace{}
	for rows.Next() {
		w, err := scanWorkspaceRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspaces: %w", err)
	}
	return workspaces, nil
}

// AddMember inserts a WorkspaceMember row binding userID to workspaceID with
// role (ADR-0044).
func (r *WorkspaceRepository) AddMember(ctx context.Context, workspaceID, userID int64, role string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
		workspaceID, userID, role,
	); err != nil {
		return fmt.Errorf("insert workspace member: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) scanWorkspace(row *sql.Row) (Workspace, error) {
	w, err := scanWorkspaceRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Workspace{}, ErrNotFound
		}
		return Workspace{}, fmt.Errorf("scan workspace: %w", err)
	}
	return w, nil
}

func scanWorkspaceRow(row rowScanner) (Workspace, error) {
	var w Workspace
	if err := row.Scan(&w.ID, &w.Name, &w.OwnerUserID, &w.SubscriptionStatus, &w.CreatedAt); err != nil {
		return Workspace{}, err
	}
	return w, nil
}
