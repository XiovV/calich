package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/XiovV/calich/server/internal/app"
	"github.com/XiovV/calich/server/internal/config"
	"github.com/XiovV/calich/server/internal/db"
	"github.com/XiovV/calich/server/internal/router"
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

// outboxTickInterval is how often the outbox Worker checks for queued
// Invitations (ADR-0059, ADR-0060) — much finer than the reminder
// scheduler's minute, since an Invitation is the one message a User cannot
// turn off and a self-hoster would notice a slow one.
const outboxTickInterval = 10 * time.Second

// replyTickInterval is how often the reply poller checks the ORGANIZER
// mailbox for unseen mail (ADR-0059) — coarser than the outbox's own
// interval since a Response arriving "one poller cycle late" is an accepted
// cost (ADR-0059), not a promise of anything approaching real time, and a
// self-hoster's IMAP provider is one more thing not worth polling harder
// than necessary.
const replyTickInterval = time.Minute

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	sqlDB, err := db.Open(cfg.DataDir)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	// The whole object graph, built in one place (#214, ADR-0065).
	a, err := app.New(sqlDB, cfg)
	if err != nil {
		logger.Error("failed to build application", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	bootstrapUser, bootstrapCreatedUser, err := a.Auth.Bootstrap(ctx)
	if err != nil {
		logger.Error("failed to bootstrap initial user", "error", err)
		os.Exit(1)
	}

	if bootstrapCreatedUser {
		// Bootstrap just created bootstrapUser's own Workspace (AuthService.
		// Bootstrap) — resolve it so the seeded default Calendars land inside
		// it rather than nowhere (#155, ADR-0045).
		bootstrapWorkspaces, err := a.Workspaces.ListForUser(ctx, bootstrapUser.ID)
		if err != nil {
			logger.Error("failed to look up bootstrap workspace", "error", err)
			os.Exit(1)
		}
		if err := a.Calendars.EnsureDefaults(ctx, bootstrapUser.ID, bootstrapWorkspaces[0].ID); err != nil {
			logger.Error("failed to seed default calendars", "error", err)
			os.Exit(1)
		}
	}

	handler, err := router.New(logger, a.AuthHandler, a.CalendarHandler, a.EventHandler, a.AttachmentHandler, a.NotificationHandler, a.AppPasswordHandler, a.AccountHandler, a.UserHandler, a.WorkspaceHandler, a.GroupHandler, a.CalDAVHandler, a.Auth, a.Auth, a.AppPasswords, a.Auth, a.Workspaces)
	if err != nil {
		logger.Error("failed to build router", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: handler,
	}

	// The firing engine's scheduler (ADR-0021).
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	go a.ReminderScheduler.Run(schedulerCtx, reminderTickInterval)

	// The background poller that refreshes Subscribed Calendars on its own,
	// whether or not a browser is open (#86, ADR-0033).
	pollerCtx, stopPoller := context.WithCancel(context.Background())
	defer stopPoller()
	go a.SubscriptionPoller.Run(pollerCtx, subscriptionPollerTickInterval)

	// The Attachment sweeper: reclaims files with no matching row, at
	// startup and daily thereafter (#132, ADR-0040).
	sweeperCtx, stopSweeper := context.WithCancel(context.Background())
	defer stopSweeper()
	go a.AttachmentSweeper.Run(sweeperCtx, attachmentSweepInterval)

	// The outbox Worker: drains queued Invitations (ADR-0059, ADR-0060).
	// Absent entirely when no SMTP is configured — there is nothing queued
	// to drain, and nothing to send it with.
	outboxCtx, stopOutbox := context.WithCancel(context.Background())
	defer stopOutbox()
	if a.OutboxWorker != nil {
		go a.OutboxWorker.Run(outboxCtx, outboxTickInterval)
	}

	// The reply poller: reads Attendee Responses back from the ORGANIZER
	// mailbox (ADR-0059). Absent entirely when IMAP isn't configured — its
	// absence never disables sending Invitations, those Attendees just stay
	// needs-action forever.
	replyCtx, stopReply := context.WithCancel(context.Background())
	defer stopReply()
	if a.ReplyWorker != nil {
		go a.ReplyWorker.Run(replyCtx, replyTickInterval)
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
	stopScheduler()
	stopPoller()
	stopSweeper()
	stopOutbox()
	stopReply()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
