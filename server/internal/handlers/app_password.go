package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

type AppPasswordHandler struct {
	appPasswords *service.AppPasswordService
}

func NewAppPasswordHandler(appPasswords *service.AppPasswordService) *AppPasswordHandler {
	return &AppPasswordHandler{appPasswords: appPasswords}
}

type appPasswordResponse struct {
	ID         int64      `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func toAppPasswordResponse(p repository.AppPassword) appPasswordResponse {
	return appPasswordResponse{
		ID:         p.ID,
		Label:      p.Label,
		CreatedAt:  p.CreatedAt,
		LastUsedAt: p.LastUsedAt,
	}
}

type createAppPasswordRequest struct {
	Label string `json:"label"`
}

// createAppPasswordResponse embeds the plaintext secret — the only response
// that ever does, since it's shown to the user exactly once (ADR-0024).
type createAppPasswordResponse struct {
	appPasswordResponse
	Secret string `json:"secret"`
}

func (h *AppPasswordHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req createAppPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	result, err := h.appPasswords.Create(r.Context(), userID, req.Label)
	switch {
	case errors.Is(err, service.ErrInvalidAppPasswordLabel):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "label must not be empty")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to create app password")
		return
	}

	httpresponse.JSON(w, http.StatusCreated, createAppPasswordResponse{
		appPasswordResponse: toAppPasswordResponse(result.AppPassword),
		Secret:              result.Secret,
	})
}

func (h *AppPasswordHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	appPasswords, err := h.appPasswords.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list app passwords")
		return
	}

	responses := make([]appPasswordResponse, len(appPasswords))
	for i, p := range appPasswords {
		responses[i] = toAppPasswordResponse(p)
	}

	httpresponse.JSON(w, http.StatusOK, responses)
}

func (h *AppPasswordHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a number")
		return
	}

	err = h.appPasswords.Revoke(r.Context(), userID, id)
	switch {
	case errors.Is(err, service.ErrAppPasswordNotFound):
		httpresponse.Error(w, http.StatusNotFound, "not_found", "app password not found")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to revoke app password")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
