package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/XiovV/calich/server/internal/clientip"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

const refreshCookieName = "refresh_token"

type AuthHandler struct {
	auth *service.AuthService
	// rateLimiter throttles Login and Register (#240, ADR-0070) — the
	// CalDAV Basic auth surface it also covers is throttled separately, in
	// httpauth.RequireCalDAVAuth, since that's where the request actually
	// terminates.
	rateLimiter *service.AuthRateLimiter
	// smtpConfigured is whether the self-hoster has set up SMTP transport
	// (ADR-0021) — combined with the user's own email to compute
	// meResponse's emailReminderChannelAvailable.
	smtpConfigured bool
	// imapConfigured is whether the self-hoster has set up IMAP transport
	// (ADR-0059) — meResponse's invitationRepliesConfigured, which Settings
	// uses to disclose whether an emailed Response ever comes back.
	imapConfigured bool
	// cookieSecure is the Refresh token cookie's Secure attribute
	// (config.Config.CookieSecure, ADR-0009, #239).
	cookieSecure bool
}

func NewAuthHandler(auth *service.AuthService, rateLimiter *service.AuthRateLimiter, smtpConfigured, imapConfigured, cookieSecure bool) *AuthHandler {
	return &AuthHandler{auth: auth, rateLimiter: rateLimiter, smtpConfigured: smtpConfigured, imapConfigured: imapConfigured, cookieSecure: cookieSecure}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken        string `json:"access_token"`
	MustChangePassword bool   `json:"must_change_password"`
	// IsDisabled is whether the account that just authenticated is Disabled
	// (ADR-0044) — Login still succeeds, since re-activating is the one
	// action a Disabled User must still be able to reach with no
	// instance-wide Admin left to do it for them. The frontend gates
	// everything else off this flag.
	IsDisabled bool `json:"is_disabled"`
}

var loginErrors = []errorCase{
	{service.ErrInvalidCredentials, unauthorized("invalid_credentials", "invalid email or password")},
	{service.ErrAuthRateLimitExceeded, rateLimited("too many attempts, please try again later")},
}

var registerErrors = []errorCase{
	{service.ErrAuthRateLimitExceeded, rateLimited("too many attempts, please try again later")},
	{service.ErrSignupsDisabled, forbidden("self-registration is disabled on this instance")},
	{service.ErrInvalidDisplayName, badRequest("name must not be empty and must be at most 100 characters")},
	{service.ErrEmailRequired, badRequest("email is required")},
	{service.ErrInvalidEmail, badRequest("email must be a valid address and must not contain whitespace or a colon")},
	{service.ErrEmailTaken, conflict("email_taken", "email is already taken")},
	{service.ErrInvalidPassword, badRequest("password must not be empty")},
	{service.ErrPasswordTooLong, badRequest("password must be at most 72 bytes")},
}

var previewWorkspaceInviteErrors = []errorCase{
	{service.ErrWorkspaceInviteInvalid, unauthorized("invite_invalid", "invite is invalid or has expired")},
}

var acceptWorkspaceInviteErrors = []errorCase{
	{service.ErrWorkspaceInviteInvalid, unauthorized("invite_invalid", "invite is invalid or has expired")},
	{service.ErrInvalidDisplayName, badRequest("name must not be empty and must be at most 100 characters")},
	{service.ErrEmailTaken, conflict("email_taken", "email is already taken")},
	{service.ErrInvalidPassword, badRequest("password must not be empty")},
	{service.ErrPasswordTooLong, badRequest("password must be at most 72 bytes")},
}

var joinWorkspaceInviteErrors = []errorCase{
	{service.ErrWorkspaceInviteInvalid, unauthorized("invite_invalid", "invite is invalid or has expired")},
	{service.ErrWorkspaceInviteEmailMismatch, conflict("invite_email_mismatch", service.ErrWorkspaceInviteEmailMismatch.Error())},
	{service.ErrAlreadyWorkspaceMember, conflict("already_member", service.ErrAlreadyWorkspaceMember.Error())},
}

var updateEmailErrors = []errorCase{
	{service.ErrEmailRequired, badRequest("email is required")},
	{service.ErrInvalidEmail, badRequest("email must be a valid address and must not contain whitespace or a colon")},
	{service.ErrEmailTaken, conflict("email_taken", "email is already taken")},
}

var updateNameErrors = []errorCase{
	{service.ErrInvalidDisplayName, badRequest("name must not be empty and must be at most 100 characters")},
}

var updatePreferencesErrors = []errorCase{
	{service.ErrInvalidWeekStart, badRequest("week_start must be between 0 and 6")},
	{service.ErrInvalidDefaultView, badRequest("default_view must be one of day, week, month, year")},
	{service.ErrInvalidTimeFormat, badRequest("time_format must be one of 12h, 24h")},
	{service.ErrInvalidWorkingHours, badRequest("working_hours_start and working_hours_end must both be set (0-1439 minutes since midnight, start < end) or both be null")},
}

var refreshErrors = []errorCase{
	{service.ErrInvalidSession, unauthorized("unauthorized", "invalid or expired refresh token")},
}

// ErrInvalidCredentials renders differently here than on login: it means the
// *current* password was wrong, not the username/password pair.
var changePasswordErrors = []errorCase{
	{service.ErrInvalidCredentials, unauthorized("invalid_credentials", "current password is incorrect")},
	{service.ErrInvalidPassword, badRequest("new password must not be empty")},
	{service.ErrPasswordTooLong, badRequest("new password must be at most 72 bytes")},
}

type setupStatusResponse struct {
	HasAccounts bool `json:"has_accounts"`
	// SignupsEnabled mirrors ENABLE_SIGNUPS (ADR-0044, #235) — the frontend
	// uses it to decide whether to offer self-registration (the login page's
	// "Create one" link, /register's form) once HasAccounts is true. It's
	// irrelevant while HasAccounts is false: Register always allows the very
	// first account regardless of this flag.
	SignupsEnabled bool `json:"signups_enabled"`
}

// SetupStatus reports whether the instance has any accounts yet (#169,
// ADR-0047) and whether self-registration is open (#235) — public and
// unauthenticated, since it must be answerable before anyone has signed in,
// and it says nothing about who's asking. The frontend uses HasAccounts to
// redirect a first-run visitor straight to Register instead of showing a
// Sign-in screen there are no accounts to sign in to, and SignupsEnabled to
// decide whether to offer registration at all thereafter.
func (h *AuthHandler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	hasAccounts, err := h.auth.HasAnyAccounts(r.Context())
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to check setup status")
		return
	}

	httpresponse.JSON(w, http.StatusOK, setupStatusResponse{
		HasAccounts:    hasAccounts,
		SignupsEnabled: h.auth.SignupsEnabled(),
	})
}

// Login is throttled ahead of AuthService.Login itself (#240, ADR-0070):
// CheckAuth's cheap SQL count runs before any password comparison, so a
// flood past the ceiling never reaches AuthService.Login's bcrypt compare.
// A failure charges both the submitted Email's and the caller's IP's own
// buckets identically whether or not that Email names a real account
// (ADR-0047's enumeration posture); a success clears the Email bucket alone.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	ip := clientip.From(r)

	if err := h.rateLimiter.CheckAuth(r.Context(), req.Email, ip); respondError(w, err, loginErrors, "failed to log in") {
		return
	}

	result, err := h.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			if recErr := h.rateLimiter.RecordAuthFailure(r.Context(), req.Email, ip); recErr != nil {
				httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to log in")
				return
			}
		}
		respondError(w, err, loginErrors, "failed to log in")
		return
	}

	if err := h.rateLimiter.ClearAuthFailures(r.Context(), req.Email); err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to log in")
		return
	}

	h.setRefreshCookie(w, result.RefreshToken, result.RefreshTokenExpiresAt)

	httpresponse.JSON(w, http.StatusOK, loginResponse{
		AccessToken:        result.AccessToken,
		MustChangePassword: result.MustChangePassword,
		IsDisabled:         result.IsDisabled,
	})
}

// registerRequest's Name is a display name only (ADR-0047) — Email is the
// account's login identifier, validated separately in the service layer.
type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Register is the public, unauthenticated first-run bootstrap form and
// self-registration endpoint (ADR-0044): it always succeeds for the very
// first account on the instance, and otherwise only when ENABLE_SIGNUPS is
// true. A successful call creates a brand-new Workspace owned by the
// registrant and logs them straight in.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	ip := clientip.From(r)

	if err := h.rateLimiter.CheckRegister(r.Context(), ip); respondError(w, err, registerErrors, "failed to register") {
		return
	}
	// Charged on every call past the ceiling check, success or failure
	// (#240, ADR-0070) — what's throttled is how often Register is invoked
	// at all, not a count of wrong guesses against a credential.
	if err := h.rateLimiter.RecordRegisterAttempt(r.Context(), ip); err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to register")
		return
	}

	result, err := h.auth.Register(r.Context(), req.Name, req.Email, req.Password)
	if respondError(w, err, registerErrors, "failed to register") {
		return
	}

	h.setRefreshCookie(w, result.RefreshToken, result.RefreshTokenExpiresAt)

	httpresponse.JSON(w, http.StatusOK, loginResponse{
		AccessToken:        result.AccessToken,
		MustChangePassword: result.MustChangePassword,
	})
}

type previewWorkspaceInviteResponse struct {
	WorkspaceName string `json:"workspace_name"`
	Email         string `json:"email"`
	// UserExists tells the accept-workspace-invite page whether to collect a
	// name and password (false: the invited email has no account yet) or
	// just prompt the invitee to log in (true) before accepting (ADR-0044).
	UserExists bool `json:"user_exists"`
}

// PreviewWorkspaceInvite is the public, unauthenticated lookup the
// accept-workspace-invite page calls before the invitee does anything
// (ADR-0044): it resolves a token to the Workspace and email it names,
// without consuming it, and reports whether that email already has an
// account — the page uses this to decide which form to show.
func (h *AuthHandler) PreviewWorkspaceInvite(w http.ResponseWriter, r *http.Request) {
	preview, err := h.auth.PreviewWorkspaceInvite(r.Context(), r.URL.Query().Get("token"))
	if respondError(w, err, previewWorkspaceInviteErrors, "failed to preview invite") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, previewWorkspaceInviteResponse{
		WorkspaceName: preview.WorkspaceName,
		Email:         preview.Email,
		UserExists:    preview.UserExists,
	})
}

type acceptWorkspaceInviteRequest struct {
	Token    string `json:"token"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// AcceptWorkspaceInvite is the public, unauthenticated new-account accept
// path (ADR-0044): a live token naming an email with no existing User,
// alongside a name and password, atomically creates the User, adds them as a
// Member of the inviting Workspace, and logs them straight in — mirroring
// AcceptInvite's shape for the account-level Invite this replaces.
func (h *AuthHandler) AcceptWorkspaceInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptWorkspaceInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	result, err := h.auth.AcceptWorkspaceInviteNewAccount(r.Context(), req.Token, req.Name, req.Password)
	if respondError(w, err, acceptWorkspaceInviteErrors, "failed to accept invite") {
		return
	}

	h.setRefreshCookie(w, result.RefreshToken, result.RefreshTokenExpiresAt)

	httpresponse.JSON(w, http.StatusOK, loginResponse{
		AccessToken:        result.AccessToken,
		MustChangePassword: result.MustChangePassword,
	})
}

type joinWorkspaceInviteRequest struct {
	Token string `json:"token"`
}

// JoinWorkspaceInvite is the authenticated existing-account accept path
// (ADR-0044): the caller must already be logged in as the User whose account
// email matches the invite's, and accepting just adds a WorkspaceMember row
// for the inviting Workspace — no new account, no password step.
func (h *AuthHandler) JoinWorkspaceInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req joinWorkspaceInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	workspace, err := h.auth.AcceptWorkspaceInviteExisting(r.Context(), userID, req.Token)
	if respondError(w, err, joinWorkspaceInviteErrors, "failed to accept invite") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toWorkspaceResponse(workspace))
}

type meResponse struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	MustChangePassword bool   `json:"must_change_password"`
	Email              string `json:"email"`
	// EmailReminderChannelAvailable is whether the Email Channel can
	// actually be used for a new Reminder — every account has an email now
	// (ADR-0047), so this collapses to just whether the self-hoster has SMTP
	// configured (ADR-0021, ADR-0010).
	EmailReminderChannelAvailable bool `json:"email_reminder_channel_available"`
	// InvitationRepliesConfigured is whether this instance has IMAP set up
	// to read Attendee Responses back (ADR-0059) — independent of
	// EmailReminderChannelAvailable's SMTP: an instance can send
	// Invitations with this false, those Attendees just stay needs-action
	// forever. Settings discloses this once, beside the SMTP configuration.
	InvitationRepliesConfigured bool `json:"invitation_replies_configured"`
	// SyncedDeviceRemindersEnabled is "let my synced devices show reminder
	// pop-ups (disable in-app reminder notifications)" (ADR-0027).
	SyncedDeviceRemindersEnabled bool `json:"synced_device_reminders_enabled"`
	// Preferences (ADR-0039): per-User display settings, wired up to the
	// frontend (#128, #129, #130, #131).
	WeekStart         int    `json:"week_start"`
	DefaultView       string `json:"default_view"`
	TimeFormat        string `json:"time_format"`
	WorkingHoursStart *int   `json:"working_hours_start"`
	WorkingHoursEnd   *int   `json:"working_hours_end"`
}

func (h *AuthHandler) toMeResponse(user repository.User) meResponse {
	return meResponse{
		ID:                            user.ID,
		Name:                          user.Name,
		MustChangePassword:            user.MustChangePassword,
		Email:                         user.Email,
		EmailReminderChannelAvailable: h.smtpConfigured,
		InvitationRepliesConfigured:   h.imapConfigured,
		SyncedDeviceRemindersEnabled:  user.SyncedDeviceRemindersEnabled,
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

type updateNameRequest struct {
	Name string `json:"name"`
}

// UpdateName renames the caller's own display name (#125, ADR-0047). No
// current password is required, matching UpdateEmail — the Access token
// already proves identity.
func (h *AuthHandler) UpdateName(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req updateNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	user, err := h.auth.UpdateName(r.Context(), userID, req.Name)
	if respondError(w, err, updateNameErrors, "failed to update name") {
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
// "unset" (ADR-0039). WorkingHoursStart/End alone can't tell "key present as
// null" apart from "key absent" this way — encoding/json collapses both to a
// nil pointer — so UpdatePreferences also checks the raw body for which of
// the two keys were actually named.
type updatePreferencesRequest struct {
	WeekStart         *int    `json:"week_start"`
	DefaultView       *string `json:"default_view"`
	TimeFormat        *string `json:"time_format"`
	WorkingHoursStart *int    `json:"working_hours_start"`
	WorkingHoursEnd   *int    `json:"working_hours_end"`
}

// UpdatePreferences applies whichever Preferences (ADR-0039) are present in
// the request body, leaving the rest untouched.
func (h *AuthHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	var req updatePreferencesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	// A second, raw decode just to see which top-level keys were named — the
	// only way to tell "working_hours_start: null" (touched, clearing) apart
	// from an omitted key (untouched), since both land on req as nil above.
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawFields); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	_, startNamed := rawFields["working_hours_start"]
	_, endNamed := rawFields["working_hours_end"]

	update := service.PreferencesUpdate{
		WeekStart:   req.WeekStart,
		DefaultView: req.DefaultView,
		TimeFormat:  req.TimeFormat,
	}
	// Working hours is a pair: naming either key builds the update, and
	// AuthService.UpdatePreferences rejects it unless both bounds ended up set
	// or both ended up null — that also catches a request naming only one key.
	if startNamed || endNamed {
		update.WorkingHours = &service.WorkingHoursUpdate{
			Start: req.WorkingHoursStart,
			End:   req.WorkingHoursEnd,
		}
	}

	user, err := h.auth.UpdatePreferences(r.Context(), userID, update)
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

	h.clearRefreshCookie(w)
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

	h.setRefreshCookie(w, result.RefreshToken, result.RefreshTokenExpiresAt)

	httpresponse.JSON(w, http.StatusOK, changePasswordResponse{AccessToken: result.AccessToken})
}

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}
