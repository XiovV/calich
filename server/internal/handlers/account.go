package handlers

import (
	"errors"
	"net/http"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/service"
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

// respondSoleWorkspaceOwnerError renders a *service.SoleWorkspaceOwnerError
// as a 409 naming the blocking Workspace(s) — err.Error() already includes
// them (service/account.go), so unlike the rest of this package's error
// cases, this one can't use a fixed table of precomputed response bodies. It
// reports whether it wrote a response, matching respondError's calling
// convention.
func respondSoleWorkspaceOwnerError(w http.ResponseWriter, err error) bool {
	var soleOwnerErr *service.SoleWorkspaceOwnerError
	if !errors.As(err, &soleOwnerErr) {
		return false
	}
	httpresponse.Error(w, http.StatusConflict, "sole_workspace_owner", soleOwnerErr.Error())
	return true
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
	userID := httpauth.MustUserID(r.Context())

	req, ok := decodeJSON[setDisabledRequest](w, r)
	if !ok {
		return
	}

	user, err := h.accounts.SetDisabled(r.Context(), userID, req.IsDisabled)
	if err != nil {
		if !respondSoleWorkspaceOwnerError(w, err) {
			respondError(w, err, nil, "failed to update account status")
		}
		return
	}

	httpresponse.JSON(w, http.StatusOK, setDisabledResponse{IsDisabled: user.IsDisabled})
}

type transferCandidateResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
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
	userID := httpauth.MustUserID(r.Context())

	impact, err := h.accounts.DeleteImpact(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to compute delete impact")
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

var deleteAccountErrors = []errorCase{
	{service.ErrInvalidDisposition, badRequest(service.ErrInvalidDisposition.Error())},
	{service.ErrTransferTargetRequired, badRequest(service.ErrTransferTargetRequired.Error())},
	{service.ErrCannotTransferToSelf, badRequest(service.ErrCannotTransferToSelf.Error())},
	{service.ErrTransferTargetNotWorkspaceMember, badRequest(service.ErrTransferTargetNotWorkspaceMember.Error())},
	{service.ErrCalendarNotOwned, badRequest(service.ErrCalendarNotOwned.Error())},
	{service.ErrDuplicateDisposition, badRequest(service.ErrDuplicateDisposition.Error())},
	{service.ErrMissingDisposition, badRequest(service.ErrMissingDisposition.Error())},
}

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
	userID := httpauth.MustUserID(r.Context())

	req, ok := decodeJSON[deleteAccountRequest](w, r)
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

	if err := h.accounts.Delete(r.Context(), userID, dispositions); err != nil {
		if respondSoleWorkspaceOwnerError(w, err) {
			return
		}
		respondError(w, err, deleteAccountErrors, "failed to delete account")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
