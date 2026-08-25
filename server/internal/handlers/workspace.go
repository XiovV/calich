package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type WorkspaceHandler struct {
	workspaces *service.WorkspaceService
}

func NewWorkspaceHandler(workspaces *service.WorkspaceService) *WorkspaceHandler {
	return &WorkspaceHandler{workspaces: workspaces}
}

type workspaceResponse struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	DefaultSharePrivacy string `json:"defaultSharePrivacy"`
}

func toWorkspaceResponse(w repository.Workspace) workspaceResponse {
	return workspaceResponse{ID: w.ID, Name: w.Name, DefaultSharePrivacy: w.DefaultSharePrivacy}
}

// parseInt64Param reads r's chi URL param name as an int64, writing a 400
// and reporting false when it isn't one.
func parseInt64Param(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	value, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", name+" must be a number")
		return 0, false
	}
	return value, true
}

// List returns every Workspace the caller belongs to (ADR-0044) — the
// workspace switcher's data source.
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

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
	{service.ErrEmailTooLong, badRequest("email must be at most 254 characters")},
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
	actorID := httpauth.MustUserID(r.Context())

	workspaceID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[createWorkspaceInviteRequest](w, r)
	if !ok {
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
	actorID := httpauth.MustUserID(r.Context())

	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	result, err := h.workspaces.ReissueInvite(r.Context(), actorID, id)
	if respondError(w, err, reissueWorkspaceInviteErrors, "failed to reissue invite") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toWorkspaceInviteResponse(result))
}

// outstandingWorkspaceInviteResponse is ListInvites' per-row shape — an
// outstanding Workspace Invite without its token, which only ever appears in
// CreateInvite's/ReissueInvite's own one-time response (ADR-0044).
type outstandingWorkspaceInviteResponse struct {
	ID              int64     `json:"id"`
	WorkspaceID     int64     `json:"workspace_id"`
	Email           string    `json:"email"`
	InviteExpiresAt time.Time `json:"invite_expires_at"`
}

func toOutstandingWorkspaceInviteResponse(i repository.WorkspaceInvite) outstandingWorkspaceInviteResponse {
	return outstandingWorkspaceInviteResponse{
		ID:              i.ID,
		WorkspaceID:     i.WorkspaceID,
		Email:           i.Email,
		InviteExpiresAt: i.InviteExpiresAt,
	}
}

var listWorkspaceInvitesErrors = []errorCase{
	{repository.ErrNotFound, notFound("workspace not found")},
}

// ListInvites returns every outstanding Workspace Invite for id (#165),
// callable only by its Owner or Admin — shown alongside active Members on the
// member-management screen.
func (h *WorkspaceHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	actorID := httpauth.MustUserID(r.Context())
	workspaceID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	invites, err := h.workspaces.ListInvites(r.Context(), actorID, workspaceID)
	if respondError(w, err, listWorkspaceInvitesErrors, "failed to list invites") {
		return
	}

	responses := make([]outstandingWorkspaceInviteResponse, len(invites))
	for i, invite := range invites {
		responses[i] = toOutstandingWorkspaceInviteResponse(invite)
	}

	httpresponse.JSON(w, http.StatusOK, responses)
}

var cancelWorkspaceInviteErrors = []errorCase{
	{repository.ErrNotFound, notFound("invite not found")},
}

// CancelInvite withdraws id outright (#165), callable only by the invite's
// Workspace's Owner or Admin.
func (h *WorkspaceHandler) CancelInvite(w http.ResponseWriter, r *http.Request) {
	actorID := httpauth.MustUserID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	if err := h.workspaces.CancelInvite(r.Context(), actorID, id); respondError(w, err, cancelWorkspaceInviteErrors, "failed to cancel invite") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// workspaceMemberResponse is one Member row on the member-management screen
// (#156, #165): their Name and Email alongside their Workspace Role.
type workspaceMemberResponse struct {
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

var listWorkspaceMembersErrors = []errorCase{
	{repository.ErrNotFound, notFound("workspace not found")},
}

// ListMembers returns every enabled Member of id (#156), callable by any
// Member — the member-management list's data source.
func (h *WorkspaceHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	actorID := httpauth.MustUserID(r.Context())
	workspaceID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	members, err := h.workspaces.ListMembersWithUser(r.Context(), actorID, workspaceID)
	if respondError(w, err, listWorkspaceMembersErrors, "failed to list members") {
		return
	}

	responses := make([]workspaceMemberResponse, len(members))
	for i, m := range members {
		responses[i] = workspaceMemberResponse{UserID: m.UserID, Name: m.Name, Email: m.Email, Role: m.Role, CreatedAt: m.CreatedAt}
	}

	httpresponse.JSON(w, http.StatusOK, responses)
}

var setWorkspaceMemberRoleErrors = []errorCase{
	{service.ErrInvalidWorkspaceRole, badRequest(service.ErrInvalidWorkspaceRole.Error())},
	{service.ErrCannotChangeOwnerRole, badRequest(service.ErrCannotChangeOwnerRole.Error())},
	{repository.ErrNotFound, notFound("member not found")},
}

type setWorkspaceMemberRoleRequest struct {
	Role string `json:"role"`
}

// workspaceMemberRoleResponse is SetMemberRole's response — deliberately
// narrower than workspaceMemberResponse, since WorkspaceService.SetMemberRole
// doesn't join against the User and callers here already have it from their
// own ListMembers call.
type workspaceMemberRoleResponse struct {
	UserID    int64     `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// SetMemberRole grants or revokes the Admin Role on a Member of id (#156),
// callable only by its Owner.
func (h *WorkspaceHandler) SetMemberRole(w http.ResponseWriter, r *http.Request) {
	actorID := httpauth.MustUserID(r.Context())
	workspaceID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	targetID, ok := parseInt64Param(w, r, "userId")
	if !ok {
		return
	}

	req, ok := decodeJSON[setWorkspaceMemberRoleRequest](w, r)
	if !ok {
		return
	}

	member, err := h.workspaces.SetMemberRole(r.Context(), actorID, workspaceID, targetID, req.Role)
	if respondError(w, err, setWorkspaceMemberRoleErrors, "failed to set member role") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, workspaceMemberRoleResponse{UserID: member.UserID, Role: member.Role, CreatedAt: member.CreatedAt})
}

var removeMemberImpactErrors = []errorCase{
	{service.ErrCannotRemoveOwner, badRequest(service.ErrCannotRemoveOwner.Error())},
	{service.ErrAdminCannotRemoveAdmin, forbidden(service.ErrAdminCannotRemoveAdmin.Error())},
	{repository.ErrNotFound, notFound("member not found")},
}

// RemoveMemberImpact reports which Calendars a Member of id owns within it,
// how many Users would lose Access under a delete disposition, and who each
// could be transferred to instead (#160) — the preview a removal-confirmation
// UI shows before RemoveMember is called.
func (h *WorkspaceHandler) RemoveMemberImpact(w http.ResponseWriter, r *http.Request) {
	actorID := httpauth.MustUserID(r.Context())
	workspaceID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	targetID, ok := parseInt64Param(w, r, "userId")
	if !ok {
		return
	}

	impact, err := h.workspaces.RemoveMemberImpact(r.Context(), actorID, workspaceID, targetID)
	if respondError(w, err, removeMemberImpactErrors, "failed to compute remove impact") {
		return
	}

	calendars := make([]calendarImpactResponse, len(impact.Calendars))
	for i, c := range impact.Calendars {
		candidates := make([]transferCandidateResponse, len(c.TransferCandidates))
		for j, candidate := range c.TransferCandidates {
			candidates[j] = transferCandidateResponse{ID: candidate.ID, Name: candidate.Name}
		}
		calendars[i] = calendarImpactResponse{
			ID:                 c.ID,
			Name:               c.Name,
			WorkspaceID:        c.WorkspaceID,
			WorkspaceName:      c.WorkspaceName,
			ShareCount:         c.ShareCount,
			TransferCandidates: candidates,
		}
	}

	httpresponse.JSON(w, http.StatusOK, deleteImpactResponse{Calendars: calendars})
}

var removeMemberErrors = []errorCase{
	{service.ErrCannotRemoveOwner, badRequest(service.ErrCannotRemoveOwner.Error())},
	{service.ErrAdminCannotRemoveAdmin, forbidden(service.ErrAdminCannotRemoveAdmin.Error())},
	{service.ErrInvalidDisposition, badRequest(service.ErrInvalidDisposition.Error())},
	{service.ErrTransferTargetRequired, badRequest(service.ErrTransferTargetRequired.Error())},
	{service.ErrCannotTransferToRemovedMember, badRequest(service.ErrCannotTransferToRemovedMember.Error())},
	{service.ErrTransferTargetNotWorkspaceMember, badRequest(service.ErrTransferTargetNotWorkspaceMember.Error())},
	{service.ErrCalendarNotOwnedByRemovedMember, badRequest(service.ErrCalendarNotOwnedByRemovedMember.Error())},
	{service.ErrDuplicateDisposition, badRequest(service.ErrDuplicateDisposition.Error())},
	{service.ErrMissingCalendarDisposition, badRequest(service.ErrMissingCalendarDisposition.Error())},
	{repository.ErrNotFound, notFound("member not found")},
}

type removeMemberRequest struct {
	Calendars []calendarDispositionRequest `json:"calendars"`
}

// RemoveMember ends a Member's Membership in id (#156, #160), callable by the
// Owner or an Admin, requiring an explicit transfer-or-delete disposition for
// every Calendar the Member owns within it.
func (h *WorkspaceHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	actorID := httpauth.MustUserID(r.Context())
	workspaceID, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	targetID, ok := parseInt64Param(w, r, "userId")
	if !ok {
		return
	}

	req, ok := decodeJSON[removeMemberRequest](w, r)
	if !ok {
		return
	}

	dispositions := make([]service.CalendarDisposition, len(req.Calendars))
	for i, c := range req.Calendars {
		dispositions[i] = service.CalendarDisposition{
			CalendarID:  c.CalendarID,
			Disposition: c.Disposition,
			TransferTo:  c.TransferTo,
		}
	}

	if err := h.workspaces.RemoveMember(r.Context(), actorID, workspaceID, targetID, dispositions); respondError(w, err, removeMemberErrors, "failed to remove member") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
