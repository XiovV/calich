// graph.go is the construction seam's lower half (#214, ADR-0065): the one
// place that knows which repository every service is built from, and in
// what order. Everything above it — handlers, the CalDAV backend, the
// background workers — is assembled by internal/app on top of the Graph
// this file returns.
//
// The split is forced rather than chosen. This package's own tests are
// in-package (they reach unexported helpers like pickFreeColor and
// calendarSwatches), and Go refuses to compile an in-package test that
// imports anything importing the package under test. A builder living in
// internal/app would therefore be unreachable from the 25 test files here
// that need a wired-up service, which is exactly the duplication the seam
// exists to remove. Putting the repository-and-service half of the build
// order in this package keeps it reachable from everywhere: from
// internal/app above it, and from this package's own tests.
package service

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/XiovV/calich/server/internal/attachmentstore"
	"github.com/XiovV/calich/server/internal/config"
	"github.com/XiovV/calich/server/internal/db"
	"github.com/XiovV/calich/server/internal/repository"
)

// Graph is every repository and service this server runs on, built once
// from a database handle and a config. Fields are named for what they are
// rather than for the constructor that made them: a repository carries a
// Repo suffix, a service carries none, so Events is the EventService and
// EventRepo the row store underneath it.
type Graph struct {
	// DB is the handle every repository below was built from, and the one
	// the transaction-spanning services (Workspaces, Events, Accounts,
	// Calendars) take directly.
	DB     *sql.DB
	Config config.Config
	// JWTSecret is what AuthService signs Access tokens with — minted fresh
	// per build unless WithJWTSecret pins it, so a restart invalidates every
	// outstanding token, and a test that needs to hand-craft one can read
	// the secret back off the Graph.
	JWTSecret       []byte
	AttachmentStore *attachmentstore.Store

	UserRepo             *repository.UserRepository
	SessionRepo          *repository.SessionRepository
	CalendarRepo         *repository.CalendarRepository
	ShareRepo            *repository.CalendarShareRepository
	GroupShareRepo       *repository.CalendarGroupShareRepository
	ColorOverrideRepo    *repository.CalendarUserColorRepository
	DefaultReminderRepo  *repository.CalendarDefaultReminderRepository
	EventRepo            *repository.EventRepository
	EventExceptionRepo   *repository.EventExceptionRepository
	EventReminderRepo    *repository.EventReminderRepository
	ExplicitReminderRepo *repository.EventReminderExplicitRepository
	SyncRepo             *repository.SyncRepository
	AttachmentRepo       *repository.AttachmentRepository
	AttendeeRepo         *repository.AttendeeRepository
	WorkspaceRepo        *repository.WorkspaceRepository
	WorkspaceInviteRepo  *repository.WorkspaceInviteRepository
	GroupRepo            *repository.GroupRepository
	NotificationRepo     *repository.NotificationRepository
	AppPasswordRepo      *repository.AppPasswordRepository
	FiredReminderRepo    *repository.FiredReminderRepository
	// OutboxRepo is nil on a deployment with no SMTP transport configured
	// (ADR-0059, ADR-0060): with nothing able to send an Invitation there is
	// nothing to queue one into, and EventService takes the nil as "queue
	// no Invitation at all".
	OutboxRepo *repository.OutboxRepository
	// RateLimitRepo backs RateLimiter (#240, ADR-0070) — built
	// unconditionally, unlike OutboxRepo, since throttling Login/Register/
	// CalDAV needs no self-hoster configuration to be worth having.
	RateLimitRepo *repository.RateLimitAttemptRepository

	Auth          *AuthService
	Accounts      *AccountService
	AppPasswords  *AppPasswordService
	Attachments   *AttachmentService
	Calendars     *CalendarService
	Events        *EventService
	Groups        *GroupService
	Imports       *ImportService
	Notifications *NotificationService
	Subscriptions *SubscribeService
	Users         *UserService
	Workspaces    *WorkspaceService
	// RateLimiter throttles Login, Register, and CalDAV Basic auth (#240,
	// ADR-0070) — held by the Graph rather than by AuthService/
	// AppPasswordService, since it's enforced from the HTTP layer
	// (AuthHandler, httpauth.RequireCalDAVAuth) rather than from inside
	// either service.
	RateLimiter *AuthRateLimiter
}

// GraphOption is a build input that isn't part of config.Config, because it
// isn't something a self-hoster sets. There are only two, both of them a
// value production mints for itself and a test needs to pin.
type GraphOption func(*graphOptions)

type graphOptions struct {
	jwtSecret           []byte
	subscribeHTTPClient *http.Client
}

// WithJWTSecret pins the secret AuthService signs Access tokens with,
// instead of the random one NewGraph mints. Only a test that hand-crafts a
// token — an expired one, say — needs this.
func WithJWTSecret(secret []byte) GraphOption {
	return func(o *graphOptions) { o.jwtSecret = secret }
}

// WithSubscribeHTTPClient replaces the client SubscribeService fetches
// feeds with. The default refuses private, loopback and link-local
// addresses (#97, ADR-0032), which is also what a test serving a feed from
// an httptest.Server on loopback trips over — such a test passes a plain
// client here, and a test of the guard itself keeps the default.
func WithSubscribeHTTPClient(client *http.Client) GraphOption {
	return func(o *graphOptions) { o.subscribeHTTPClient = client }
}

// NewGraph builds every repository and service from a database handle and a
// config. This is the production adapter over the seam; NewInMemoryGraph is
// the other one.
func NewGraph(sqlDB *sql.DB, cfg config.Config, opts ...GraphOption) (*Graph, error) {
	var built graphOptions
	for _, opt := range opts {
		opt(&built)
	}

	if built.jwtSecret == nil {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate JWT signing secret: %w", err)
		}
		built.jwtSecret = secret
	}

	g := &Graph{
		DB:              sqlDB,
		Config:          cfg,
		JWTSecret:       built.jwtSecret,
		AttachmentStore: attachmentstore.New(cfg.DataDir),

		UserRepo:             repository.NewUserRepository(sqlDB),
		SessionRepo:          repository.NewSessionRepository(sqlDB),
		CalendarRepo:         repository.NewCalendarRepository(sqlDB),
		ShareRepo:            repository.NewCalendarShareRepository(sqlDB),
		GroupShareRepo:       repository.NewCalendarGroupShareRepository(sqlDB),
		ColorOverrideRepo:    repository.NewCalendarUserColorRepository(sqlDB),
		DefaultReminderRepo:  repository.NewCalendarDefaultReminderRepository(sqlDB),
		EventRepo:            repository.NewEventRepository(sqlDB),
		EventExceptionRepo:   repository.NewEventExceptionRepository(sqlDB),
		EventReminderRepo:    repository.NewEventReminderRepository(sqlDB),
		ExplicitReminderRepo: repository.NewEventReminderExplicitRepository(sqlDB),
		SyncRepo:             repository.NewSyncRepository(sqlDB),
		AttachmentRepo:       repository.NewAttachmentRepository(sqlDB),
		AttendeeRepo:         repository.NewAttendeeRepository(sqlDB),
		WorkspaceRepo:        repository.NewWorkspaceRepository(sqlDB),
		WorkspaceInviteRepo:  repository.NewWorkspaceInviteRepository(sqlDB),
		GroupRepo:            repository.NewGroupRepository(sqlDB),
		NotificationRepo:     repository.NewNotificationRepository(sqlDB),
		AppPasswordRepo:      repository.NewAppPasswordRepository(sqlDB),
		FiredReminderRepo:    repository.NewFiredReminderRepository(sqlDB),
	}
	if cfg.SMTPConfigured() {
		g.OutboxRepo = repository.NewOutboxRepository(sqlDB)
	}
	g.RateLimitRepo = repository.NewRateLimitAttemptRepository(sqlDB)

	g.RateLimiter = NewAuthRateLimiter(g.RateLimitRepo, cfg.AuthRateLimitPerEmail, cfg.AuthRateLimitPerIP, cfg.RegisterRateLimitPerIP)
	g.Workspaces = NewWorkspaceService(sqlDB, g.WorkspaceRepo, g.WorkspaceInviteRepo, g.CalendarRepo, g.ShareRepo)
	g.Groups = NewGroupService(g.GroupRepo, g.WorkspaceRepo)
	g.Calendars = NewCalendarService(sqlDB, g.CalendarRepo, g.ShareRepo, g.UserRepo, g.EventReminderRepo, g.DefaultReminderRepo, g.ExplicitReminderRepo, g.ColorOverrideRepo, g.WorkspaceRepo, g.GroupShareRepo, g.GroupRepo)
	g.Auth = NewAuthService(g.UserRepo, g.SessionRepo, g.Workspaces, g.WorkspaceInviteRepo, g.Calendars, g.AttendeeRepo, g.JWTSecret, cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword, cfg.EnableSignups)
	g.Events = NewEventService(sqlDB, g.EventRepo, g.EventExceptionRepo, g.EventReminderRepo, g.DefaultReminderRepo, g.ExplicitReminderRepo, g.SyncRepo, g.Calendars, g.UserRepo, g.AttachmentRepo, g.AttendeeRepo, g.WorkspaceRepo, g.GroupRepo, g.NotificationRepo, g.OutboxRepo, cfg.InviteRateLimitPerHour)
	g.Attachments = NewAttachmentService(g.AttachmentRepo, g.EventRepo, g.Calendars, g.Events, g.AttachmentStore, cfg.MaxAttachmentsPerEvent)
	g.Accounts = NewAccountService(sqlDB, g.UserRepo, g.SessionRepo, g.CalendarRepo, g.ShareRepo, g.WorkspaceRepo, g.Workspaces)
	g.AppPasswords = NewAppPasswordService(g.AppPasswordRepo, g.UserRepo)
	g.Notifications = NewNotificationService(g.NotificationRepo)
	g.Users = NewUserService(g.UserRepo)
	g.Imports = NewImportService(g.Events, g.Calendars, g.AttachmentStore, cfg.MaxAttachmentSize, cfg.MaxAttachmentsPerEvent)

	var subscribeOpts []SubscribeOption
	if built.subscribeHTTPClient != nil {
		subscribeOpts = append(subscribeOpts, WithHTTPClient(built.subscribeHTTPClient))
	}
	g.Subscriptions = NewSubscribeService(g.Events, g.Calendars, cfg.SubscriptionRefreshInterval, subscribeOpts...)

	return g, nil
}

// NewInMemoryGraph is NewGraph over a fresh in-memory database with every
// migration applied — the second adapter over the seam, and the one every
// test builds its services from. Ordinary code rather than a test helper on
// purpose: a package's own in-package tests can't import a package that
// imports them, so the in-memory adapter has to sit where the production
// one does.
//
// The caller owns the database: Close it (a test, via t.Cleanup) when done.
func NewInMemoryGraph(cfg config.Config, opts ...GraphOption) (*Graph, error) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		return nil, fmt.Errorf("open in-memory database: %w", err)
	}

	g, err := NewGraph(sqlDB, cfg, opts...)
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	return g, nil
}

// Close releases the database the Graph was built on.
func (g *Graph) Close() error {
	return g.DB.Close()
}
