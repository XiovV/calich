package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/service"
)

type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken        string `json:"access_token"`
	MustChangePassword bool   `json:"must_change_password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	result, err := h.auth.Login(r.Context(), req.Username, req.Password)
	if errors.Is(err, service.ErrInvalidCredentials) {
		httpresponse.Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to log in")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    result.RefreshToken,
		Path:     "/",
		Expires:  result.RefreshTokenExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	httpresponse.JSON(w, http.StatusOK, loginResponse{
		AccessToken:        result.AccessToken,
		MustChangePassword: result.MustChangePassword,
	})
}

type meResponse struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	MustChangePassword bool   `json:"must_change_password"`
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	user, err := h.auth.GetUser(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to load user")
		return
	}

	httpresponse.JSON(w, http.StatusOK, meResponse{
		ID:                 user.ID,
		Username:           user.Username,
		MustChangePassword: user.MustChangePassword,
	})
}
