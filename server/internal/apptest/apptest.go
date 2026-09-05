// Package apptest holds the config every test builds its graph from
// (#214, ADR-0065).
//
// It deliberately imports nothing but config and testing. The two in-memory
// constructors themselves — service.NewInMemoryGraph and app.NewInMemory —
// are ordinary code in the packages that own the build order, because Go
// refuses to compile an in-package test that imports a package importing
// the package under test, and internal/service and internal/handlers both
// test themselves in-package. Keeping this package free of those imports is
// what lets every test package, those two included, share one set of
// defaults.
package apptest

import (
	"testing"

	"github.com/XiovV/calich/server/internal/config"
)

// Config returns the settings a test's graph is built from: a Data
// directory scoped to the test, a bootstrap account, and limits generous
// enough that a test not exercising one never trips it. A test that needs a
// different value takes a copy and changes that one field.
//
// Deliberately not config.Load: that reads the environment and ../.env,
// which would make a test's behaviour depend on the machine running it.
func Config(t *testing.T) config.Config {
	t.Helper()

	return config.Config{
		Port:            "8080",
		DataDir:         t.TempDir(),
		InitialName:     "admin",
		InitialEmail:    "admin@example.com",
		InitialPassword: "admin",
		// Matching MAX_ATTACHMENT_SIZE and MAX_ATTACHMENTS_PER_EVENT's own
		// defaults (ADR-0040) — arbitrary but generous, since most tests
		// aren't exercising the limits themselves.
		MaxAttachmentSize:      25 << 20,
		MaxAttachmentsPerEvent: 10,
		// Well above defaultInviteRateLimitPerHour (ADR-0058), so only a test
		// that lowers it deliberately ever sees the cap.
		InviteRateLimitPerHour: 1000,
		// Well above defaultAuthRateLimitPerEmail/PerIP/defaultRegisterRateLimitPerIP
		// (#240, ADR-0070), so only a test that lowers one deliberately ever
		// sees the cap — every handler test in this repo logs in or
		// registers repeatedly from the same loopback address.
		AuthRateLimitPerEmail:  1000,
		AuthRateLimitPerIP:     1000,
		RegisterRateLimitPerIP: 1000,
		// No cadence of its own: a test that refreshes a Subscription does so
		// explicitly rather than waiting for one to come due (ADR-0033).
		SubscriptionRefreshInterval: 0,
		EnableSignups:               false,
		CookieSecure:                true,
	}
}

// SMTPConfig is Config with an SMTP transport, which is what makes
// config.SMTPConfigured true and so gives the graph an OutboxRepo to queue
// Invitations into (ADR-0059, ADR-0060). Nothing is ever sent: no test
// drains the outbox over a real connection, they assert on what was queued.
func SMTPConfig(t *testing.T) config.Config {
	t.Helper()

	cfg := Config(t)
	cfg.SMTPHost = "smtp.example.com"
	cfg.SMTPPort = "587"
	cfg.SMTPUser = "calendar"
	cfg.SMTPPass = "hunter2"
	cfg.SMTPFrom = "calendar@example.com"
	return cfg
}

// GoogleConfig is Config with Google OAuth credentials and a Connections
// encryption key, which is what makes config.GoogleConfigured true (#285,
// ADR-0051) — the Provider a test exercising Connect/Callback needs present.
func GoogleConfig(t *testing.T) config.Config {
	t.Helper()

	cfg := Config(t)
	cfg.GoogleClientID = "test-client-id.apps.googleusercontent.com"
	cfg.GoogleClientSecret = "test-client-secret"
	cfg.ConnectionsEncryptionKey = "test-connections-encryption-key"
	return cfg
}
