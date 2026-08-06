package config

import (
	"log/slog"
	"os"
	"time"
)

// defaultSubscriptionRefreshInterval is how often a Subscription is
// refreshed when its feed states no cadence of its own, and
// SUBSCRIPTION_REFRESH_INTERVAL is unset (#86, ADR-0033).
const defaultSubscriptionRefreshInterval = time.Hour

type Config struct {
	Port            string
	DataDir         string
	InitialUsername string
	InitialPassword string
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
}

func Load() Config {
	return Config{
		Port:                        getEnv("PORT", "8080"),
		DataDir:                     getEnv("DATA_DIR", "/data"),
		InitialUsername:             getEnv("INITIAL_USERNAME", ""),
		InitialPassword:             getEnv("INITIAL_PASSWORD", ""),
		SMTPHost:                    getEnv("SMTP_HOST", ""),
		SMTPPort:                    getEnv("SMTP_PORT", ""),
		SMTPUser:                    getEnv("SMTP_USER", ""),
		SMTPPass:                    getEnv("SMTP_PASS", ""),
		SMTPFrom:                    getEnv("SMTP_FROM", ""),
		SubscriptionRefreshInterval: getEnvDuration("SUBSCRIPTION_REFRESH_INTERVAL", defaultSubscriptionRefreshInterval),
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

// SMTPConfigured reports whether every SMTP setting needed to send mail is
// present — the Email Channel is only offered/dispatched when this is true
// (ADR-0021).
func (c Config) SMTPConfigured() bool {
	return c.SMTPHost != "" && c.SMTPPort != "" && c.SMTPUser != "" && c.SMTPPass != "" && c.SMTPFrom != ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
