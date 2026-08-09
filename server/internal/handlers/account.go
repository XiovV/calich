package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// AccountHandler is account administration (ADR-0037) — every route it
// serves sits behind httpauth.RequireAdmin, so the caller's own identity is
// never consulted here beyond that gate.
type AccountHandler struct {
	accounts *service.AccountService
}

func NewAccountHandler(accounts *service.AccountService) *AccountHandler {
	return &AccountHandler{accounts: accounts}
}

type accountResponse struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	IsAdmin            bool       `json:"is_admin"`
	IsDisabled         bool       `json:"is_disabled"`
	MustChangePassword bool       `json:"must_change_password"`
	CreatedAt          time.Time  `json:"created_at"`
	// IsPending is whether this account was created via an Invite and hasn't
	// accepted it yet (ADR-0042) — a distinct state from IsDisabled even
	// though both block Login, Refresh, and CalDAV Basic auth.
	IsPending bool `json:"is_pending"`
	// InviteExpiresAt is when the current outstanding Invite token stops
	// being acceptable; nil once IsPending is false.
	InviteExpiresAt *time.Time `json:"invite_expires_at,omitempty"`
}

func toAccountResponse(u repository.User) accountResponse {
	return accountResponse{
		ID:                 u.ID,
		Username:           u.Username,
		IsAdmin:            u.IsAdmin,
		IsDisabled:         u.IsDisabled,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt,
		IsPending:          u.IsPending(),
		InviteExpiresAt:    u.InviteExpiresAt,
	}
}

// inviteResponse is CreateInvite's and ReissueInvite's response — the
// resulting Pending account alongside the plaintext Invite token, which is
// never retrievable again once this response is sent (ADR-0042).
type inviteResponse struct {
	Account accountResponse `json:"account"`
	Token   string          `json:"token"`
}

func toInviteResponse(result service.InviteResult) inviteResponse {
	return inviteResponse{Account: toAccountResponse(result.User), Token: result.Token}
}

var createAccountErrors = []errorCase{
	{service.ErrInvalidUsername, badRequest("username must not be empty")},
	{service.ErrInvalidPassword, badRequest("temporary password must not be empty")},
	{service.ErrUsernameTaken, conflict("username_taken", "username is already taken")},
}

// pendingAccountErrors is shared by every operation ADR-0037 wrote for an
// already-Active account — ResetPassword, SetAdmin, SetDisabled — which
// AccountService refuses against a Pending one instead (ErrUserIsPending).
var pendingAccountErrors = []errorCase{
	{service.ErrUserIsPending, conflict("account_pending", service.ErrUserIsPending.Error())},
}

var resetPasswordErrors = alsoHandling(pendingAccountErrors,
	errorCase{service.ErrInvalidPassword, badRequest("temporary password must not be empty")},
	errorCase{repository.ErrNotFound, notFound("account not found")},
)

var setAdminErrors = alsoHandling(pendingAccountErrors,
	errorCase{service.ErrLastAdmin, conflict("last_admin", "cannot remove the last remaining admin")},
	errorCase{repository.ErrNotFound, notFound("account not found")},
)

var setDisabledErrors = alsoHandling(pendingAccountErrors,
	errorCase{service.ErrLastAdmin, conflict("last_admin", "cannot disable the last remaining admin")},
	errorCase{repository.ErrNotFound, notFound("account not found")},
)

var setUsernameErrors = []errorCase{
	{service.ErrInvalidUsername, badRequest("username must not be empty, must not contain whitespace or a colon, and must be at most 64 characters")},
	{service.ErrUsernameTaken, conflict("username_taken", "username is already taken")},
	{repository.ErrNotFound, notFound("account not found")},
}

var usernameImpactErrors = []errorCase{
	{repository.ErrNotFound, notFound("account not found")},
}

var deleteImpactErrors = []errorCase{
	{repository.ErrNotFound, notFound("account not found")},
}

var createInviteErrors = []errorCase{
	{service.ErrInvalidUsername, badRequest("username must not be empty, must not contain whitespace or a colon, and must be at most 64 characters")},
	{service.ErrUsernameTaken, conflict("username_taken", "username is already taken")},
}

// notPendingErrors is shared by ReissueInvite and CancelInvite, the two
// operations that only make sense against a Pending account.
var notPendingErrors = []errorCase{
	{service.ErrUserNotPending, conflict("not_pending", service.ErrUserNotPending.Error())},
	{repository.ErrNotFound, notFound("account not found")},
}

var reissueInviteErrors = notPendingErrors

var cancelInviteErrors = notPendingErrors

var deleteAccountErrors = []errorCase{
	{service.ErrInvalidDisposition, badRequest(service.ErrInvalidDisposition.Error())},
	{service.ErrTransferTargetRequired, badRequest(service.ErrTransferTargetRequired.Error())},
	{service.ErrCannotTransferToSelf, badRequest(service.ErrCannotTransferToSelf.Error())},
	{service.ErrTransferTargetNotFound, badRequest(service.ErrTransferTargetNotFound.Error())},
	{service.ErrLastAdmin, conflict("last_admin", "cannot delete the last remaining admin")},
	{repository.ErrNotFound, notFound("account not found")},
}

type createAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AccountHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.accounts.Create(r.Context(), req.Username, req.Password)
	if respondError(w, err, createAccountErrors, "failed to create account") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toAccountResponse(user))
}

type createInviteRequest struct {
	Username string `json:"username"`
}

// CreateInvite makes a new Pending account and issues its Invite (ADR-0042)
// — the returned token is shown to the Admin exactly once, to distribute
// however they choose.
func (h *AccountHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	result, err := h.accounts.CreateInvite(r.Context(), req.Username)
	if respondError(w, err, createInviteErrors, "failed to create invite") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toInviteResponse(result))
}

// ReissueInvite replaces id's outstanding Invite with a fresh token and
// expiry (ADR-0042), invalidating whichever token came before it.
func (h *AccountHandler) ReissueInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	result, err := h.accounts.ReissueInvite(r.Context(), id)
	if respondError(w, err, reissueInviteErrors, "failed to reissue invite") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toInviteResponse(result))
}

// CancelInvite deletes id's Pending account outright (ADR-0042) — no
// disposition for owned Calendars, since a Pending account owns nothing a
// disposition would need to decide about.
func (h *AccountHandler) CancelInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	if err := h.accounts.CancelInvite(r.Context(), id); respondError(w, err, cancelInviteErrors, "failed to cancel invite") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AccountHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.accounts.List(r.Context())
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list accounts")
		return
	}

	responses := make([]accountResponse, len(users))
	for i, u := range users {
		responses[i] = toAccountResponse(u)
	}

	httpresponse.JSON(w, http.StatusOK, responses)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

func (h *AccountHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.accounts.ResetPassword(r.Context(), id, req.Password)
	if respondError(w, err, resetPasswordErrors, "failed to reset password") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toAccountResponse(user))
}

type setAdminRequest struct {
	IsAdmin bool `json:"is_admin"`
}

func (h *AccountHandler) SetAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	var req setAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.accounts.SetAdmin(r.Context(), id, req.IsAdmin)
	if respondError(w, err, setAdminErrors, "failed to update admin status") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toAccountResponse(user))
}

type setDisabledRequest struct {
	IsDisabled bool `json:"is_disabled"`
}

func (h *AccountHandler) SetDisabled(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	var req setDisabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.accounts.SetDisabled(r.Context(), id, req.IsDisabled)
	if respondError(w, err, setDisabledErrors, "failed to update account status") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toAccountResponse(user))
}

type setUsernameRequest struct {
	Username string `json:"username"`
}

func (h *AccountHandler) SetUsername(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	var req setUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.accounts.SetUsername(r.Context(), id, req.Username)
	if respondError(w, err, setUsernameErrors, "failed to update username") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toAccountResponse(user))
}

type usernameImpactResponse struct {
	AppPasswordCount int `json:"app_password_count"`
}

// UsernameImpact reports how many App passwords id's account holds (#125) —
// the Admin-facing preview shown before renaming somebody else's account, so
// the Admin knows how many of their synced devices are about to stop
// syncing until reconfigured.
func (h *AccountHandler) UsernameImpact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	count, err := h.accounts.UsernameImpact(r.Context(), id)
	if respondError(w, err, usernameImpactErrors, "failed to compute username impact") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, usernameImpactResponse{AppPasswordCount: count})
}

type calendarImpactResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ShareCount int    `json:"share_count"`
}

type deleteImpactResponse struct {
	Calendars         []calendarImpactResponse `json:"calendars"`
	AffectedUserCount int                       `json:"affected_user_count"`
}

// DeleteImpact reports what deleting id's account would affect (ADR-0037):
// which of its Calendars have Shares, and how many Users would lose Access
// — so an Admin can see the impact before committing to DispositionDelete.
func (h *AccountHandler) DeleteImpact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	impact, err := h.accounts.DeleteImpact(r.Context(), id)
	if respondError(w, err, deleteImpactErrors, "failed to compute delete impact") {
		return
	}

	calendars := make([]calendarImpactResponse, len(impact.Calendars))
	for i, c := range impact.Calendars {
		calendars[i] = calendarImpactResponse{ID: c.ID, Name: c.Name, ShareCount: c.ShareCount}
	}

	httpresponse.JSON(w, http.StatusOK, deleteImpactResponse{Calendars: calendars, AffectedUserCount: impact.AffectedUserCount})
}

type deleteAccountRequest struct {
	// OwnedCalendars is the disposition for id's owned Calendars — "transfer"
	// or "delete" (ADR-0037). There is no default: an empty or unrecognized
	// value is rejected.
	OwnedCalendars string `json:"owned_calendars"`
	// TransferTo is required when OwnedCalendars is "transfer" — the User
	// every Calendar id owned is reassigned to.
	TransferTo *int64 `json:"transfer_to,omitempty"`
}

func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	var req deleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	if err := h.accounts.Delete(r.Context(), id, req.OwnedCalendars, req.TransferTo); respondError(w, err, deleteAccountErrors, "failed to delete account") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
