package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"order-management/authn"
	"order-management/model"

	"github.com/gin-gonic/gin"
	"github.com/sony/gobreaker"
)

func TestHTTPProductClientForwardsAuthenticatedRequestToken(t *testing.T) {
	const secret = "test-secret-long-enough-for-jwt-validation"
	token, err := authn.IssueToken([]byte(secret), model.User{
		ID:         9,
		Username:   "customer",
		Role:       model.RoleCustomer,
		CustomerID: 101,
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	productServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("product request Authorization = %q, want original bearer token", got)
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"price":12.5}`))
	}))
	defer productServer.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	client := NewHTTPProductClient(productServer.URL, time.Second, 0)
	router.GET("/orders", authn.Middleware([]byte(secret)), func(c *gin.Context) {
		price, err := client.Price(c.Request.Context(), 501)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"price": price})
	})
	request := httptest.NewRequest(http.MethodGet, "/orders", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("order request returned %d: %s", response.Code, response.Body.String())
	}
}

func TestHTTPProductClientRetriesWithExponentialBackoff(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) <= 2 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"price":12.5}`))
	}))
	defer server.Close()

	client := NewHTTPProductClient(server.URL, time.Second, 2)
	start := time.Now()
	price, err := client.Price(context.Background(), 501)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Price returned unexpected error: %v", err)
	}
	if price != 12.5 {
		t.Fatalf("expected price 12.5, got %v", price)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("expected 3 requests (initial plus 2 retries), got %d", got)
	}
	if elapsed < 290*time.Millisecond {
		t.Fatalf("expected 100ms and 200ms backoff, elapsed %s", elapsed)
	}
}

func TestHTTPProductClientDoesNotRetryClientErrors(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewHTTPProductClient(server.URL, time.Second, 3)
	_, err := client.Price(context.Background(), 404)
	if !errors.Is(err, ErrProductUnavailable) {
		t.Fatalf("expected controlled product error, got %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected 1 request for a non-retryable status, got %d", got)
	}
}

func TestHTTPProductClientEnforcesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewHTTPProductClient(server.URL, 30*time.Millisecond, 0)
	_, err := client.Price(context.Background(), 501)
	if !errors.Is(err, ErrProductUnavailable) {
		t.Fatalf("expected controlled product error, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout to be preserved in error, got %v", err)
	}
}

func TestHTTPProductClientCancellationStopsRetryBackoff(t *testing.T) {
	var requests atomic.Int32
	firstRequest := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		firstRequest <- struct{}{}
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPProductClient(server.URL, time.Second, 5)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Price(ctx, 501)
		result <- err
	}()

	<-firstRequest
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, ErrProductUnavailable) || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected controlled cancellation error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Price did not stop after the request context was canceled")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected cancellation to prevent retries, got %d requests", got)
	}
}

func TestHTTPProductClientOpensCircuitAfterRepeatedFailures(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPProductClient(server.URL, time.Second, 0)
	for attempt := 0; attempt < circuitFailureLimit; attempt++ {
		_, err := client.Price(context.Background(), 501)
		if !errors.Is(err, ErrProductUnavailable) {
			t.Fatalf("attempt %d: expected controlled product error, got %v", attempt+1, err)
		}
	}

	_, err := client.Price(context.Background(), 501)
	if !errors.Is(err, ErrProductUnavailable) || !errors.Is(err, gobreaker.ErrOpenState) {
		t.Fatalf("expected controlled circuit-open error, got %v", err)
	}
	if got := requests.Load(); got != int32(circuitFailureLimit) {
		t.Fatalf("expected open circuit to prevent another request, got %d requests", got)
	}
}

func TestHTTPProductClientBoundsRetryConfiguration(t *testing.T) {
	if got := NewHTTPProductClient("http://localhost", time.Second, maxProductRetries+100).retries; got != maxProductRetries {
		t.Fatalf("expected retries capped at %d, got %d", maxProductRetries, got)
	}
	if got := NewHTTPProductClient("http://localhost", time.Second, -1).retries; got != 0 {
		t.Fatalf("expected negative retries clamped to 0, got %d", got)
	}
	if got := NewHTTPProductClient("http://localhost", 0, 0).timeout; got != defaultProductTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultProductTimeout, got)
	}
}
