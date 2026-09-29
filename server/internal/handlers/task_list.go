// task_list.go implements TaskListHandler: List/Create/Rename/Recolor/
// SetDefault/Delete back the Tasks panel's Lists filter (#317, ADR-0083). A
// Task List is private outright, so every route resolves by the caller's own
// userID and the active workspaceID together and renders a mismatch as 404 —
// a Task List belonging to someone else is indistinguishable from one that
// does not exist.
package handlers

import (
	"net/http"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type TaskListHandler struct {
	taskLists *service.TaskListService
}

func NewTaskListHandler(taskLists *service.TaskListService) *TaskListHandler {
	return &TaskListHandler{taskLists: taskLists}
}

type taskListResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	IsDefault bool   `json:"isDefault"`
}

func toTaskListResponse(l repository.TaskList) taskListResponse {
	return taskListResponse{ID: l.ID, Name: l.Name, Color: l.Color, IsDefault: l.IsDefault}
}

// taskListErrors renders the sentinels every TaskListService call can
// return: an empty name, an invalid color, an attempt to delete the default
// Task List, and repository.ErrNotFound for a Task List that doesn't exist,
// belongs to another User, or belongs to another Workspace — the three are
// indistinguishable by design.
var taskListErrors = []errorCase{
	{service.ErrInvalidTaskListName, badRequest(service.ErrInvalidTaskListName.Error())},
	{service.ErrInvalidTaskListColor, badRequest(service.ErrInvalidTaskListColor.Error())},
	{service.ErrCannotDeleteDefaultTaskList, badRequest(service.ErrCannotDeleteDefaultTaskList.Error())},
	{repository.ErrNotFound, notFound("task list not found")},
}

// List serves GET /api/task-lists: every Task List the caller owns in their
// active Workspace.
func (h *TaskListHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	lists, err := h.taskLists.ListForUser(r.Context(), userID, workspaceID)
	if respondError(w, err, taskListErrors, "failed to list task lists") {
		return
	}

	response := make([]taskListResponse, len(lists))
	for i, l := range lists {
		response[i] = toTaskListResponse(l)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

type createTaskListRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Create makes a new, non-default Task List in the caller's active
// Workspace. Color is optional — a blank one auto-assigns a Swatch.
func (h *TaskListHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	req, ok := decodeJSON[createTaskListRequest](w, r)
	if !ok {
		return
	}

	list, err := h.taskLists.Create(r.Context(), userID, workspaceID, req.Name, req.Color)
	if respondError(w, err, taskListErrors, "failed to create task list") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toTaskListResponse(list))
}

type renameTaskListRequest struct {
	Name string `json:"name"`
}

// Rename changes a Task List's name.
func (h *TaskListHandler) Rename(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[renameTaskListRequest](w, r)
	if !ok {
		return
	}

	list, err := h.taskLists.Rename(r.Context(), userID, workspaceID, id, req.Name)
	if respondError(w, err, taskListErrors, "failed to rename task list") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskListResponse(list))
}

type recolorTaskListRequest struct {
	Color string `json:"color"`
}

// Recolor changes a Task List's color.
func (h *TaskListHandler) Recolor(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[recolorTaskListRequest](w, r)
	if !ok {
		return
	}

	list, err := h.taskLists.Recolor(r.Context(), userID, workspaceID, id, req.Color)
	if respondError(w, err, taskListErrors, "failed to recolor task list") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskListResponse(list))
}

// SetDefault serves PUT /api/task-lists/{id}/default: promotes a Task List
// to be the caller's default within their active Workspace, atomically
// clearing whichever Task List held the flag before (ADR-0083).
func (h *TaskListHandler) SetDefault(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	list, err := h.taskLists.SetDefault(r.Context(), userID, workspaceID, id)
	if respondError(w, err, taskListErrors, "failed to set default task list") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toTaskListResponse(list))
}

// Delete removes a Task List outright, refusing while it holds the default
// flag (ADR-0083).
func (h *TaskListHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	if err := h.taskLists.Delete(r.Context(), userID, workspaceID, id); respondError(w, err, taskListErrors, "failed to delete task list") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
