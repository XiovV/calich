package config

import "os"

type Config struct {
	Port    string
	DataDir string
}

func Load() Config {
	return Config{
		Port:    getEnv("PORT", "8080"),
		DataDir: getEnv("DATA_DIR", "/data"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
