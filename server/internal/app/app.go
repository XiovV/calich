// Package app builds the server's object graph (#214, ADR-0065). It is the
// construction seam's upper half: every handler, the CalDAV backend, the
// mail transport and the background workers, assembled on top of the
// repositories and services service.NewGraph returns.
//
// Two adapters sit over the seam and nothing else does. New builds the
// graph over a real database, which is what cmd/server uses; NewInMemory
// builds the same graph over db.OpenInMemory, which is what a test outside
// this package's own dependency graph uses. Adding a repository is
// therefore a change to service.NewGraph and that repository's own package,
// and to no test file anywhere.
//
// What this package deliberately doesn't own is routing. router.New takes
// its handlers and its five narrow role interfaces as parameters, and keeps
// doing so: those interfaces are real seams with real adapters (AuthService
// satisfies three of them, AppPasswordService a fourth), so the App's job
// is to supply their arguments, not to absorb them. Leaving router out of
// this package's imports is also what lets router's own tests build an App.
package app

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/XiovV/calendar/server/internal/caldavserver"
	"github.com/XiovV/calendar/server/internal/config"
	"github.com/XiovV/calendar/server/internal/handlers"
	"github.com/XiovV/calendar/server/internal/mailer"
	"github.com/XiovV/calendar/server/internal/outbox"
	"github.com/XiovV/calendar/server/internal/reminder"
	"github.com/XiovV/calendar/server/internal/reply"
	"github.com/XiovV/calendar/server/internal/service"
)

// App is the whole server as a value: the repositories and services of the
// embedded Graph, plus everything built on top of them. Nothing here is
// optional except the two workers whose transports a self-hoster may not
// have configured.
type App struct {
	*service.Graph

	// Mailer is the SMTP transport Email-Channel Reminders (ADR-0021) and
	// Invitations (ADR-0059) both send over — nil when this deployment has
	// configured none, in which case Reminders fall back to the log sink and
	// no Invitation is ever queued.
	Mailer *mailer.SMTPMailer

	AuthHandler         *handlers.AuthHandler
	CalendarHandler     *handlers.CalendarHandler
	EventHandler        *handlers.EventHandler
	AttachmentHandler   *handlers.AttachmentHandler
	NotificationHandler *handlers.NotificationHandler
	AppPasswordHandler  *handlers.AppPasswordHandler
	AccountHandler      *handlers.AccountHandler
	UserHandler         *handlers.UserHandler
	WorkspaceHandler    *handlers.WorkspaceHandler
	GroupHandler        *handlers.GroupHandler

	CalDAVBackend *caldavserver.Backend
	CalDAVHandler http.Handler

	// The background workers. Each is built here and run by whoever owns the
	// process — cmd/server gives each its own context and tick interval.
	ReminderScheduler  *reminder.Scheduler
	SubscriptionPoller *service.Poller
	AttachmentSweeper  *service.AttachmentSweeper
	// OutboxWorker is nil when no SMTP transport is configured: there is
	// nothing queued to drain and nothing to send it with (ADR-0059,
	// ADR-0060).
	OutboxWorker *outbox.Worker
	// ReplyWorker is nil when no IMAP transport is configured. Its absence
	// never stops an Invitation going out — those Attendees simply stay
	// needs-action forever (ADR-0059).
	ReplyWorker *reply.Worker
}

// New builds the graph over sqlDB. The production adapter over the seam.
func New(sqlDB *sql.DB, cfg config.Config, opts ...service.GraphOption) (*App, error) {
	graph, err := service.NewGraph(sqlDB, cfg, opts...)
	if err != nil {
		return nil, err
	}
	return newFromGraph(graph, cfg), nil
}

// NewInMemory builds the same graph over a fresh in-memory database — the
// in-memory adapter over the seam. The caller owns the database and Closes
// the App when done.
func NewInMemory(cfg config.Config, opts ...service.GraphOption) (*App, error) {
	graph, err := service.NewInMemoryGraph(cfg, opts...)
	if err != nil {
		return nil, err
	}
	return newFromGraph(graph, cfg), nil
}

func newFromGraph(graph *service.Graph, cfg config.Config) *App {
	a := &App{Graph: graph}

	if cfg.SMTPConfigured() {
		a.Mailer = mailer.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom)
	}

	a.AuthHandler = handlers.NewAuthHandler(a.Auth, cfg.SMTPConfigured(), cfg.ImapConfigured())
	a.CalendarHandler = handlers.NewCalendarHandler(a.Calendars, a.Events, a.Imports, a.Subscriptions, a.AttachmentStore)
	a.EventHandler = handlers.NewEventHandler(a.Events, a.AttachmentStore)
	a.AttachmentHandler = handlers.NewAttachmentHandler(a.Attachments, cfg.MaxAttachmentSize)
	a.NotificationHandler = handlers.NewNotificationHandler(a.Notifications)
	a.AppPasswordHandler = handlers.NewAppPasswordHandler(a.AppPasswords)
	a.AccountHandler = handlers.NewAccountHandler(a.Accounts)
	a.UserHandler = handlers.NewUserHandler(a.Users)
	a.WorkspaceHandler = handlers.NewWorkspaceHandler(a.Workspaces)
	a.GroupHandler = handlers.NewGroupHandler(a.Groups)

	a.CalDAVBackend = caldavserver.NewBackend(a.Calendars, a.Events, a.Attachments, cfg.MaxAttachmentSize, cfg.MaxAttachmentsPerEvent)
	a.CalDAVHandler = caldavserver.NewHTTPHandler(a.CalDAVBackend)

	// The firing engine's dispatch chain (ADR-0021): a Notification-Channel
	// Reminder inserts a persistent Notification (#56); an Email-Channel one
	// sends over SMTP (#57) once the self-hoster has configured it, and
	// falls back to the log sink otherwise.
	var emailDispatcher reminder.Dispatcher = reminder.LogDispatcher{}
	if a.Mailer != nil {
		emailDispatcher = reminder.EmailDispatcher{Mailer: a.Mailer, Fallback: reminder.LogDispatcher{}}
	}
	dispatcher := reminder.NotificationDispatcher{Notifications: a.NotificationRepo, Fallback: emailDispatcher, Now: time.Now}
	a.ReminderScheduler = reminder.NewScheduler(a.Events, a.FiredReminderRepo, a.UserRepo, dispatcher, time.Now)

	a.SubscriptionPoller = service.NewPoller(a.Calendars, a.Subscriptions, time.Now)
	a.AttachmentSweeper = service.NewAttachmentSweeper(a.AttachmentRepo, a.AttachmentStore)

	if a.OutboxRepo != nil {
		a.OutboxWorker = outbox.NewWorker(a.OutboxRepo, service.NewInvitationSender(a.Events, a.Mailer, cfg.SMTPFrom), time.Now)
	}
	if cfg.ImapConfigured() {
		a.ReplyWorker = reply.NewWorker(reply.NewIMAPClient(cfg.IMAPHost, cfg.IMAPPort, cfg.IMAPUser, cfg.IMAPPass), a.Events)
	}

	return a
}
