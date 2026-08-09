// group.go implements GroupHandler's List endpoint: every Group of the
// caller's active Workspace (ADR-0045) — currently only what the Calendar
// share dialog's Group picker needs (#159). Create/Rename/Delete/membership
// management stay HTTP-unreached until the Groups management UI ticket
// (#167) wires them up.
package handlers

import (
	"net/http"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

type GroupHandler struct {
	groups *service.GroupService
}

func NewGroupHandler(groups *service.GroupService) *GroupHandler {
	return &GroupHandler{groups: groups}
}

type groupResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func toGroupResponse(g repository.Group) groupResponse {
	return groupResponse{ID: g.ID, Name: g.Name}
}

// listGroupsErrors renders GroupService.ListByWorkspace's not-a-Member
// answer — repository.ErrNotFound from workspaces.GetMember — as a 403,
// mirroring RequireWorkspace's own "not a member of this workspace" wording.
var listGroupsErrors = []errorCase{
	{repository.ErrNotFound, forbidden("not a member of this workspace")},
}

// List serves GET /api/groups: every Group of the caller's active Workspace,
// open to any Member of it (#159, ADR-0045).
func (h *GroupHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	workspaceID, ok := httpauth.WorkspaceIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusForbidden, "forbidden", "not a member of this workspace")
		return
	}

	groups, err := h.groups.ListByWorkspace(r.Context(), userID, workspaceID)
	if respondError(w, err, listGroupsErrors, "failed to list groups") {
		return
	}

	response := make([]groupResponse, len(groups))
	for i, g := range groups {
		response[i] = toGroupResponse(g)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}
