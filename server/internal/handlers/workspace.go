package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

type WorkspaceHandler struct {
	workspaces *service.WorkspaceService
}

func NewWorkspaceHandler(workspaces *service.WorkspaceService) *WorkspaceHandler {
	return &WorkspaceHandler{workspaces: workspaces}
}

type workspaceResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func toWorkspaceResponse(w repository.Workspace) workspaceResponse {
	return workspaceResponse{ID: w.ID, Name: w.Name}
}

// List returns every Workspace the caller belongs to (ADR-0044) — the
// workspace switcher's data source.
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	workspaces, err := h.workspaces.ListForUser(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list workspaces")
		return
	}

	responses := make([]workspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		responses[i] = toWorkspaceResponse(ws)
	}

	httpresponse.JSON(w, http.StatusOK, responses)
}

// workspaceInviteResponse is CreateInvite's and ReissueInvite's response —
// the resulting WorkspaceInvite alongside the plaintext token, which is never
// retrievable again once this response is sent (ADR-0044).
type workspaceInviteResponse struct {
	ID              int64     `json:"id"`
	WorkspaceID     int64     `json:"workspace_id"`
	Email           string    `json:"email"`
	InviteExpiresAt time.Time `json:"invite_expires_at"`
	Token           string    `json:"token"`
}

func toWorkspaceInviteResponse(result service.WorkspaceInviteResult) workspaceInviteResponse {
	return workspaceInviteResponse{
		ID:              result.Invite.ID,
		WorkspaceID:     result.Invite.WorkspaceID,
		Email:           result.Invite.Email,
		InviteExpiresAt: result.Invite.InviteExpiresAt,
		Token:           result.Token,
	}
}

var createWorkspaceInviteErrors = []errorCase{
	{service.ErrInvalidEmail, badRequest("email is not a valid address")},
	{repository.ErrWorkspaceInviteExists, conflict("invite_exists", repository.ErrWorkspaceInviteExists.Error())},
	{repository.ErrNotFound, notFound("workspace not found")},
}

var reissueWorkspaceInviteErrors = []errorCase{
	{repository.ErrNotFound, notFound("invite not found")},
}

type createWorkspaceInviteRequest struct {
	Email string `json:"email"`
}

// CreateInvite issues a single-use, 7-day Workspace Invite for an email
// (ADR-0044), callable only by the Workspace's Owner or Admin — the returned
// token is shown to the caller exactly once, to distribute however they
// choose.
func (h *WorkspaceHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	actorID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	workspaceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	var req createWorkspaceInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	result, err := h.workspaces.CreateInvite(r.Context(), actorID, workspaceID, req.Email)
	if respondError(w, err, createWorkspaceInviteErrors, "failed to create invite") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toWorkspaceInviteResponse(result))
}

// ReissueInvite replaces id's outstanding Workspace Invite with a fresh token
// and expiry (ADR-0044), invalidating whichever token came before it —
// callable only by the invite's Workspace's Owner or Admin.
func (h *WorkspaceHandler) ReissueInvite(w http.ResponseWriter, r *http.Request) {
	actorID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	result, err := h.workspaces.ReissueInvite(r.Context(), actorID, id)
	if respondError(w, err, reissueWorkspaceInviteErrors, "failed to reissue invite") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toWorkspaceInviteResponse(result))
}
