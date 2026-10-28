package consul

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRegisterDiscoverAndRoundRobin(t *testing.T) {
	var registrations atomic.Int32
	var discoveries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/agent/service/register":
			registrations.Add(1)
			var body registration
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode registration: %v", err)
			}
			if body.Name != "order-service" || body.Port != 8081 || body.Check == nil {
				t.Errorf("unexpected registration: %+v", body)
			}
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/v1/health/service/order-service":
			discoveries.Add(1)
			_, _ = w.Write([]byte(`[
				{"Node":{"Address":"127.0.0.1"},"Service":{"ID":"order-1","Address":"127.0.0.1","Port":8081}},
				{"Node":{"Address":"127.0.0.1"},"Service":{"ID":"order-2","Address":"127.0.0.1","Port":8084}}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), "order-service-8081", "order-service", "127.0.0.1", 8081, "/health/ready"); err != nil {
		t.Fatal(err)
	}
	first, err := client.Next(context.Background(), "order-service")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Next(context.Background(), "order-service")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("expected round-robin to select separate instances, got %q twice", first)
	}
	if got := discoveries.Load(); got != 1 {
		t.Fatalf("expected cached discovery after first lookup, got %d calls", got)
	}
}

func TestDiscoveryCacheIsSafeForConcurrentCallers(t *testing.T) {
	var discoveries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveries.Add(1)
		_, _ = w.Write([]byte(`[{"Node":{"Address":"127.0.0.1"},"Service":{"ID":"product-1","Address":"127.0.0.1","Port":8082}}]`))
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	const callers = 25
	var workers sync.WaitGroup
	workers.Add(callers)
	for range callers {
		go func() {
			defer workers.Done()
			if _, err := client.Next(context.Background(), "product-service"); err != nil {
				t.Errorf("discover product service: %v", err)
			}
		}()
	}
	workers.Wait()
	if discoveries.Load() < 1 {
		t.Fatal("expected at least one discovery request")
	}
}
