// share.go implements calendarHandler's Share endpoints (#100, ADR-0034):
// granting/changing a Share's Role, revoking one, listing who has Access to
// a Calendar, and a User leaving a Share on their own. Every route here
// except LeaveShare is Owner-only — enforced by CalendarService, not here,
// so a non-Owner (with or without some lesser Access) gets the same
// not-found Update and Delete already return.
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

type shareResponse struct {
	UserID    int64     `json:"userId"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

func toShareResponse(name, email string, s repository.CalendarShare) shareResponse {
	return shareResponse{UserID: s.UserID, Name: name, Email: email, Role: s.Role, CreatedAt: s.CreatedAt}
}

func toShareWithUserResponse(s repository.CalendarShareWithUser) shareResponse {
	return toShareResponse(s.Name, s.Email, s.CalendarShare)
}

var shareErrors = []errorCase{
	{service.ErrInvalidRole, badRequest("role must be \"viewer\" or \"editor\"")},
	{service.ErrUserNotFound, badRequest("user not found")},
	{service.ErrCannotShareWithSelf, badRequest("cannot share a calendar with its owner")},
	{service.ErrShareTargetNotInWorkspace, badRequest("share target does not belong to this workspace")},
}

var shareErrorsWithNotFound = alsoHandling(shareErrors, calendarNotFoundErrors...)

var groupShareErrors = []errorCase{
	{service.ErrInvalidRole, badRequest("role must be \"viewer\" or \"editor\"")},
	{service.ErrGroupNotFound, badRequest("group not found")},
	{service.ErrShareTargetNotInWorkspace, badRequest("share target does not belong to this workspace")},
}

var groupShareErrorsWithNotFound = alsoHandling(groupShareErrors, calendarNotFoundErrors...)

// ListShares serves GET /api/calendars/{id}/shares: every Share on the
// Calendar, Owner-only.
func (h *CalendarHandler) ListShares(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	shares, err := h.calendars.ListShares(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to list shares") {
		return
	}

	response := make([]shareResponse, len(shares))
	for i, s := range shares {
		response[i] = toShareWithUserResponse(s)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

type shareRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Share serves POST /api/calendars/{id}/shares: grants a Share to the User
// named by req.Email with req.Role, or changes an existing Share's Role if
// they already have one (ADR-0034, ADR-0047). Owner-only.
func (h *CalendarHandler) Share(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req shareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	share, targetName, err := h.calendars.Share(r.Context(), userID, id, req.Email, req.Role)
	if respondError(w, err, shareErrorsWithNotFound, "failed to share calendar") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toShareResponse(targetName, req.Email, share))
}

// RevokeShare serves DELETE /api/calendars/{id}/shares/{userId}: removes
// userId's Share on the Calendar. Owner-only.
func (h *CalendarHandler) RevokeShare(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	targetUserID, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "userId must be a number")
		return
	}

	if err := h.calendars.RevokeShare(r.Context(), userID, id, targetUserID); respondError(w, err, calendarNotFoundErrors, "failed to revoke share") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// LeaveShare serves POST /api/calendars/{id}/leave: the caller renounces
// their own Share on the Calendar, without the Owner's involvement
// (ADR-0034). Returns not-found if the caller holds no Share on it —
// including when the caller is its Owner, who never has one.
func (h *CalendarHandler) LeaveShare(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	if err := h.calendars.LeaveShare(r.Context(), userID, id); respondError(w, err, calendarNotFoundErrors, "failed to leave calendar") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type groupShareResponse struct {
	GroupID   int64     `json:"groupId"`
	GroupName string    `json:"groupName"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

func toGroupShareResponse(groupName string, s repository.CalendarGroupShare) groupShareResponse {
	return groupShareResponse{GroupID: s.GroupID, GroupName: groupName, Role: s.Role, CreatedAt: s.CreatedAt}
}

func toGroupShareWithNameResponse(s repository.CalendarGroupShareWithGroupName) groupShareResponse {
	return toGroupShareResponse(s.GroupName, s.CalendarGroupShare)
}

// ListGroupShares serves GET /api/calendars/{id}/group-shares: every Group
// Share on the Calendar, Owner-only (ADR-0045).
func (h *CalendarHandler) ListGroupShares(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	shares, err := h.calendars.ListGroupShares(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to list group shares") {
		return
	}

	response := make([]groupShareResponse, len(shares))
	for i, s := range shares {
		response[i] = toGroupShareWithNameResponse(s)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

type groupShareRequest struct {
	GroupID int64  `json:"groupId"`
	Role    string `json:"role"`
}

// ShareWithGroup serves POST /api/calendars/{id}/group-shares: grants a
// Share to req.GroupID with req.Role, or changes an existing Group Share's
// Role if req.GroupID already has one (ADR-0045). Owner-only.
func (h *CalendarHandler) ShareWithGroup(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req groupShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	share, err := h.calendars.ShareWithGroup(r.Context(), userID, id, req.GroupID, req.Role)
	if respondError(w, err, groupShareErrorsWithNotFound, "failed to share calendar with group") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toGroupShareWithNameResponse(share))
}

// RevokeGroupShare serves DELETE /api/calendars/{id}/group-shares/{groupId}:
// removes groupId's Share on the Calendar. Owner-only.
func (h *CalendarHandler) RevokeGroupShare(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	groupID, err := strconv.ParseInt(chi.URLParam(r, "groupId"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "groupId must be a number")
		return
	}

	if err := h.calendars.RevokeGroupShare(r.Context(), userID, id, groupID); respondError(w, err, calendarNotFoundErrors, "failed to revoke group share") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type shareTargetUserResponse struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

type shareTargetGroupResponse struct {
	GroupID int64  `json:"groupId"`
	Name    string `json:"name"`
}

type shareTargetsResponse struct {
	Users  []shareTargetUserResponse  `json:"users"`
	Groups []shareTargetGroupResponse `json:"groups"`
}

// ShareTargets serves GET /api/calendars/{id}/share-targets: every User and
// Group of the Calendar's own Workspace the share dialog may offer as a
// target (#159, ADR-0045). Owner-only.
func (h *CalendarHandler) ShareTargets(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	members, groups, err := h.calendars.ShareTargets(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to list share targets") {
		return
	}

	response := shareTargetsResponse{
		Users:  make([]shareTargetUserResponse, len(members)),
		Groups: make([]shareTargetGroupResponse, len(groups)),
	}
	for i, m := range members {
		response.Users[i] = shareTargetUserResponse{UserID: m.UserID, Name: m.Name, Email: m.Email}
	}
	for i, g := range groups {
		response.Groups[i] = shareTargetGroupResponse{GroupID: g.ID, Name: g.Name}
	}

	httpresponse.JSON(w, http.StatusOK, response)
}
