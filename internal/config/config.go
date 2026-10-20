package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	Port           string
	MongoURI       string
	MongoDB        string
	JWTSecret      string
	JWTExpiryHours int
	AdminUserID    string
	AdminPassword  string
	AdminName      string
	AdminEmail     string
	ResendAPIKey   string
	ResendFrom     string
	AppName        string
	AppBaseURL     string
}

// Load reads configuration from environment variables.
// It returns an error if any required variable is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:        getEnv("PORT", "8080"),
		MongoURI:    getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:     getEnv("MONGO_DB", "arch_erp"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		AdminUserID: getEnv("ADMIN_USER_ID", "admin"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "Admin@1234"),
		AdminName:   getEnv("ADMIN_NAME", "System Administrator"),
		AdminEmail:  getEnv("ADMIN_EMAIL", "admin@archfirm.com"),
		ResendAPIKey: os.Getenv("RESEND_API_KEY"),
		ResendFrom:  getEnv("RESEND_FROM", "notifications@archfirm.com"),
		AppName:     getEnv("APP_NAME", "Arch ERP"),
		AppBaseURL:  getEnv("APP_BASE_URL", "http://localhost:8080"),
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET environment variable is required")
	}

	expiryStr := getEnv("JWT_EXPIRY_HOURS", "24")
	expiry, err := strconv.Atoi(expiryStr)
	if err != nil {
		return nil, fmt.Errorf("JWT_EXPIRY_HOURS must be an integer: %w", err)
	}
	cfg.JWTExpiryHours = expiry

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
