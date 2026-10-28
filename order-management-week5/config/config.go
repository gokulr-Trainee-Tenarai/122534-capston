package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds application settings loaded from environment variables.
type Config struct {
	Environment            string
	AppPort                string
	DBHost                 string
	DBPort                 string
	DBName                 string
	DBUser                 string
	DBPassword             string
	DBSSLMode              string
	JWTSecret              string
	ConsulAddress          string
	ServiceName            string
	ServiceID              string
	ServiceAddress         string
	OrderServiceURL        string
	OrderServiceName       string
	ProductServiceURL      string
	ProductServiceName     string
	ProductTimeout         time.Duration
	ProductRetries         int
	BootstrapAdminUsername string
	BootstrapAdminPassword string
	ProductBootstrapID     int64
	ProductBootstrapName   string
	ProductBootstrapPrice  float64
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		Environment:            envString("APP_ENV"),
		AppPort:                envString("APP_PORT"),
		DBHost:                 envString("DB_HOST"),
		DBPort:                 envString("DB_PORT"),
		DBName:                 envString("DB_NAME"),
		DBUser:                 envString("DB_USER"),
		DBPassword:             envString("DB_PASSWORD"),
		DBSSLMode:              envString("DB_SSLMODE"),
		JWTSecret:              envString("JWT_SECRET"),
		ConsulAddress:          envString("CONSUL_HTTP_ADDR"),
		ServiceName:            envString("SERVICE_NAME"),
		ServiceID:              envString("SERVICE_ID"),
		ServiceAddress:         envString("SERVICE_ADDRESS"),
		OrderServiceURL:        envString("ORDER_SERVICE_URL"),
		OrderServiceName:       envString("ORDER_SERVICE_NAME"),
		ProductServiceURL:      envString("PRODUCT_SERVICE_URL"),
		ProductServiceName:     envString("PRODUCT_SERVICE_NAME"),
		ProductTimeout:         time.Duration(envInt("PRODUCT_TIMEOUT_MS")) * time.Millisecond,
		ProductRetries:         envInt("PRODUCT_RETRIES"),
		BootstrapAdminUsername: envString("BOOTSTRAP_ADMIN_USERNAME"),
		BootstrapAdminPassword: envString("BOOTSTRAP_ADMIN_PASSWORD"),
		ProductBootstrapID:     int64(envInt("PRODUCT_BOOTSTRAP_ID")),
		ProductBootstrapName:   envString("PRODUCT_BOOTSTRAP_NAME"),
		ProductBootstrapPrice:  envFloat("PRODUCT_BOOTSTRAP_PRICE"),
	}

	if cfg.Environment == "production" && len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must contain at least 32 characters in production mode")
	}
	if cfg.Environment == "production" && cfg.ConsulAddress == "" {
		return Config{}, fmt.Errorf("CONSUL_HTTP_ADDR is required in production mode")
	}
	if cfg.Environment == "production" && cfg.ServiceAddress == "" {
		return Config{}, fmt.Errorf("SERVICE_ADDRESS is required in production mode")
	}

	return cfg, nil
}

func envString(key string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func envInt(key string) int {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func envFloat(key string) float64 {
	if value, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}
