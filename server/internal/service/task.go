package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// completedTaskTail bounds how many completed Tasks ListCompleted returns
// (ADR-0083): a recent tail, never all history.
const completedTaskTail = 50

// ErrInvalidTaskTitle is returned by TaskService.Create and Update when
// title is empty.
var ErrInvalidTaskTitle = errors.New("task title must not be empty")

// ErrInvalidTaskPriority is returned by TaskService.UpdatePriority when
// priority falls outside VTODO's PRIORITY range (ADR-0083, #311): 0-9, where
// 0 is None.
var ErrInvalidTaskPriority = errors.New("task priority must be between 0 and 9")

// TaskService creates, lists, updates, completes/uncompletes and deletes
// Tasks (#310, ADR-0083). Private outright, like TaskListService — every
// method is scoped to the caller's own userID and workspaceID by the
// repository call itself, and a Task belonging to someone else surfaces as
// repository.ErrNotFound.
type TaskService struct {
	tasks     *repository.TaskRepository
	taskLists *repository.TaskListRepository
}

func NewTaskService(tasks *repository.TaskRepository, taskLists *repository.TaskListRepository) *TaskService {
	return &TaskService{tasks: tasks, taskLists: taskLists}
}

// ListIncomplete returns every incomplete Task userID owns inside
// workspaceID, unwindowed (ADR-0083).
func (s *TaskService) ListIncomplete(ctx context.Context, userID, workspaceID int64) ([]repository.Task, error) {
	tasks, err := s.tasks.ListIncomplete(ctx, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list incomplete tasks: %w", err)
	}
	return tasks, nil
}

// ListCompleted returns userID's most recently completed Tasks inside
// workspaceID, bounded to completedTaskTail.
func (s *TaskService) ListCompleted(ctx context.Context, userID, workspaceID int64) ([]repository.Task, error) {
	tasks, err := s.tasks.ListCompleted(ctx, userID, workspaceID, completedTaskTail)
	if err != nil {
		return nil, fmt.Errorf("list completed tasks: %w", err)
	}
	return tasks, nil
}

// Create makes a new, incomplete Task titled title in taskListID, owned by
// userID inside workspaceID. taskListID must already be the caller's own —
// quick-add resolves it client-side (the default Task List, or the single
// checked Task List) before ever calling this, but the ownership check here
// is what stops a caller from filing a Task into someone else's Task List by
// typing a different id.
func (s *TaskService) Create(ctx context.Context, userID, workspaceID, taskListID int64, title string) (repository.Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return repository.Task{}, ErrInvalidTaskTitle
	}

	if _, err := s.taskLists.GetByID(ctx, taskListID, userID, workspaceID); err != nil {
		return repository.Task{}, fmt.Errorf("get task list: %w", err)
	}

	task, err := s.tasks.Create(ctx, userID, workspaceID, taskListID, title)
	if err != nil {
		return repository.Task{}, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

// Update changes id's title, scoped to userID and workspaceID.
func (s *TaskService) Update(ctx context.Context, userID, workspaceID, id int64, title string) (repository.Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return repository.Task{}, ErrInvalidTaskTitle
	}

	task, err := s.tasks.UpdateTitle(ctx, id, userID, workspaceID, title)
	if err != nil {
		return repository.Task{}, fmt.Errorf("update task: %w", err)
	}
	return task, nil
}

// Complete marks id completed, scoped to userID and workspaceID, written
// with no confirmation (ADR-0068).
func (s *TaskService) Complete(ctx context.Context, userID, workspaceID, id int64) (repository.Task, error) {
	task, err := s.tasks.Complete(ctx, id, userID, workspaceID)
	if err != nil {
		return repository.Task{}, fmt.Errorf("complete task: %w", err)
	}
	return task, nil
}

// Uncomplete un-marks id completed, scoped to userID and workspaceID.
func (s *TaskService) Uncomplete(ctx context.Context, userID, workspaceID, id int64) (repository.Task, error) {
	task, err := s.tasks.Uncomplete(ctx, id, userID, workspaceID)
	if err != nil {
		return repository.Task{}, fmt.Errorf("uncomplete task: %w", err)
	}
	return task, nil
}

// UpdateNotes changes id's notes, scoped to userID and workspaceID.
func (s *TaskService) UpdateNotes(ctx context.Context, userID, workspaceID, id int64, notes string) (repository.Task, error) {
	task, err := s.tasks.UpdateNotes(ctx, id, userID, workspaceID, notes)
	if err != nil {
		return repository.Task{}, fmt.Errorf("update task notes: %w", err)
	}
	return task, nil
}

// SetDeadline sets id's Deadline, scoped to userID and workspaceID — the
// detail surface's Deadline field, independent of the Time block
// (ADR-0083): never touches start/duration_minutes.
func (s *TaskService) SetDeadline(ctx context.Context, userID, workspaceID, id int64, due time.Time) (repository.Task, error) {
	task, err := s.tasks.SetDue(ctx, id, userID, workspaceID, due)
	if err != nil {
		return repository.Task{}, fmt.Errorf("set task deadline: %w", err)
	}
	return task, nil
}

// ClearDeadline clears id's Deadline, scoped to userID and workspaceID.
func (s *TaskService) ClearDeadline(ctx context.Context, userID, workspaceID, id int64) (repository.Task, error) {
	task, err := s.tasks.ClearDue(ctx, id, userID, workspaceID)
	if err != nil {
		return repository.Task{}, fmt.Errorf("clear task deadline: %w", err)
	}
	return task, nil
}

// UpdatePriority changes id's raw PRIORITY value, scoped to userID and
// workspaceID. Stores the raw 0-9 value rather than an app-specific enum
// (ADR-0083) — None/Low/Medium/High is a presentation mapping the caller
// applies, not a value this accepts.
func (s *TaskService) UpdatePriority(ctx context.Context, userID, workspaceID, id int64, priority int) (repository.Task, error) {
	if priority < 0 || priority > 9 {
		return repository.Task{}, ErrInvalidTaskPriority
	}

	task, err := s.tasks.UpdatePriority(ctx, id, userID, workspaceID, priority)
	if err != nil {
		return repository.Task{}, fmt.Errorf("update task priority: %w", err)
	}
	return task, nil
}

// Move reparents id onto taskListID, scoped to userID and workspaceID.
// taskListID must already be the caller's own — the same ownership check
// Create runs, so a caller can't move a Task into someone else's Task List
// by naming a different id.
func (s *TaskService) Move(ctx context.Context, userID, workspaceID, id, taskListID int64) (repository.Task, error) {
	if _, err := s.taskLists.GetByID(ctx, taskListID, userID, workspaceID); err != nil {
		return repository.Task{}, fmt.Errorf("get task list: %w", err)
	}

	task, err := s.tasks.Move(ctx, id, userID, workspaceID, taskListID)
	if err != nil {
		return repository.Task{}, fmt.Errorf("move task: %w", err)
	}
	return task, nil
}

// Delete removes id outright, scoped to userID and workspaceID.
func (s *TaskService) Delete(ctx context.Context, userID, workspaceID, id int64) error {
	if err := s.tasks.Delete(ctx, id, userID, workspaceID); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}
