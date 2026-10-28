package config

import (
	"strings"
	"testing"
)

func TestProductionRequiresJWTSecret(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DB_HOST", "database.example")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("CONSUL_HTTP_ADDR", "http://localhost:8500")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET must contain") {
		t.Fatalf("expected production JWT_SECRET configuration error, got %v", err)
	}
}
