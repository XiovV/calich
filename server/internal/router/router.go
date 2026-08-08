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

func New(logger *slog.Logger, authHandler *handlers.AuthHandler, calendarHandler *handlers.CalendarHandler, eventHandler *handlers.EventHandler, notificationHandler *handlers.NotificationHandler, appPasswordHandler *handlers.AppPasswordHandler, accountHandler *handlers.AccountHandler, userHandler *handlers.UserHandler, calDAVHandler http.Handler, authenticator httpauth.Authenticator, activeUserChecker httpauth.ActiveUserChecker, calDAVAuthenticator httpauth.CalDAVAuthenticator, adminChecker httpauth.AdminChecker) (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(requestLogger(logger))
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", handlers.Health)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", authHandler.Login)
			r.Post("/refresh", authHandler.Refresh)
			r.Post("/logout", authHandler.Logout)

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

			r.Get("/", calendarHandler.List)
			r.Post("/", calendarHandler.Create)
			r.Get("/ics", calendarHandler.ICSAll)
			r.Post("/import", calendarHandler.Import)
			r.Post("/subscribe", calendarHandler.Subscribe)
			r.Get("/{id}", calendarHandler.Get)
			r.Patch("/{id}", calendarHandler.Update)
			r.Delete("/{id}", calendarHandler.Delete)
			r.Get("/{id}/ics", calendarHandler.ICS)
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

			// Per-User Reminder overrides (#105, ADR-0036): a personal
			// delivery preference, open to any Access level (Viewer
			// included), not gated by the write guard the rest of this
			// group's mutating routes enforce.
			r.Get("/{id}/reminder-override", eventHandler.GetReminderOverride)
			r.Put("/{id}/reminder-override", eventHandler.SetReminderOverride)
			r.Delete("/{id}/reminder-override", eventHandler.ClearReminderOverride)
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

		// Account administration (ADR-0037): who exists, never what they can
		// see — RequireAdmin is the only authorization this group needs.
		r.Route("/accounts", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))
			r.Use(httpauth.RequireAdmin(adminChecker))

			r.Get("/", accountHandler.List)
			r.Post("/", accountHandler.Create)
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
