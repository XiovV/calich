package config

import "os"

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
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8080"),
		DataDir:         getEnv("DATA_DIR", "/data"),
		InitialUsername: getEnv("INITIAL_USERNAME", ""),
		InitialPassword: getEnv("INITIAL_PASSWORD", ""),
		SMTPHost:        getEnv("SMTP_HOST", ""),
		SMTPPort:        getEnv("SMTP_PORT", ""),
		SMTPUser:        getEnv("SMTP_USER", ""),
		SMTPPass:        getEnv("SMTP_PASS", ""),
		SMTPFrom:        getEnv("SMTP_FROM", ""),
	}
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
