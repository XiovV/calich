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
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	IsAdmin            bool      `json:"is_admin"`
	IsDisabled         bool      `json:"is_disabled"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
}

func toAccountResponse(u repository.User) accountResponse {
	return accountResponse{
		ID:                 u.ID,
		Username:           u.Username,
		IsAdmin:            u.IsAdmin,
		IsDisabled:         u.IsDisabled,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt,
	}
}

var createAccountErrors = []errorCase{
	{service.ErrInvalidUsername, badRequest("username must not be empty")},
	{service.ErrInvalidPassword, badRequest("temporary password must not be empty")},
	{service.ErrUsernameTaken, conflict("username_taken", "username is already taken")},
}

var resetPasswordErrors = []errorCase{
	{service.ErrInvalidPassword, badRequest("temporary password must not be empty")},
	{repository.ErrNotFound, notFound("account not found")},
}

var setAdminErrors = []errorCase{
	{service.ErrLastAdmin, conflict("last_admin", "cannot remove the last remaining admin")},
	{repository.ErrNotFound, notFound("account not found")},
}

var setDisabledErrors = []errorCase{
	{service.ErrLastAdmin, conflict("last_admin", "cannot disable the last remaining admin")},
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
