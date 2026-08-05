package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

const refreshCookieName = "refresh_token"

type AuthHandler struct {
	auth *service.AuthService
	// smtpConfigured is whether the self-hoster has set up SMTP transport
	// (ADR-0021) — combined with the user's own email to compute
	// meResponse's emailReminderChannelAvailable.
	smtpConfigured bool
}

func NewAuthHandler(auth *service.AuthService, smtpConfigured bool) *AuthHandler {
	return &AuthHandler{auth: auth, smtpConfigured: smtpConfigured}
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

	setRefreshCookie(w, result.RefreshToken, result.RefreshTokenExpiresAt)

	httpresponse.JSON(w, http.StatusOK, loginResponse{
		AccessToken:        result.AccessToken,
		MustChangePassword: result.MustChangePassword,
	})
}

type meResponse struct {
	ID                 int64   `json:"id"`
	Username           string  `json:"username"`
	MustChangePassword bool    `json:"must_change_password"`
	Email              *string `json:"email"`
	// EmailReminderChannelAvailable is whether the Email Channel can
	// actually be used for a new Reminder — the user has an email set *and*
	// the self-hoster has SMTP configured (ADR-0021, ADR-0010).
	EmailReminderChannelAvailable bool `json:"email_reminder_channel_available"`
	// SyncedDeviceRemindersEnabled is "let my synced devices show reminder
	// pop-ups (disable in-app reminder notifications)" (ADR-0027).
	SyncedDeviceRemindersEnabled bool `json:"synced_device_reminders_enabled"`
}

func (h *AuthHandler) toMeResponse(user repository.User) meResponse {
	return meResponse{
		ID:                            user.ID,
		Username:                      user.Username,
		MustChangePassword:            user.MustChangePassword,
		Email:                         user.Email,
		EmailReminderChannelAvailable: h.smtpConfigured && user.Email != nil,
		SyncedDeviceRemindersEnabled:  user.SyncedDeviceRemindersEnabled,
	}
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

	httpresponse.JSON(w, http.StatusOK, h.toMeResponse(user))
}

type updateEmailRequest struct {
	Email string `json:"email"`
}

func (h *AuthHandler) UpdateEmail(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req updateEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.auth.UpdateEmail(r.Context(), userID, req.Email)
	switch {
	case errors.Is(err, service.ErrInvalidEmail):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "email is not a valid address")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to update email")
		return
	}

	httpresponse.JSON(w, http.StatusOK, h.toMeResponse(user))
}

type updateSyncedDeviceRemindersRequest struct {
	SyncedDeviceRemindersEnabled bool `json:"synced_device_reminders_enabled"`
}

// UpdateSyncedDeviceReminders sets "let my synced devices show reminder
// pop-ups (disable in-app reminder notifications)" (ADR-0027).
func (h *AuthHandler) UpdateSyncedDeviceReminders(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req updateSyncedDeviceRemindersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.auth.UpdateSyncedDeviceReminders(r.Context(), userID, req.SyncedDeviceRemindersEnabled)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to update reminder preference")
		return
	}

	httpresponse.JSON(w, http.StatusOK, h.toMeResponse(user))
}

type refreshResponse struct {
	AccessToken string `json:"access_token"`
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "missing refresh token")
		return
	}

	accessToken, err := h.auth.Refresh(r.Context(), cookie.Value)
	if errors.Is(err, service.ErrInvalidSession) {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "invalid or expired refresh token")
		return
	}
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to refresh session")
		return
	}

	httpresponse.JSON(w, http.StatusOK, refreshResponse{AccessToken: accessToken})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(refreshCookieName); err == nil {
		if err := h.auth.Logout(r.Context(), cookie.Value); err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to log out")
			return
		}
	}

	clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	err := h.auth.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword)
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		httpresponse.Error(w, http.StatusUnauthorized, "invalid_credentials", "current password is incorrect")
		return
	case errors.Is(err, service.ErrInvalidPassword):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "new password must not be empty")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to change password")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func setRefreshCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
