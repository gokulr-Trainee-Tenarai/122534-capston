package middleware

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLogIncludesRequestFieldsAndError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previousOutput)

	router := gin.New()
	router.Use(RequestID(), AccessLog())
	router.GET("/orders/:id", func(c *gin.Context) {
		_ = c.Error(errors.New("database unavailable"))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	})

	request := httptest.NewRequest(http.MethodGet, "/orders/42", nil)
	request.Header.Set("X-Request-ID", "test-request-123")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	entry := output.String()
	for _, field := range []string{
		"request_id=test-request-123",
		"method=GET",
		"path=/orders/:id",
		"status=500",
		"duration=",
		"error=",
		"database unavailable",
	} {
		if !strings.Contains(entry, field) {
			t.Errorf("access log missing %q: %s", field, entry)
		}
	}
}

func TestMetricsStoreTracksRequestsLatencyAndErrors(t *testing.T) {
	store := &metricsStore{
		requests:  make(map[string]int64),
		latencies: make(map[string]latencySummary),
	}
	store.Record("POST", "/api/v1/orders", "500", 0.015, true)

	snapshot := store.Snapshot()
	for _, metric := range []string{
		`http_requests_total{method="POST",path="/api/v1/orders",status="500"} 1`,
		`http_request_duration_seconds_count{method="POST",path="/api/v1/orders"} 1`,
		`http_request_duration_seconds_sum{method="POST",path="/api/v1/orders"} 0.015000`,
		"http_errors_total 1",
		"database_errors_total ",
	} {
		if !strings.Contains(snapshot, metric) {
			t.Errorf("metrics snapshot missing %q: %s", metric, snapshot)
		}
	}
}
