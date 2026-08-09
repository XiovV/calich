package handlers

import (
	"net/http"

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
