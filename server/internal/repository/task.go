package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Task is a Task (#310, ADR-0083): a unit of work a User tracks, belonging
// to exactly one Task List and private to its owner. Modelled on VTODO —
// Due and Start/DurationMinutes are two independent date axes, neither
// derived from the other. Completed is a nullable instant, never a status
// enum.
type Task struct {
	ID              int64
	UserID          int64
	WorkspaceID     int64
	TaskListID      int64
	Title           string
	Notes           string
	Due             *time.Time
	Start           *time.Time
	DurationMinutes *int
	Priority        int
	CompletedAt     *time.Time
	CreatedAt       time.Time
}

type TaskRepository struct {
	db DBTX
}

func NewTaskRepository(db *sql.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx.
func (r *TaskRepository) WithTx(tx *sql.Tx) *TaskRepository {
	return &TaskRepository{db: tx}
}

const taskColumns = `id, user_id, workspace_id, task_list_id, title, notes, due, start, duration_minutes, priority, completed_at, created_at`

// Create inserts a new, incomplete Task titled title into taskListID, owned
// by userID inside workspaceID. Every other column (notes, due, start,
// duration, priority) lands empty/null — this ticket's quick-add only ever
// states a title (#310, ADR-0083).
func (r *TaskRepository) Create(ctx context.Context, userID, workspaceID, taskListID int64, title string) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tasks (user_id, workspace_id, task_list_id, title) VALUES (?, ?, ?, ?)`,
		userID, workspaceID, taskListID, title,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Task{}, fmt.Errorf("get inserted task id: %w", err)
	}

	return r.GetByID(ctx, id, userID, workspaceID)
}

// GetByID returns the Task identified by id, scoped to userID and
// workspaceID together — a Task belonging to another User, or to a
// different Workspace, is ErrNotFound rather than any error naming the
// mismatch, so the caller can never distinguish "not mine" from "doesn't
// exist".
func (r *TaskRepository) GetByID(ctx context.Context, id, userID, workspaceID int64) (Task, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	task, err := scanTaskRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("scan task: %w", err)
	}
	return task, nil
}

// ListIncomplete returns every incomplete Task userID owns inside
// workspaceID — deliberately not windowed by date (ADR-0083): a Task with
// no Deadline and no Time block has no window to be in.
func (r *TaskRepository) ListIncomplete(ctx context.Context, userID, workspaceID int64) ([]Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE user_id = ? AND workspace_id = ? AND completed_at IS NULL ORDER BY created_at, id`,
		userID, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list incomplete tasks: %w", err)
	}
	return collectRows(rows, scanTaskRow)
}

// ListCompleted returns userID's most recently completed Tasks inside
// workspaceID, newest first, bounded to limit rows — completed Tasks are
// returned only when asked for, and never as all history (ADR-0083).
func (r *TaskRepository) ListCompleted(ctx context.Context, userID, workspaceID, limit int64) ([]Task, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE user_id = ? AND workspace_id = ? AND completed_at IS NOT NULL ORDER BY completed_at DESC, id DESC LIMIT ?`,
		userID, workspaceID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list completed tasks: %w", err)
	}
	return collectRows(rows, scanTaskRow)
}

func scanTaskRow(row rowScanner) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.UserID, &t.WorkspaceID, &t.TaskListID, &t.Title, &t.Notes, &t.Due, &t.Start, &t.DurationMinutes, &t.Priority, &t.CompletedAt, &t.CreatedAt)
	return t, err
}

// UpdateTitle changes id's title, scoped to userID and workspaceID like
// GetByID.
func (r *TaskRepository) UpdateTitle(ctx context.Context, id, userID, workspaceID int64, title string) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET title = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		title, id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("update task title: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// Complete sets id's completed_at to now, scoped to userID and workspaceID
// like GetByID. Idempotent: completing an already-completed Task just
// refreshes the instant, since the caller (a single click, ADR-0068) never
// needs to distinguish the two.
func (r *TaskRepository) Complete(ctx context.Context, id, userID, workspaceID int64) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET completed_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("complete task: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// Uncomplete clears id's completed_at, scoped to userID and workspaceID
// like GetByID.
func (r *TaskRepository) Uncomplete(ctx context.Context, id, userID, workspaceID int64) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET completed_at = NULL WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("uncomplete task: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// UpdateNotes changes id's notes, scoped to userID and workspaceID like
// GetByID. An empty string clears them.
func (r *TaskRepository) UpdateNotes(ctx context.Context, id, userID, workspaceID int64, notes string) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET notes = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		notes, id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("update task notes: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// SetDue sets id's Deadline, scoped to userID and workspaceID like GetByID —
// the Time block (start/duration_minutes) is untouched, the two axes being
// independent (ADR-0083).
func (r *TaskRepository) SetDue(ctx context.Context, id, userID, workspaceID int64, due time.Time) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET due = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		due, id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("set task due: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// ClearDue clears id's Deadline, scoped to userID and workspaceID like
// GetByID.
func (r *TaskRepository) ClearDue(ctx context.Context, id, userID, workspaceID int64) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET due = NULL WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("clear task due: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// SetTimeBlock sets id's Time block (start + duration_minutes), scoped to
// userID and workspaceID like GetByID — the Deadline (`due`) is untouched,
// the two axes being independent (ADR-0083).
func (r *TaskRepository) SetTimeBlock(ctx context.Context, id, userID, workspaceID int64, start time.Time, durationMinutes int) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET start = ?, duration_minutes = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		start, durationMinutes, id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("set task time block: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// ClearTimeBlock clears id's Time block (start and duration_minutes both to
// NULL), scoped to userID and workspaceID like GetByID. The Deadline is
// untouched.
func (r *TaskRepository) ClearTimeBlock(ctx context.Context, id, userID, workspaceID int64) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET start = NULL, duration_minutes = NULL WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("clear task time block: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// UpdatePriority changes id's raw PRIORITY value (0-9), scoped to userID and
// workspaceID like GetByID. Range validation is the service's job.
func (r *TaskRepository) UpdatePriority(ctx context.Context, id, userID, workspaceID int64, priority int) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET priority = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		priority, id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("update task priority: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// Move reparents id onto taskListID, scoped to userID and workspaceID like
// GetByID. Whether taskListID actually belongs to the caller is the
// service's job (mirroring Create) — this only guards the Task itself.
func (r *TaskRepository) Move(ctx context.Context, id, userID, workspaceID, taskListID int64) (Task, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET task_list_id = ? WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		taskListID, id, userID, workspaceID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("move task: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return Task{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// Delete removes id outright, scoped to userID and workspaceID like GetByID.
func (r *TaskRepository) Delete(ctx context.Context, id, userID, workspaceID int64) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return requireAffected(res)
}

// ReparentToDefault moves every Task in fromListID, scoped to userID and
// workspaceID, into toListID — TaskListService.Delete's first step, run in
// the same transaction as the Task List's own removal, so deleting a
// container never silently deletes work (#310, ADR-0083). Zero rows moved
// is not an error: the Task List being deleted may hold no Tasks at all.
func (r *TaskRepository) ReparentToDefault(ctx context.Context, fromListID, toListID, userID, workspaceID int64) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE tasks SET task_list_id = ? WHERE task_list_id = ? AND user_id = ? AND workspace_id = ?`,
		toListID, fromListID, userID, workspaceID,
	); err != nil {
		return fmt.Errorf("reparent tasks to default task list: %w", err)
	}
	return nil
}
