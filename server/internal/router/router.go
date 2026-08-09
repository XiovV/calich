package router

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/XiovV/calendar/server/internal/handlers"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/spa"
	"github.com/XiovV/calendar/server/internal/static"
)

func New(logger *slog.Logger, authHandler *handlers.AuthHandler, calendarHandler *handlers.CalendarHandler, eventHandler *handlers.EventHandler, attachmentHandler *handlers.AttachmentHandler, notificationHandler *handlers.NotificationHandler, appPasswordHandler *handlers.AppPasswordHandler, accountHandler *handlers.AccountHandler, userHandler *handlers.UserHandler, workspaceHandler *handlers.WorkspaceHandler, calDAVHandler http.Handler, authenticator httpauth.Authenticator, activeUserChecker httpauth.ActiveUserChecker, calDAVAuthenticator httpauth.CalDAVAuthenticator, adminChecker httpauth.AdminChecker, workspaceMembershipChecker httpauth.WorkspaceMembershipChecker) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(requestLogger(logger))
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", handlers.Health)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", authHandler.Login)
			r.Post("/refresh", authHandler.Refresh)
			r.Post("/logout", authHandler.Logout)
			// Register (ADR-0044) is public like the three routes above it: it
			// establishes a Session rather than requiring one, either
			// bootstrapping the very first account or self-registering a new
			// one while ENABLE_SIGNUPS is true.
			r.Post("/register", authHandler.Register)
			// Accept-invite (ADR-0042) is public like the three routes above
			// it: it establishes a Session rather than requiring one, proving
			// identity via a single-use token instead of a bearer token.
			r.Post("/accept-invite", authHandler.AcceptInvite)
			r.Get("/accept-invite", authHandler.PreviewInvite)

			// Workspace Invite accept (ADR-0044, #154): the preview and the
			// new-account path are public like accept-invite above; the
			// existing-account path instead requires the caller already be
			// logged in as the User whose email the invite names.
			r.Get("/accept-workspace-invite", authHandler.PreviewWorkspaceInvite)
			r.Post("/accept-workspace-invite", authHandler.AcceptWorkspaceInvite)
			r.With(httpauth.RequireAuth(authenticator)).Post("/accept-workspace-invite/join", authHandler.JoinWorkspaceInvite)

			r.With(httpauth.RequireAuth(authenticator)).Post("/change-password", authHandler.ChangePassword)

			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequireAuth(authenticator))
				r.Use(httpauth.RequireActiveUser(activeUserChecker))
				r.Get("/me", authHandler.Me)
				r.Put("/email", authHandler.UpdateEmail)
				r.Put("/username", authHandler.UpdateUsername)
				r.Put("/synced-device-reminders", authHandler.UpdateSyncedDeviceReminders)
				r.Patch("/preferences", authHandler.UpdatePreferences)
			})
		})

		r.Route("/calendars", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))

			// List/Create/Import/Subscribe create or read Calendars scoped to
			// the caller's active Workspace (#155, ADR-0045) — RequireWorkspace
			// runs after this group's RequireAuth/RequireActiveUser, which
			// .With() on each route already guarantees. Every other
			// /calendars/... route below operates on an already-existing
			// Calendar, which already carries its own workspace_id, so none of
			// them need this.
			r.With(httpauth.RequireWorkspace(workspaceMembershipChecker)).Get("/", calendarHandler.List)
			r.With(httpauth.RequireWorkspace(workspaceMembershipChecker)).Post("/", calendarHandler.Create)
			r.Get("/ics", calendarHandler.ICSAll)
			r.Get("/ics/oversized-attachments", calendarHandler.ICSAllOversizedAttachments)
			r.With(httpauth.RequireWorkspace(workspaceMembershipChecker)).Post("/import", calendarHandler.Import)
			r.With(httpauth.RequireWorkspace(workspaceMembershipChecker)).Post("/subscribe", calendarHandler.Subscribe)
			r.Get("/{id}", calendarHandler.Get)
			r.Patch("/{id}", calendarHandler.Update)
			r.Delete("/{id}", calendarHandler.Delete)
			r.Get("/{id}/ics", calendarHandler.ICS)
			r.Get("/{id}/ics/oversized-attachments", calendarHandler.ICSOversizedAttachments)
			r.Post("/{id}/refresh", calendarHandler.Refresh)

			// Sharing (ADR-0034): grant/revoke/list are Owner-only,
			// enforced by CalendarService rather than here; leave needs no
			// such check.
			r.Get("/{id}/shares", calendarHandler.ListShares)
			r.Post("/{id}/shares", calendarHandler.Share)
			r.Delete("/{id}/shares/{userId}", calendarHandler.RevokeShare)
			r.Post("/{id}/leave", calendarHandler.LeaveShare)
		})

		r.Route("/events", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))

			r.Get("/", eventHandler.List)
			r.Post("/", eventHandler.Create)
			r.Get("/{id}", eventHandler.Get)
			r.Patch("/{id}", eventHandler.Update)
			r.Delete("/{id}", eventHandler.Delete)
			r.Post("/{id}/exceptions", eventHandler.AddException)
			r.Post("/{id}/reparent", eventHandler.Reparent)
			r.Get("/{id}/ics", eventHandler.ICS)
			r.Get("/{id}/ics/oversized-attachments", eventHandler.ICSOversizedAttachments)

			// Per-User Reminder overrides (#105, ADR-0036): a personal
			// delivery preference, open to any Access level (Viewer
			// included), not gated by the write guard the rest of this
			// group's mutating routes enforce.
			r.Get("/{id}/reminder-override", eventHandler.GetReminderOverride)
			r.Put("/{id}/reminder-override", eventHandler.SetReminderOverride)
			r.Delete("/{id}/reminder-override", eventHandler.ClearReminderOverride)

			// Attachments (#132, ADR-0040): list rides along on the Event
			// itself (eventResponse.Attachments) rather than its own route.
			r.Post("/{id}/attachments", attachmentHandler.Upload)
			r.Get("/{id}/attachments/{attachmentId}", attachmentHandler.Download)
			r.Delete("/{id}/attachments/{attachmentId}", attachmentHandler.Delete)
		})

		r.Route("/notifications", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))

			r.Get("/", notificationHandler.List)
			r.Post("/seen", notificationHandler.MarkSeen)
		})

		r.Route("/app-passwords", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))

			r.Get("/", appPasswordHandler.List)
			r.Post("/", appPasswordHandler.Create)
			r.Delete("/{id}", appPasswordHandler.Revoke)
		})

		// User directory (#113): any authenticated caller may see who else
		// has an account, to pick a Share recipient — deliberately not the
		// Admin-only /accounts listing below, which carries status a
		// non-Admin has no business seeing.
		r.Route("/users", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))

			r.Get("/", userHandler.Directory)
		})

		// Workspace switcher (ADR-0044): every Workspace the caller belongs
		// to.
		r.Route("/workspaces", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))

			r.Get("/", workspaceHandler.List)
			// Invite issuance (ADR-0044, #154): WorkspaceService itself
			// refuses a caller who isn't the target Workspace's Owner or
			// Admin, so no extra middleware gate is needed here.
			r.Post("/{id}/invites", workspaceHandler.CreateInvite)
			r.Post("/invites/{id}/reissue", workspaceHandler.ReissueInvite)
		})

		// Account administration (ADR-0037): who exists, never what they can
		// see — RequireAdmin is the only authorization this group needs.
		r.Route("/accounts", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))
			r.Use(httpauth.RequireAdmin(adminChecker))

			r.Get("/", accountHandler.List)
			r.Post("/", accountHandler.Create)
			r.Post("/invite", accountHandler.CreateInvite)
			r.Post("/{id}/invite/reissue", accountHandler.ReissueInvite)
			r.Delete("/{id}/invite", accountHandler.CancelInvite)
			r.Post("/{id}/invite/email", accountHandler.SendInviteEmail)
			r.Post("/{id}/reset-password", accountHandler.ResetPassword)
			r.Put("/{id}/admin", accountHandler.SetAdmin)
			r.Put("/{id}/disabled", accountHandler.SetDisabled)
			r.Put("/{id}/username", accountHandler.SetUsername)
			r.Get("/{id}/username-impact", accountHandler.UsernameImpact)
			r.Get("/{id}/delete-impact", accountHandler.DeleteImpact)
			r.Delete("/{id}", accountHandler.Delete)
		})
	})

	// CalDAV (ADR-0023, ADR-0024): mounted ahead of the SPA catch-all below and
	// outside RequireAuth — it authenticates via HTTP Basic against hashed App
	// passwords instead of a bearer access token.
	r.Route("/dav", func(r chi.Router) {
		r.Use(httpauth.RequireCalDAVAuth(calDAVAuthenticator))
		r.Handle("/", calDAVHandler)
		r.Handle("/*", calDAVHandler)
	})
	r.With(httpauth.RequireCalDAVAuth(calDAVAuthenticator)).Handle("/.well-known/caldav", calDAVHandler)

	distFS, err := static.Dist()
	if err != nil {
		return nil, fmt.Errorf("load embedded frontend: %w", err)
	}

	spaHandler, err := spa.New(distFS)
	if err != nil {
		return nil, fmt.Errorf("build spa handler: %w", err)
	}
	r.Handle("/*", spaHandler)

	return r, nil
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration", time.Since(start),
			)
		})
	}
}
