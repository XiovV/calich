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

func New(logger *slog.Logger, authHandler *handlers.AuthHandler, calendarHandler *handlers.CalendarHandler, eventHandler *handlers.EventHandler, notificationHandler *handlers.NotificationHandler, appPasswordHandler *handlers.AppPasswordHandler, calDAVHandler http.Handler, authenticator httpauth.Authenticator, activeUserChecker httpauth.ActiveUserChecker, calDAVAuthenticator httpauth.CalDAVAuthenticator) (http.Handler, error) {
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
				r.Put("/synced-device-reminders", authHandler.UpdateSyncedDeviceReminders)
			})
		})

		r.Route("/calendars", func(r chi.Router) {
			r.Use(httpauth.RequireAuth(authenticator))
			r.Use(httpauth.RequireActiveUser(activeUserChecker))

			r.Get("/", calendarHandler.List)
			r.Post("/", calendarHandler.Create)
			r.Get("/{id}", calendarHandler.Get)
			r.Patch("/{id}", calendarHandler.Update)
			r.Delete("/{id}", calendarHandler.Delete)
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
