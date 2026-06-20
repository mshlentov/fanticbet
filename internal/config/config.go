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

	// Cookie для refresh-токена. Secure=false в dev (http://localhost),
	// true в prod (https). Domain="" означает текущий хост.
	CookieSecure bool
	CookieDomain string

	// CORS: список разрешённых origin'ов (фронты). В dev — Vite на :5173.
	CORSAllowedOrigins []string

	OddsAPIKey string
	Bookmaker  string
	Sports     []string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	YandexClientID     string
	YandexClientSecret string
	YandexRedirectURI  string

	VKClientID     string
	VKClientSecret string
	VKRedirectURI  string

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

		CookieSecure: getEnvBool("COOKIE_SECURE", false),
		CookieDomain: getEnv("COOKIE_DOMAIN", ""),

		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),

		OddsAPIKey: getEnv("ODDS_API_KEY", ""),
		Bookmaker:  getEnv("BOOKMAKER", "pinnacle"),
		Sports:     splitCSV(getEnv("SPORTS", "football,basketball")),

		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURI:  getEnv("GOOGLE_REDIRECT_URI", ""),

		YandexClientID:     getEnv("YANDEX_CLIENT_ID", ""),
		YandexClientSecret: getEnv("YANDEX_CLIENT_SECRET", ""),
		YandexRedirectURI:  getEnv("YANDEX_REDIRECT_URI", "http://localhost:8080/api/v1/auth/yandex/callback"),

		VKClientID:     getEnv("VK_CLIENT_ID", ""),
		VKClientSecret: getEnv("VK_CLIENT_SECRET", ""),
		VKRedirectURI:  getEnv("VK_REDIRECT_URI", "http://localhost:8080/api/v1/auth/vk/callback"),

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

// getEnvBool читает bool из env. Принимает 1/0, true/false (case-insensitive),
// yes/no. При ошибке или пустом значении — fallback.
func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
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
