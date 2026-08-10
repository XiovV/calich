package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// defaultSubscriptionRefreshInterval is how often a Subscription is
// refreshed when its feed states no cadence of its own, and
// SUBSCRIPTION_REFRESH_INTERVAL is unset (#86, ADR-0033).
const defaultSubscriptionRefreshInterval = time.Hour

// defaultMaxAttachmentSize and defaultMaxAttachmentsPerEvent are Attachments'
// limits (#132, ADR-0040) when MAX_ATTACHMENT_SIZE/MAX_ATTACHMENTS_PER_EVENT
// are unset. Env-configurable rather than a hardcoded const like
// maxImportUploadBytes, because disk is the one resource that varies wildly
// between self-hosters and is already the thing they configure.
const (
	defaultMaxAttachmentSize      int64 = 25 << 20
	defaultMaxAttachmentsPerEvent       = 10
)

type Config struct {
	Port        string
	DataDir     string
	InitialName string
	// InitialEmail is the bootstrap account's login identifier (ADR-0047) —
	// required, alongside InitialPassword, for Bootstrap to create the first
	// account.
	InitialEmail    string
	InitialPassword string
	// MaxAttachmentSize and MaxAttachmentsPerEvent are Attachments' limits
	// (#132, ADR-0040).
	MaxAttachmentSize      int64
	MaxAttachmentsPerEvent int
	// SMTP transport for Email-Channel Reminders (ADR-0021). Email delivery
	// is only wired up when every one of these is set.
	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
	SMTPFrom string
	// SubscriptionRefreshInterval is the background poller's default
	// refresh cadence for a Subscribed Calendar whose feed states none of
	// its own — SUBSCRIPTION_REFRESH_INTERVAL, parsed as a Go duration
	// (e.g. "1h", "30m"), or defaultSubscriptionRefreshInterval when unset
	// or unparseable (#86, ADR-0033).
	SubscriptionRefreshInterval time.Duration
	// EnableSignups gates self-registration (ADR-0044): when false (the
	// default), a registration attempt is rejected for everyone except the
	// very first account on the instance, which Bootstrap or a first-run
	// Register call always creates regardless of this setting.
	EnableSignups bool
}

func Load() Config {
	return Config{
		Port:                        getEnv("PORT", "8080"),
		DataDir:                     getEnv("DATA_DIR", "/data"),
		InitialName:                 getEnv("INITIAL_NAME", "Admin"),
		InitialEmail:                getEnv("INITIAL_EMAIL", ""),
		InitialPassword:             getEnv("INITIAL_PASSWORD", ""),
		SMTPHost:                    getEnv("SMTP_HOST", ""),
		SMTPPort:                    getEnv("SMTP_PORT", ""),
		SMTPUser:                    getEnv("SMTP_USER", ""),
		SMTPPass:                    getEnv("SMTP_PASS", ""),
		SMTPFrom:                    getEnv("SMTP_FROM", ""),
		SubscriptionRefreshInterval: getEnvDuration("SUBSCRIPTION_REFRESH_INTERVAL", defaultSubscriptionRefreshInterval),
		MaxAttachmentSize:           getEnvInt64("MAX_ATTACHMENT_SIZE", defaultMaxAttachmentSize),
		MaxAttachmentsPerEvent:      getEnvInt("MAX_ATTACHMENTS_PER_EVENT", defaultMaxAttachmentsPerEvent),
		EnableSignups:               getEnvBool("ENABLE_SIGNUPS", false),
	}
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid duration env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return d
}

// getEnvInt64 parses key as a base-10 integer (e.g. a byte count), falling
// back to fallback when unset or unparseable.
func getEnvInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		slog.Warn("invalid integer env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return n
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid integer env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return n
}

// SMTPConfigured reports whether every SMTP setting needed to send mail is
// present — the Email Channel is only offered/dispatched when this is true
// (ADR-0021).
func (c Config) SMTPConfigured() bool {
	return c.SMTPHost != "" && c.SMTPPort != "" && c.SMTPUser != "" && c.SMTPPass != "" && c.SMTPFrom != ""
}

// getEnvBool parses key as a bool (e.g. "true"/"false", "1"/"0"), falling
// back to fallback when unset or unparseable.
func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		slog.Warn("invalid boolean env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return b
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
