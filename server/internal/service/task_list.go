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
	// ErrInvalidTaskListName is returned by TaskListService.Create and Rename
	// when name is empty.
	ErrInvalidTaskListName = errors.New("task list name must not be empty")

	// ErrInvalidTaskListColor is returned by Create and Recolor when color is
	// non-empty but not a valid hex color.
	ErrInvalidTaskListColor = errors.New("invalid task list color")

	// ErrCannotDeleteDefaultTaskList is returned by Delete when id still
	// holds the default flag (ADR-0083) — promote another Task List to
	// default first.
	ErrCannotDeleteDefaultTaskList = errors.New("cannot delete the default task list")
)

// TaskListService creates, lists, renames, recolors, promotes-to-default and
// deletes Task Lists (ADR-0083). Private outright — there is no sharing
// mechanism and no Role, so unlike GroupService this performs no
// requireWorkspaceOwnerOrAdmin-style authority check; every method is
// scoped to the caller's own userID and workspaceID by the repository call
// itself, and a Task List belonging to someone else surfaces as
// repository.ErrNotFound rather than any distinguishable "forbidden".
type TaskListService struct {
	db        *sql.DB
	taskLists *repository.TaskListRepository
}

func NewTaskListService(db *sql.DB, taskLists *repository.TaskListRepository) *TaskListService {
	return &TaskListService{db: db, taskLists: taskLists}
}

// ListForUser returns every Task List userID owns inside workspaceID.
func (s *TaskListService) ListForUser(ctx context.Context, userID, workspaceID int64) ([]repository.TaskList, error) {
	lists, err := s.taskLists.ListForUser(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list task lists: %w", err)
	}
	return lists, nil
}

// Create makes a new, non-default Task List named name inside workspaceID,
// owned by userID. color is normalized via NormalizeColor if given; an empty
// color instead auto-assigns the first Swatch absent from the caller's
// existing Task Lists in this Workspace, mirroring Calendar's own
// pickFreeColor (ADR-0057) — a "New list" flow that only asks for a name
// still gets a distinguishable colour.
func (s *TaskListService) Create(ctx context.Context, userID, workspaceID int64, name, color string) (repository.TaskList, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.TaskList{}, ErrInvalidTaskListName
	}

	resolved, err := s.resolveColor(ctx, userID, workspaceID, color)
	if err != nil {
		return repository.TaskList{}, err
	}

	list, err := s.taskLists.Create(ctx, userID, workspaceID, name, resolved, false)
	if err != nil {
		return repository.TaskList{}, fmt.Errorf("create task list: %w", err)
	}
	return list, nil
}

// resolveColor normalizes an explicit color, or — when color is blank —
// picks the first Swatch not already used by one of the caller's own Task
// Lists in workspaceID.
func (s *TaskListService) resolveColor(ctx context.Context, userID, workspaceID int64, color string) (string, error) {
	if strings.TrimSpace(color) == "" {
		existing, err := s.taskLists.ListForUser(ctx, userID, workspaceID)
		if err != nil {
			return "", fmt.Errorf("list task lists: %w", err)
		}
		used := make([]string, len(existing))
		for i, l := range existing {
			used[i] = l.Color
		}
		return pickFreeColor(used), nil
	}

	normalized, ok := NormalizeColor(color)
	if !ok {
		return "", ErrInvalidTaskListColor
	}
	return normalized, nil
}

// Rename changes id's name, scoped to userID and workspaceID.
func (s *TaskListService) Rename(ctx context.Context, userID, workspaceID, id int64, name string) (repository.TaskList, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.TaskList{}, ErrInvalidTaskListName
	}

	list, err := s.taskLists.Rename(ctx, id, userID, workspaceID, name)
	if err != nil {
		return repository.TaskList{}, fmt.Errorf("rename task list: %w", err)
	}
	return list, nil
}

// Recolor changes id's color, scoped to userID and workspaceID.
func (s *TaskListService) Recolor(ctx context.Context, userID, workspaceID, id int64, color string) (repository.TaskList, error) {
	normalized, ok := NormalizeColor(color)
	if !ok {
		return repository.TaskList{}, ErrInvalidTaskListColor
	}

	list, err := s.taskLists.Recolor(ctx, id, userID, workspaceID, normalized)
	if err != nil {
		return repository.TaskList{}, fmt.Errorf("recolor task list: %w", err)
	}
	return list, nil
}

// SetDefault promotes id to be userID's default Task List inside
// workspaceID, atomically clearing whichever Task List held the flag before
// (ADR-0083). GetByID confirms id is the caller's own before the
// transaction opens, so an id belonging to someone else — or nobody —
// surfaces as repository.ErrNotFound rather than silently clearing the
// caller's own current default for nothing.
func (s *TaskListService) SetDefault(ctx context.Context, userID, workspaceID, id int64) (repository.TaskList, error) {
	if _, err := s.taskLists.GetByID(ctx, id, userID, workspaceID); err != nil {
		return repository.TaskList{}, fmt.Errorf("get task list: %w", err)
	}

	err := repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txLists := s.taskLists.WithTx(tx)

		if err := txLists.ClearDefault(ctx, userID, workspaceID); err != nil {
			return err
		}
		if err := txLists.SetDefaultFlag(ctx, id, userID, workspaceID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return repository.TaskList{}, err
	}

	return s.taskLists.GetByID(ctx, id, userID, workspaceID)
}

// Delete removes id outright, scoped to userID and workspaceID, refusing
// while it holds the default flag (ADR-0083) — promote another Task List to
// default first via SetDefault.
func (s *TaskListService) Delete(ctx context.Context, userID, workspaceID, id int64) error {
	list, err := s.taskLists.GetByID(ctx, id, userID, workspaceID)
	if err != nil {
		return fmt.Errorf("get task list: %w", err)
	}
	if list.IsDefault {
		return ErrCannotDeleteDefaultTaskList
	}

	if err := s.taskLists.Delete(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("delete task list: %w", err)
	}
	return nil
}
