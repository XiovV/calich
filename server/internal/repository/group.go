package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Group is a Group (ADR-0045): a named set of a Workspace's Members, usable
// as a Share target in place of a single User. Belongs to exactly one
// Workspace.
type Group struct {
	ID          int64
	WorkspaceID int64
	Name        string
	CreatedAt   time.Time
}

type GroupRepository struct {
	db DBTX
}

func NewGroupRepository(db *sql.DB) *GroupRepository {
	return &GroupRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx.
func (r *GroupRepository) WithTx(tx *sql.Tx) *GroupRepository {
	return &GroupRepository{db: tx}
}

// Create inserts a new Group named name inside workspaceID.
func (r *GroupRepository) Create(ctx context.Context, workspaceID int64, name string) (Group, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO groups (workspace_id, name) VALUES (?, ?)`,
		workspaceID, name,
	)
	if err != nil {
		return Group{}, fmt.Errorf("insert group: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Group{}, fmt.Errorf("get inserted group id: %w", err)
	}

	return r.GetByID(ctx, id)
}

// GetByID returns the Group identified by id, or ErrNotFound if none exists.
func (r *GroupRepository) GetByID(ctx context.Context, id int64) (Group, error) {
	var g Group
	err := r.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, name, created_at FROM groups WHERE id = ?`, id,
	).Scan(&g.ID, &g.WorkspaceID, &g.Name, &g.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Group{}, ErrNotFound
		}
		return Group{}, fmt.Errorf("scan group: %w", err)
	}
	return g, nil
}

// ListByWorkspace returns every Group belonging to workspaceID, ordered by
// when it was created.
func (r *GroupRepository) ListByWorkspace(ctx context.Context, workspaceID int64) ([]Group, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workspace_id, name, created_at FROM groups WHERE workspace_id = ? ORDER BY created_at, id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.Name, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate groups: %w", err)
	}
	return groups, nil
}

// Rename updates id's name.
func (r *GroupRepository) Rename(ctx context.Context, id int64, name string) (Group, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE groups SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return Group{}, fmt.Errorf("rename group: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Group{}, err
	}
	return r.GetByID(ctx, id)
}

// Delete removes id and, via ON DELETE CASCADE, every group_members row that
// referenced it.
func (r *GroupRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return requireAffected(res)
}

// ErrAlreadyGroupMember is returned by AddMember when userID already belongs
// to groupID — the group_members primary key is (group_id, user_id).
var ErrAlreadyGroupMember = errors.New("user is already a member of this group")

// AddMember inserts a group_members row binding userID to groupID.
func (r *GroupRepository) AddMember(ctx context.Context, groupID, userID int64) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES (?, ?)`,
		groupID, userID,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrAlreadyGroupMember
		}
		return fmt.Errorf("insert group member: %w", err)
	}
	return nil
}

// RemoveMember deletes userID's group_members row from groupID.
func (r *GroupRepository) RemoveMember(ctx context.Context, groupID, userID int64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`,
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete group member: %w", err)
	}
	return requireAffected(res)
}

// GroupMember is one User's Membership in one Group, mirroring
// WorkspaceMember's shape.
type GroupMember struct {
	GroupID   int64
	UserID    int64
	CreatedAt time.Time
}

// ListMembers returns every GroupMember of groupID, ordered by when they
// joined.
func (r *GroupRepository) ListMembers(ctx context.Context, groupID int64) ([]GroupMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT group_id, user_id, created_at FROM group_members WHERE group_id = ? ORDER BY created_at, user_id`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	defer rows.Close()

	members := []GroupMember{}
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.GroupID, &m.UserID, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan group member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group members: %w", err)
	}
	return members, nil
}
