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

	"github.com/XiovV/calendar/server/internal/attachmentstore"
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

// attachmentSweepInterval is how often the Attachment sweeper reclaims
// orphaned files on top of the startup sweep Run always does first
// (#132, ADR-0040) — daily, since an orphan is only wasted disk, not a
// correctness problem, so there is no reason to chase it more eagerly.
const attachmentSweepInterval = 24 * time.Hour

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
	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	reminderOverrideRepo := repository.NewReminderOverrideRepository(sqlDB)
	colorOverrideRepo := repository.NewCalendarUserColorRepository(sqlDB)
	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspaceInviteRepo := repository.NewWorkspaceInviteRepository(sqlDB)
	workspaceService := service.NewWorkspaceService(sqlDB, workspaceRepo, workspaceInviteRepo, calendarRepo, shareRepo)
	groupShareRepo := repository.NewCalendarGroupShareRepository(sqlDB)
	groupRepo := repository.NewGroupRepository(sqlDB)
	groupService := service.NewGroupService(groupRepo, workspaceRepo)
	calendarService := service.NewCalendarService(calendarRepo, shareRepo, users, reminderOverrideRepo, colorOverrideRepo, workspaceRepo, groupShareRepo, groupRepo)
	authService := service.NewAuthService(users, sessions, workspaceService, workspaceInviteRepo, calendarService, jwtSecret, cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword, cfg.EnableSignups)
	attachmentRepo := repository.NewAttachmentRepository(sqlDB)
	eventRepo := repository.NewEventRepository(sqlDB)
	attendeeRepo := repository.NewAttendeeRepository(sqlDB)
	eventService := service.NewEventService(sqlDB, eventRepo, repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), reminderOverrideRepo, repository.NewSyncRepository(sqlDB), calendarService, users, attachmentRepo, attendeeRepo, workspaceRepo, groupRepo)
	attachmentStore := attachmentstore.New(cfg.DataDir)
	attachmentService := service.NewAttachmentService(attachmentRepo, eventRepo, calendarService, eventService, attachmentStore, cfg.MaxAttachmentsPerEvent)
	notificationRepo := repository.NewNotificationRepository(sqlDB)
	notificationService := service.NewNotificationService(notificationRepo)
	appPasswordService := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	// smtpMailer serves Reminder email delivery (ADR-0021) — nil, and
	// Reminders fall back to the log sink, when this deployment has no SMTP
	// transport configured.
	var smtpMailer *mailer.SMTPMailer
	if cfg.SMTPConfigured() {
		smtpMailer = mailer.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom)
	}
	accountService := service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, workspaceRepo, workspaceService)

	ctx := context.Background()
	bootstrapUser, bootstrapCreatedUser, err := authService.Bootstrap(ctx)
	if err != nil {
		logger.Error("failed to bootstrap initial user", "error", err)
		os.Exit(1)
	}

	if bootstrapCreatedUser {
		// Bootstrap just created bootstrapUser's own Workspace (AuthService.
		// Bootstrap) — resolve it so the seeded default Calendars land inside
		// it rather than nowhere (#155, ADR-0045).
		bootstrapWorkspaces, err := workspaceService.ListForUser(ctx, bootstrapUser.ID)
		if err != nil {
			logger.Error("failed to look up bootstrap workspace", "error", err)
			os.Exit(1)
		}
		if err := calendarService.EnsureDefaults(ctx, bootstrapUser.ID, bootstrapWorkspaces[0].ID); err != nil {
			logger.Error("failed to seed default calendars", "error", err)
			os.Exit(1)
		}
	}

	importService := service.NewImportService(eventService, calendarService, attachmentStore, cfg.MaxAttachmentSize, cfg.MaxAttachmentsPerEvent)
	subscribeService := service.NewSubscribeService(eventService, calendarService, cfg.SubscriptionRefreshInterval)

	authHandler := handlers.NewAuthHandler(authService, cfg.SMTPConfigured())
	calendarHandler := handlers.NewCalendarHandler(calendarService, eventService, importService, subscribeService, attachmentStore)
	eventHandler := handlers.NewEventHandler(eventService, attachmentStore)
	attachmentHandler := handlers.NewAttachmentHandler(attachmentService, cfg.MaxAttachmentSize)
	notificationHandler := handlers.NewNotificationHandler(notificationService)
	appPasswordHandler := handlers.NewAppPasswordHandler(appPasswordService)
	accountHandler := handlers.NewAccountHandler(accountService)
	userService := service.NewUserService(users)
	userHandler := handlers.NewUserHandler(userService)
	workspaceHandler := handlers.NewWorkspaceHandler(workspaceService)
	groupHandler := handlers.NewGroupHandler(groupService)
	calDAVHandler := caldavserver.NewHTTPHandler(caldavserver.NewBackend(calendarService, eventService, attachmentService, cfg.MaxAttachmentSize, cfg.MaxAttachmentsPerEvent))

	handler, err := router.New(logger, authHandler, calendarHandler, eventHandler, attachmentHandler, notificationHandler, appPasswordHandler, accountHandler, userHandler, workspaceHandler, groupHandler, calDAVHandler, authService, authService, appPasswordService, authService, workspaceService)
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
	if smtpMailer != nil {
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

	// The Attachment sweeper: reclaims files with no matching row, at
	// startup and daily thereafter (#132, ADR-0040).
	sweeperCtx, stopSweeper := context.WithCancel(context.Background())
	defer stopSweeper()
	sweeper := service.NewAttachmentSweeper(attachmentRepo, attachmentStore)
	go sweeper.Run(sweeperCtx, attachmentSweepInterval)

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
	stopSweeper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
