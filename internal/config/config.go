package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DBHost               string
	DBPort               int
	DBName               string
	DBUser               string
	DBPassword           string
	DBSSLMode            string
	SlowQueryThresholdMs float64
	ServerPort           int
}

func Load() Config {
	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	serverPort, _ := strconv.Atoi(getEnv("SERVER_PORT", "8081"))
	threshold, _ := strconv.ParseFloat(getEnv("SLOW_QUERY_THRESHOLD_MS", "100"), 64)
	return Config{
		DBHost:               getEnv("DB_HOST", "localhost"),
		DBPort:               dbPort,
		DBName:               getEnv("DB_NAME", "postgres"),
		DBUser:               getEnv("DB_USER", "postgres"),
		DBPassword:           os.Getenv("DB_PASSWORD"),
		DBSSLMode:            getEnv("DB_SSL_MODE", "disable"),
		SlowQueryThresholdMs: threshold,
		ServerPort:           serverPort,
	}
}

func (c Config) Validate() error {
	if c.DBPort <= 0 || c.DBPort > 65535 {
		return fmt.Errorf("DB_PORT must be between 1 and 65535 (got %q)", os.Getenv("DB_PORT"))
	}
	if c.SlowQueryThresholdMs <= 0 {
		return fmt.Errorf("SLOW_QUERY_THRESHOLD_MS must be positive (got %q)", os.Getenv("SLOW_QUERY_THRESHOLD_MS"))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
