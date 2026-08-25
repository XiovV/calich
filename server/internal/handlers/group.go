// group.go implements GroupHandler: List serves the Calendar share dialog's
// Group picker (#159); Create/Rename/Delete/AddMember/RemoveMember/
// ListMembers back the Groups management screen (#167, ADR-0045).
package handlers

import (
	"net/http"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
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
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

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

// groupWriteErrors renders the ErrNotFound every Owner/Admin-gated
// GroupService call returns for a caller who either isn't a Member of the
// Group's Workspace or is one without Owner/Admin authority — mirroring
// listGroupsErrors' rendering, since the two cases are indistinguishable by
// design (requireWorkspaceOwnerOrAdmin's doc comment).
var groupWriteErrors = []errorCase{
	{service.ErrInvalidGroupName, badRequest(service.ErrInvalidGroupName.Error())},
	{repository.ErrNotFound, forbidden("not a member of this workspace")},
}

type createGroupRequest struct {
	Name string `json:"name"`
}

// Create makes a new Group in the caller's active Workspace (#167), callable
// only by its Owner or Admin.
func (h *GroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	req, ok := decodeJSON[createGroupRequest](w, r)
	if !ok {
		return
	}

	group, err := h.groups.Create(r.Context(), userID, workspaceID, req.Name)
	if respondError(w, err, groupWriteErrors, "failed to create group") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toGroupResponse(group))
}

type renameGroupRequest struct {
	Name string `json:"name"`
}

// Rename changes a Group's name (#167), callable only by its Workspace's
// Owner or Admin.
func (h *GroupHandler) Rename(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	groupID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[renameGroupRequest](w, r)
	if !ok {
		return
	}

	group, err := h.groups.Rename(r.Context(), userID, groupID, req.Name)
	if respondError(w, err, groupWriteErrors, "failed to rename group") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toGroupResponse(group))
}

// Delete removes a Group outright (#167), callable only by its Workspace's
// Owner or Admin.
func (h *GroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	groupID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	if err := h.groups.Delete(r.Context(), userID, groupID); respondError(w, err, groupWriteErrors, "failed to delete group") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// groupMemberResponse is one GroupMember row on the Groups management
// screen's membership list (#167).
type groupMemberResponse struct {
	UserID int64 `json:"userId"`
}

// ListMembers returns every GroupMember of a Group (#167), open to any
// Member of its Workspace.
func (h *GroupHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	groupID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	members, err := h.groups.ListMembers(r.Context(), userID, groupID)
	if respondError(w, err, listGroupsErrors, "failed to list group members") {
		return
	}

	response := make([]groupMemberResponse, len(members))
	for i, m := range members {
		response[i] = groupMemberResponse{UserID: m.UserID}
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

// addGroupMemberErrors renders GroupService.AddMember's own sentinels
// alongside groupWriteErrors' Owner/Admin gate.
var addGroupMemberErrors = alsoHandling(groupWriteErrors,
	errorCase{service.ErrGroupMemberNotInWorkspace, badRequest(service.ErrGroupMemberNotInWorkspace.Error())},
	errorCase{repository.ErrAlreadyGroupMember, conflict("already_member", repository.ErrAlreadyGroupMember.Error())},
)

type addGroupMemberRequest struct {
	UserID int64 `json:"userId"`
}

// AddMember adds a Workspace Member to a Group (#167), callable only by the
// Group's Workspace's Owner or Admin.
func (h *GroupHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	groupID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[addGroupMemberRequest](w, r)
	if !ok {
		return
	}

	if err := h.groups.AddMember(r.Context(), userID, groupID, req.UserID); respondError(w, err, addGroupMemberErrors, "failed to add group member") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveMember removes a Member from a Group (#167), callable only by the
// Group's Workspace's Owner or Admin.
func (h *GroupHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	groupID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	targetID, ok := parseInt64Param(w, r, "userId")
	if !ok {
		return
	}

	if err := h.groups.RemoveMember(r.Context(), userID, groupID, targetID); respondError(w, err, groupWriteErrors, "failed to remove group member") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
