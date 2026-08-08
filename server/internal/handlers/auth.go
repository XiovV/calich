package handlers

import (
	"encoding/json"
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

var loginErrors = []errorCase{
	{service.ErrInvalidCredentials, unauthorized("invalid_credentials", "invalid username or password")},
	{service.ErrAccountDisabled, unauthorized("account_disabled", "this account has been disabled")},
}

var updateEmailErrors = []errorCase{
	{service.ErrInvalidEmail, badRequest("email is not a valid address")},
}

var updateUsernameErrors = []errorCase{
	{service.ErrInvalidUsername, badRequest("username must not be empty, must not contain whitespace or a colon, and must be at most 64 characters")},
	{service.ErrUsernameTaken, conflict("username_taken", "username is already taken")},
}

var updatePreferencesErrors = []errorCase{
	{service.ErrInvalidWeekStart, badRequest("week_start must be between 0 and 6")},
	{service.ErrInvalidDefaultView, badRequest("default_view must be one of day, week, month, year")},
}

var refreshErrors = []errorCase{
	{service.ErrInvalidSession, unauthorized("unauthorized", "invalid or expired refresh token")},
	{service.ErrAccountDisabled, unauthorized("account_disabled", "this account has been disabled")},
}

// ErrInvalidCredentials renders differently here than on login: it means the
// *current* password was wrong, not the username/password pair.
var changePasswordErrors = []errorCase{
	{service.ErrInvalidCredentials, unauthorized("invalid_credentials", "current password is incorrect")},
	{service.ErrInvalidPassword, badRequest("new password must not be empty")},
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	result, err := h.auth.Login(r.Context(), req.Username, req.Password)
	if respondError(w, err, loginErrors, "failed to log in") {
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
	// IsAdmin is authority over who exists on the instance (ADR-0037) —
	// without it the web app cannot decide whether to render any
	// administration UI (#119).
	IsAdmin bool `json:"is_admin"`
	// Preferences (ADR-0039): per-User display settings. Only WeekStart is
	// wired up to the frontend yet (#128) — the rest wait on #129, #130,
	// #131 — but all five are served here as soon as they exist on the User.
	WeekStart         int    `json:"week_start"`
	DefaultView       string `json:"default_view"`
	TimeFormat        string `json:"time_format"`
	WorkingHoursStart *int   `json:"working_hours_start"`
	WorkingHoursEnd   *int   `json:"working_hours_end"`
}

func (h *AuthHandler) toMeResponse(user repository.User) meResponse {
	return meResponse{
		ID:                            user.ID,
		Username:                      user.Username,
		MustChangePassword:            user.MustChangePassword,
		Email:                         user.Email,
		EmailReminderChannelAvailable: h.smtpConfigured && user.Email != nil,
		SyncedDeviceRemindersEnabled:  user.SyncedDeviceRemindersEnabled,
		IsAdmin:                       user.IsAdmin,
		WeekStart:                     user.WeekStart,
		DefaultView:                   user.DefaultView,
		TimeFormat:                    user.TimeFormat,
		WorkingHoursStart:             user.WorkingHoursStart,
		WorkingHoursEnd:               user.WorkingHoursEnd,
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
	if respondError(w, err, updateEmailErrors, "failed to update email") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, h.toMeResponse(user))
}

type updateUsernameRequest struct {
	Username string `json:"username"`
}

// UpdateUsername renames the caller's own account (#125). No current
// password is required, matching UpdateEmail — the Access token already
// proves identity.
func (h *AuthHandler) UpdateUsername(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req updateUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.auth.UpdateUsername(r.Context(), userID, req.Username)
	if respondError(w, err, updateUsernameErrors, "failed to update username") {
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

// updatePreferencesRequest decodes into a pointer so an absent field is
// distinguishable from a zero value — `week_start: 0` is Sunday, not
// "unset" (ADR-0039).
type updatePreferencesRequest struct {
	WeekStart   *int    `json:"week_start"`
	DefaultView *string `json:"default_view"`
}

// UpdatePreferences applies whichever Preferences (ADR-0039) are present in
// the request body, leaving the rest untouched.
func (h *AuthHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req updatePreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.auth.UpdatePreferences(r.Context(), userID, service.PreferencesUpdate{
		WeekStart:   req.WeekStart,
		DefaultView: req.DefaultView,
	})
	if respondError(w, err, updatePreferencesErrors, "failed to update preferences") {
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
	if respondError(w, err, refreshErrors, "failed to refresh session") {
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

type changePasswordResponse struct {
	AccessToken string `json:"access_token"`
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

	result, err := h.auth.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword)
	if respondError(w, err, changePasswordErrors, "failed to change password") {
		return
	}

	setRefreshCookie(w, result.RefreshToken, result.RefreshTokenExpiresAt)

	httpresponse.JSON(w, http.StatusOK, changePasswordResponse{AccessToken: result.AccessToken})
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
