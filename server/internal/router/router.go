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

func New(logger *slog.Logger, authHandler *handlers.AuthHandler, authenticator httpauth.Authenticator, activeUserChecker httpauth.ActiveUserChecker) (http.Handler, error) {
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
			})
		})
	})

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
