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

	"github.com/XiovV/calendar/server/internal/caldavserver"
	"github.com/XiovV/calendar/server/internal/config"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/handlers"
	"github.com/XiovV/calendar/server/internal/mailer"
	"github.com/XiovV/calendar/server/internal/reminder"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/router"
	"github.com/XiovV/calendar/server/internal/service"
)

// reminderTickInterval is how often the firing engine checks for due
// Reminders (ADR-0021). Reminder offsets are minute-granular, so a
// once-a-minute tick is fine-grained enough without being wasteful.
const reminderTickInterval = time.Minute

// subscriptionPollerTickInterval is how often the background poller checks
// for due Subscriptions (#86, ADR-0033) — independent of, and much finer
// than, any individual Subscription's own refresh cadence: this only
// governs how promptly a due Calendar is noticed, not how often it's
// actually refreshed.
const subscriptionPollerTickInterval = time.Minute

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
	calendarService := service.NewCalendarService(repository.NewCalendarRepository(sqlDB))
	eventService := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService)
	notificationRepo := repository.NewNotificationRepository(sqlDB)
	notificationService := service.NewNotificationService(notificationRepo)
	appPasswordService := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)

	ctx := context.Background()
	bootstrapUser, bootstrapCreatedUser, err := authService.Bootstrap(ctx)
	if err != nil {
		logger.Error("failed to bootstrap initial user", "error", err)
		os.Exit(1)
	}

	if bootstrapCreatedUser {
		if err := calendarService.EnsureDefaults(ctx, bootstrapUser.ID); err != nil {
			logger.Error("failed to seed default calendars", "error", err)
			os.Exit(1)
		}
	}

	importService := service.NewImportService(eventService, calendarService)
	subscribeService := service.NewSubscribeService(eventService, calendarService, cfg.SubscriptionRefreshInterval)

	authHandler := handlers.NewAuthHandler(authService, cfg.SMTPConfigured())
	calendarHandler := handlers.NewCalendarHandler(calendarService, eventService, importService, subscribeService)
	eventHandler := handlers.NewEventHandler(eventService)
	notificationHandler := handlers.NewNotificationHandler(notificationService)
	appPasswordHandler := handlers.NewAppPasswordHandler(appPasswordService)
	calDAVHandler := caldavserver.NewHTTPHandler(caldavserver.NewBackend(calendarService, eventService))

	handler, err := router.New(logger, authHandler, calendarHandler, eventHandler, notificationHandler, appPasswordHandler, calDAVHandler, authService, authService, appPasswordService)
	if err != nil {
		logger.Error("failed to build router", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: handler,
	}

	// The firing engine's scheduler (ADR-0021): a Notification-Channel
	// Reminder inserts a persistent Notification (#56); an Email-Channel
	// Reminder sends over SMTP (#57) once the self-hoster has configured it,
	// otherwise it falls back to the log sink.
	var emailDispatcher reminder.Dispatcher = reminder.LogDispatcher{}
	if cfg.SMTPConfigured() {
		smtpMailer := mailer.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom)
		emailDispatcher = reminder.EmailDispatcher{Users: users, Mailer: smtpMailer, Fallback: reminder.LogDispatcher{}}
	}
	dispatcher := reminder.NotificationDispatcher{Notifications: notificationRepo, Users: users, Fallback: emailDispatcher, Now: time.Now}
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	scheduler := reminder.NewScheduler(eventService, repository.NewFiredReminderRepository(sqlDB), dispatcher, time.Now)
	go scheduler.Run(schedulerCtx, reminderTickInterval)

	// The background poller that refreshes Subscribed Calendars on its own,
	// whether or not a browser is open (#86, ADR-0033).
	pollerCtx, stopPoller := context.WithCancel(context.Background())
	defer stopPoller()
	poller := service.NewPoller(calendarService, subscribeService, time.Now)
	go poller.Run(pollerCtx, subscriptionPollerTickInterval)

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
	stopScheduler()
	stopPoller()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
