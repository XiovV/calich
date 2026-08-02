package config

import "os"

type Config struct {
	Port            string
	DataDir         string
	InitialUsername string
	InitialPassword string
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8080"),
		DataDir:         getEnv("DATA_DIR", "/data"),
		InitialUsername: getEnv("INITIAL_USERNAME", ""),
		InitialPassword: getEnv("INITIAL_PASSWORD", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
