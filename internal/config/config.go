package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppPort string

	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBDSN      string

	JWTSecret      string
	JWTTTLMinutes  int
	RefreshTTLDays int

	OddsAPIKey string
	Bookmaker  string
	Sports     []string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	SignupBonus int64
	BetMin      int64
	BetMax      int64
}

func Load() (*Config, error) {
	cfg := &Config{
		AppPort: getEnv("APP_PORT", "8080"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnvInt("DB_PORT", 5432),
		DBUser:     getEnv("DB_USER", "fanticbet"),
		DBPassword: getEnv("DB_PASSWORD", "fanticbet"),
		DBName:     getEnv("DB_NAME", "fanticbet"),
		DBDSN:      getEnv("DB_DSN", ""),

		JWTSecret:      getEnv("JWT_SECRET", ""),
		JWTTTLMinutes:  getEnvInt("JWT_TTL_MINUTES", 15),
		RefreshTTLDays: getEnvInt("REFRESH_TTL_DAYS", 30),

		OddsAPIKey: getEnv("ODDS_API_KEY", ""),
		Bookmaker:  getEnv("BOOKMAKER", "pinnacle"),
		Sports:     splitCSV(getEnv("SPORTS", "football,basketball")),

		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURI:  getEnv("GOOGLE_REDIRECT_URI", ""),

		SignupBonus: getEnvInt64("SIGNUP_BONUS", 10000),
		BetMin:      getEnvInt64("BET_MIN", 10),
		BetMax:      getEnvInt64("BET_MAX", 10000),
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	// Build DSN from parts if not set directly
	if cfg.DBDSN == "" {
		cfg.DBDSN = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName,
		)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
