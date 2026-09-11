package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TaskList is a Task List (ADR-0083): a named, private container of Tasks
// belonging to one User within one Workspace. Deliberately not a Calendar —
// no Access, Share, Role, Source or Exposure.
type TaskList struct {
	ID          int64
	UserID      int64
	WorkspaceID int64
	Name        string
	Color       string
	IsDefault   bool
	CreatedAt   time.Time
}

type TaskListRepository struct {
	db DBTX
}

func NewTaskListRepository(db *sql.DB) *TaskListRepository {
	return &TaskListRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx.
func (r *TaskListRepository) WithTx(tx *sql.Tx) *TaskListRepository {
	return &TaskListRepository{db: tx}
}

const taskListColumns = `id, user_id, workspace_id, name, color, is_default, created_at`

// Create inserts a new Task List named name, in color, owned by userID
// inside workspaceID. isDefault is true only for the Inbox a (User,
// Workspace) pair is provisioned with (ADR-0083) — every other caller passes
// false, since promoting a Task List to default is SetDefault's job, not
// Create's.
func (r *TaskListRepository) Create(ctx context.Context, userID, workspaceID int64, name, color string, isDefault bool) (TaskList, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO task_lists (user_id, workspace_id, name, color, is_default) VALUES (?, ?, ?, ?, ?)`,
		userID, workspaceID, name, color, isDefault,
	)
	if err != nil {
		return TaskList{}, fmt.Errorf("insert task list: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return TaskList{}, fmt.Errorf("get inserted task list id: %w", err)
	}

	return r.GetByID(ctx, id, userID, workspaceID)
}

// GetByID returns the Task List identified by id, scoped to userID and
// workspaceID together — a Task List belonging to another User, or to a
// different Workspace, is ErrNotFound rather than any error naming the
// mismatch, so the caller can never distinguish "not mine" from "doesn't
// exist".
func (r *TaskListRepository) GetByID(ctx context.Context, id, userID, workspaceID int64) (TaskList, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+taskListColumns+` FROM task_lists WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	list, err := scanTaskListRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskList{}, ErrNotFound
		}
		return TaskList{}, fmt.Errorf("scan task list: %w", err)
	}
	return list, nil
}

// ListForUser returns every Task List userID owns inside workspaceID,
// ordered by when it was created.
func (r *TaskListRepository) ListForUser(ctx context.Context, userID, workspaceID int64) ([]TaskList, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+taskListColumns+` FROM task_lists WHERE user_id = ? AND workspace_id = ? ORDER BY created_at, id`,
		userID, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list task lists: %w", err)
	}
	return collectRows(rows, scanTaskListRow)
}

func scanTaskListRow(row rowScanner) (TaskList, error) {
	var l TaskList
	err := row.Scan(&l.ID, &l.UserID, &l.WorkspaceID, &l.Name, &l.Color, &l.IsDefault, &l.CreatedAt)
	return l, err
}

// Rename updates id's name, scoped to userID and workspaceID like GetByID.
func (r *TaskListRepository) Rename(ctx context.Context, id, userID, workspaceID int64, name string) (TaskList, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE task_lists SET name = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		name, id, userID, workspaceID,
	)
	if err != nil {
		return TaskList{}, fmt.Errorf("rename task list: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return TaskList{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// Recolor updates id's color, scoped to userID and workspaceID like GetByID.
func (r *TaskListRepository) Recolor(ctx context.Context, id, userID, workspaceID int64, color string) (TaskList, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE task_lists SET color = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		color, id, userID, workspaceID,
	)
	if err != nil {
		return TaskList{}, fmt.Errorf("recolor task list: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return TaskList{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// ClearDefault unsets is_default on whichever Task List currently holds it
// for (userID, workspaceID) — SetDefault's first step, run in the same
// transaction as the second so a (User, Workspace) pair never has zero or
// two default Task Lists in between. A no-op (zero rows affected) is not an
// error: SetDefault's own caller might be promoting the very first Task
// List a User has, with no prior default to clear.
func (r *TaskListRepository) ClearDefault(ctx context.Context, userID, workspaceID int64) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE task_lists SET is_default = 0 WHERE user_id = ? AND workspace_id = ? AND is_default = 1`,
		userID, workspaceID,
	); err != nil {
		return fmt.Errorf("clear default task list: %w", err)
	}
	return nil
}

// SetDefaultFlag sets is_default on id, scoped to userID and workspaceID
// like GetByID — SetDefault's second step, after ClearDefault.
func (r *TaskListRepository) SetDefaultFlag(ctx context.Context, id, userID, workspaceID int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE task_lists SET is_default = 1 WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("set default task list: %w", err)
	}
	return requireAffected(res)
}

// Delete removes id outright, scoped to userID and workspaceID like GetByID.
// Refusing to delete a Task List that still holds the default flag is
// TaskListService's job, checked before this is ever called — this method
// itself doesn't inspect is_default.
func (r *TaskListRepository) Delete(ctx context.Context, id, userID, workspaceID int64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM task_lists WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("delete task list: %w", err)
	}
	return requireAffected(res)
}
