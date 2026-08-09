package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/service"
)

// AccountHandler is self-service account lifecycle (ADR-0044): a User
// disabling/re-activating or deleting their own account, and nobody else's
// — every route it serves acts on the caller identified by their own access
// token, never on an id path parameter.
type AccountHandler struct {
	accounts *service.AccountService
}

func NewAccountHandler(accounts *service.AccountService) *AccountHandler {
	return &AccountHandler{accounts: accounts}
}

var soleWorkspaceOwnerErrors = []errorCase{
	{service.ErrSoleWorkspaceOwner, conflict("sole_workspace_owner", service.ErrSoleWorkspaceOwner.Error())},
}

type setDisabledRequest struct {
	IsDisabled bool `json:"is_disabled"`
}

type setDisabledResponse struct {
	IsDisabled bool `json:"is_disabled"`
}

// SetDisabled disables or re-activates the caller's own account (ADR-0044).
// Reachable while Disabled — it sits behind httpauth.RequireAuth only, not
// RequireEnabledUser, since re-activating is exactly the action a Disabled
// User must still be able to reach.
func (h *AccountHandler) SetDisabled(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req setDisabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.accounts.SetDisabled(r.Context(), userID, req.IsDisabled)
	if respondError(w, err, soleWorkspaceOwnerErrors, "failed to update account status") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, setDisabledResponse{IsDisabled: user.IsDisabled})
}

type transferCandidateResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type calendarImpactResponse struct {
	ID                 string                      `json:"id"`
	Name               string                      `json:"name"`
	WorkspaceID        int64                       `json:"workspace_id"`
	WorkspaceName      string                      `json:"workspace_name"`
	ShareCount         int                         `json:"share_count"`
	TransferCandidates []transferCandidateResponse `json:"transfer_candidates"`
}

type deleteImpactResponse struct {
	Calendars []calendarImpactResponse `json:"calendars"`
}

// DeleteImpact reports what deleting the caller's own account would affect
// (ADR-0044): every Calendar they own, across every Workspace they belong
// to, alongside its Share count and the Workspace Members it could be
// transferred to instead — the preview shown before a transfer-or-delete
// choice is made for each one.
func (h *AccountHandler) DeleteImpact(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	impact, err := h.accounts.DeleteImpact(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to compute delete impact")
		return
	}

	calendars := make([]calendarImpactResponse, len(impact.Calendars))
	for i, c := range impact.Calendars {
		candidates := make([]transferCandidateResponse, len(c.TransferCandidates))
		for j, candidate := range c.TransferCandidates {
			candidates[j] = transferCandidateResponse{ID: candidate.ID, Username: candidate.Username}
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

var deleteAccountErrors = alsoHandling(soleWorkspaceOwnerErrors,
	errorCase{service.ErrInvalidDisposition, badRequest(service.ErrInvalidDisposition.Error())},
	errorCase{service.ErrTransferTargetRequired, badRequest(service.ErrTransferTargetRequired.Error())},
	errorCase{service.ErrCannotTransferToSelf, badRequest(service.ErrCannotTransferToSelf.Error())},
	errorCase{service.ErrTransferTargetNotWorkspaceMember, badRequest(service.ErrTransferTargetNotWorkspaceMember.Error())},
	errorCase{service.ErrCalendarNotOwned, badRequest(service.ErrCalendarNotOwned.Error())},
	errorCase{service.ErrDuplicateDisposition, badRequest(service.ErrDuplicateDisposition.Error())},
	errorCase{service.ErrMissingDisposition, badRequest(service.ErrMissingDisposition.Error())},
)

type calendarDispositionRequest struct {
	CalendarID string `json:"calendar_id"`
	// Disposition is "transfer" or "delete" (ADR-0044). There is no default:
	// every Calendar the caller owns needs one, named explicitly.
	Disposition string `json:"disposition"`
	// TransferTo is required, and must name a current Member of the
	// Calendar's own Workspace, when Disposition is "transfer".
	TransferTo *int64 `json:"transfer_to,omitempty"`
}

type deleteAccountRequest struct {
	Calendars []calendarDispositionRequest `json:"calendars"`
}

// Delete removes the caller's own account outright (ADR-0044), requiring an
// explicit transfer-or-delete disposition for every Calendar they own,
// across every Workspace they belong to.
func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req deleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
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

	if err := h.accounts.Delete(r.Context(), userID, dispositions); respondError(w, err, deleteAccountErrors, "failed to delete account") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
