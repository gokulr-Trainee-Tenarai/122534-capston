package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"order-management/observability"

	"github.com/gin-gonic/gin"
)

type latencySummary struct {
	count int64
	total float64
}

type metricsStore struct {
	mu          sync.RWMutex
	requests    map[string]int64
	latencies   map[string]latencySummary
	errorsTotal int64
}

var appMetrics = &metricsStore{
	requests:  make(map[string]int64),
	latencies: make(map[string]latencySummary),
}

func (m *metricsStore) Record(method, path, status string, durationSeconds float64, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests[method+"|"+path+"|"+status]++
	key := method + "|" + path
	entry := m.latencies[key]
	entry.count++
	entry.total += durationSeconds
	m.latencies[key] = entry
	if isError {
		m.errorsTotal++
	}
}

func (m *metricsStore) Snapshot() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder
	for key, count := range m.requests {
		parts := strings.SplitN(key, "|", 3)
		if len(parts) != 3 {
			continue
		}
		sb.WriteString(fmt.Sprintf("http_requests_total{method=%q,path=%q,status=%q} %d\n", parts[0], parts[1], parts[2], count))
	}
	for key, summary := range m.latencies {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		sb.WriteString(fmt.Sprintf("http_request_duration_seconds_count{method=%q,path=%q} %d\n", parts[0], parts[1], summary.count))
		sb.WriteString(fmt.Sprintf("http_request_duration_seconds_sum{method=%q,path=%q} %0.6f\n", parts[0], parts[1], summary.total))
	}
	sb.WriteString(fmt.Sprintf("http_errors_total %d\n", m.errorsTotal))
	sb.WriteString(fmt.Sprintf("database_errors_total %d\n", observability.DatabaseErrorCount()))
	return sb.String()
}

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		errorMessage := "-"
		if len(c.Errors) > 0 {
			errorMessage = c.Errors.String()
		} else if c.Writer.Status() >= http.StatusBadRequest {
			errorMessage = http.StatusText(c.Writer.Status())
		}
		log.Printf("request_id=%s method=%s path=%s status=%d duration=%s error=%q", c.GetString("request_id"), c.Request.Method, path, c.Writer.Status(), duration, errorMessage)
	}
}

func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		statusCode := strconv.Itoa(c.Writer.Status())
		appMetrics.Record(c.Request.Method, path, statusCode, time.Since(start).Seconds(), c.Writer.Status() >= 400)
	}
}

func MetricsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		c.String(http.StatusOK, appMetrics.Snapshot())
	}
}

func PrometheusHandler() gin.HandlerFunc {
	return MetricsHandler()
}
