package main

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/XiovV/calendar/server/internal/config"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/handlers"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/router"
	"github.com/XiovV/calendar/server/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	sqlDB, err := db.Open(cfg.DataDir)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	jwtSecret := make([]byte, 32)
	if _, err := rand.Read(jwtSecret); err != nil {
		logger.Error("failed to generate JWT signing secret", "error", err)
		os.Exit(1)
	}

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	authService := service.NewAuthService(users, sessions, jwtSecret, cfg.InitialUsername, cfg.InitialPassword)

	ctx := context.Background()
	if err := authService.Bootstrap(ctx); err != nil {
		logger.Error("failed to bootstrap initial user", "error", err)
		os.Exit(1)
	}

	authHandler := handlers.NewAuthHandler(authService)

	handler, err := router.New(logger, authHandler, authService)
	if err != nil {
		logger.Error("failed to build router", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: handler,
	}

	go func() {
		logger.Info("starting server", "port", cfg.Port, "data_dir", cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
