package database

import (
	"strings"
	"testing"

	"order-management/config"
)

func TestConnectRequiresDatabaseHost(t *testing.T) {
	_, err := Connect(config.Config{})
	if err == nil || !strings.Contains(err.Error(), "DB_HOST is required") {
		t.Fatalf("Connect error = %v, want DB_HOST required error", err)
	}
}
