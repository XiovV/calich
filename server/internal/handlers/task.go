// task.go implements TaskHandler: List/ListCompleted/Create/Update/Delete/
// Complete/Uncomplete back the Tasks panel's quick-add and completion
// control (#310, ADR-0083). A Task is private outright, so every route
// resolves by the caller's own userID and the active workspaceID together
// and renders a mismatch as 404 — a Task belonging to someone else is
// indistinguishable from one that does not exist.
package handlers

import (
	"net/http"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type TaskHandler struct {
	tasks *service.TaskService
}

func NewTaskHandler(tasks *service.TaskService) *TaskHandler {
	return &TaskHandler{tasks: tasks}
}

type taskResponse struct {
	ID         int64  `json:"id"`
	TaskListID int64  `json:"taskListId"`
	Title      string `json:"title"`
	Completed  bool   `json:"completed"`
}

func toTaskResponse(t repository.Task) taskResponse {
	return taskResponse{
		ID:         t.ID,
		TaskListID: t.TaskListID,
		Title:      t.Title,
		Completed:  t.CompletedAt != nil,
	}
}

// taskErrors renders the sentinels every TaskService call can return: an
// empty title, and repository.ErrNotFound for a Task (or, on Create, a Task
// List) that doesn't exist, belongs to another User, or belongs to another
// Workspace — indistinguishable by design.
var taskErrors = []errorCase{
	{service.ErrInvalidTaskTitle, badRequest(service.ErrInvalidTaskTitle.Error())},
	{repository.ErrNotFound, notFound("task not found")},
}

// List serves GET /api/tasks: every incomplete Task the caller owns in
// their active Workspace, unwindowed by date.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	tasks, err := h.tasks.ListIncomplete(r.Context(), userID, workspaceID)
	if respondError(w, err, taskErrors, "failed to list tasks") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskResponses(tasks))
}

// ListCompleted serves GET /api/tasks/completed: the caller's most recently
// completed Tasks, bounded to a recent tail rather than all history.
func (h *TaskHandler) ListCompleted(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	tasks, err := h.tasks.ListCompleted(r.Context(), userID, workspaceID)
	if respondError(w, err, taskErrors, "failed to list completed tasks") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskResponses(tasks))
}

func toTaskResponses(tasks []repository.Task) []taskResponse {
	response := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		response[i] = toTaskResponse(t)
	}
	return response
}

type createTaskRequest struct {
	Title      string `json:"title"`
	TaskListID int64  `json:"taskListId"`
}

// Create makes a new, incomplete Task in the caller's active Workspace —
// the quick-add field's commit-on-Enter. TaskListID is resolved client-side
// (the default Task List, or the single checked Task List in the Lists
// filter) before this is ever called.
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	req, ok := decodeJSON[createTaskRequest](w, r)
	if !ok {
		return
	}

	task, err := h.tasks.Create(r.Context(), userID, workspaceID, req.TaskListID, req.Title)
	if respondError(w, err, taskErrors, "failed to create task") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toTaskResponse(task))
}

type updateTaskRequest struct {
	Title string `json:"title"`
}

// Update changes a Task's title.
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[updateTaskRequest](w, r)
	if !ok {
		return
	}

	task, err := h.tasks.Update(r.Context(), userID, workspaceID, id, req.Title)
	if respondError(w, err, taskErrors, "failed to update task") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskResponse(task))
}

// Complete serves PUT /api/tasks/{id}/complete: the completion control's
// one-click, no-confirmation commit (ADR-0068).
func (h *TaskHandler) Complete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	task, err := h.tasks.Complete(r.Context(), userID, workspaceID, id)
	if respondError(w, err, taskErrors, "failed to complete task") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskResponse(task))
}

// Uncomplete serves DELETE /api/tasks/{id}/complete: the completion
// control's second click, un-completing a Task.
func (h *TaskHandler) Uncomplete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	task, err := h.tasks.Uncomplete(r.Context(), userID, workspaceID, id)
	if respondError(w, err, taskErrors, "failed to uncomplete task") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskResponse(task))
}

// Delete removes a Task outright.
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	if err := h.tasks.Delete(r.Context(), userID, workspaceID, id); respondError(w, err, taskErrors, "failed to delete task") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
