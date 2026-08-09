package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Workspace is the account-management and billing boundary above User
// (ADR-0044): a named container with an Owner and an opaque
// subscription_status.
type Workspace struct {
	ID                  int64
	Name                string
	OwnerUserID         int64
	SubscriptionStatus  string
	DefaultSharePrivacy string
	CreatedAt           time.Time
}

// DefaultSharePrivacy values a Workspace's default_share_privacy setting may
// hold (#156) — stored now, consumed by a later Calendar/Group ticket
// (ADR-0045) as the default privacy a newly shared Calendar gets.
const (
	DefaultSharePrivacyPrivate   = "private"
	DefaultSharePrivacyWorkspace = "workspace"
)

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

const workspaceColumns = `id, name, owner_user_id, subscription_status, default_share_privacy, created_at`

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
		`SELECT w.id, w.name, w.owner_user_id, w.subscription_status, w.default_share_privacy, w.created_at
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

// ErrAlreadyMember is returned by AddMember when userID already belongs to
// workspaceID — the workspace_members primary key is (workspace_id, user_id).
var ErrAlreadyMember = errors.New("user is already a member of this workspace")

// AddMember inserts a WorkspaceMember row binding userID to workspaceID with
// role (ADR-0044).
func (r *WorkspaceRepository) AddMember(ctx context.Context, workspaceID, userID int64, role string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
		workspaceID, userID, role,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrAlreadyMember
		}
		return fmt.Errorf("insert workspace member: %w", err)
	}
	return nil
}

// GetMember returns userID's WorkspaceMember row in workspaceID, or
// ErrNotFound if they don't belong to it — the Role check every Workspace
// invite-issuance operation resolves against (ADR-0044).
func (r *WorkspaceRepository) GetMember(ctx context.Context, workspaceID, userID int64) (WorkspaceMember, error) {
	var m WorkspaceMember
	err := r.db.QueryRowContext(ctx,
		`SELECT workspace_id, user_id, role, created_at FROM workspace_members WHERE workspace_id = ? AND user_id = ?`,
		workspaceID, userID,
	).Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return WorkspaceMember{}, ErrNotFound
		}
		return WorkspaceMember{}, fmt.Errorf("get workspace member: %w", err)
	}
	return m, nil
}

// CountMembers reports how many Members workspaceID currently has, itself
// included — the sole-Owner guard's building block (ADR-0044,
// WorkspaceService.OwnsNonEmptyWorkspace): a Workspace whose Owner is its
// only Member (count 1) never blocks that Owner's self-Disable or
// self-Delete, since nobody else depends on them.
func (r *WorkspaceRepository) CountMembers(ctx context.Context, workspaceID int64) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ?`, workspaceID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count workspace members: %w", err)
	}
	return count, nil
}

// Delete removes workspaceID outright, cascading its workspace_members and
// calendars rows (ADR-0044) — used by AccountService.Delete to retire every
// solo Workspace (Owner with no other Members) a self-deleting User owns,
// since workspaces.owner_user_id carries no ON DELETE behaviour and must not
// be left pointing at a deleted User.
func (r *WorkspaceRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return requireAffected(res)
}

// ListMembers returns every WorkspaceMember of workspaceID, ordered by when
// their Membership was created — the member-management list's data source
// (#156).
func (r *WorkspaceRepository) ListMembers(ctx context.Context, workspaceID int64) ([]WorkspaceMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT workspace_id, user_id, role, created_at FROM workspace_members WHERE workspace_id = ? ORDER BY created_at, user_id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}
	defer rows.Close()

	members := []WorkspaceMember{}
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspace members: %w", err)
	}
	return members, nil
}

// SetMemberRole updates userID's Role within workspaceID (#156) — the Owner
// grant/revoke-Admin operation.
func (r *WorkspaceRepository) SetMemberRole(ctx context.Context, workspaceID, userID int64, role string) (WorkspaceMember, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE workspace_members SET role = ? WHERE workspace_id = ? AND user_id = ?`,
		role, workspaceID, userID,
	)
	if err != nil {
		return WorkspaceMember{}, fmt.Errorf("update workspace member role: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return WorkspaceMember{}, err
	}
	return r.GetMember(ctx, workspaceID, userID)
}

// RemoveMember deletes userID's WorkspaceMember row from workspaceID (#156)
// — ends only that Membership; the User account and any other Workspace
// Memberships are untouched (ADR-0044).
func (r *WorkspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID int64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`,
		workspaceID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete workspace member: %w", err)
	}
	return requireAffected(res)
}

// UpdateSettings renames workspaceID and sets its default_share_privacy
// (#156) — callable only by its Owner or Admin.
func (r *WorkspaceRepository) UpdateSettings(ctx context.Context, workspaceID int64, name, defaultSharePrivacy string) (Workspace, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE workspaces SET name = ?, default_share_privacy = ? WHERE id = ?`,
		name, defaultSharePrivacy, workspaceID,
	)
	if err != nil {
		return Workspace{}, fmt.Errorf("update workspace settings: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Workspace{}, err
	}
	return r.GetByID(ctx, workspaceID)
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
	if err := row.Scan(&w.ID, &w.Name, &w.OwnerUserID, &w.SubscriptionStatus, &w.DefaultSharePrivacy, &w.CreatedAt); err != nil {
		return Workspace{}, err
	}
	return w, nil
}
