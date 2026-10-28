package servicehost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"order-management/config"
	"order-management/consul"
)

func TestPortUsesFallbackAndRejectsOutOfRangeValues(t *testing.T) {
	value, number, err := Port("", "8081")
	if err != nil || value != "8081" || number != 8081 {
		t.Fatalf("Port fallback = (%q, %d, %v), want (8081, 8081, nil)", value, number, err)
	}

	for _, invalid := range []string{"0", "65536", "not-a-port"} {
		if _, _, err := Port(invalid, "8080"); err == nil {
			t.Errorf("Port(%q) should fail", invalid)
		}
	}
}

func TestRegisterIsNoopWithoutConsul(t *testing.T) {
	id, err := Register(context.Background(), nil, config.Config{}, "order-service", 8081, "/health/ready")
	if err != nil || id != "" {
		t.Fatalf("Register without Consul = (%q, %v), want empty ID and nil error", id, err)
	}
}

func TestRegisterUsesConfiguredServiceIdentity(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/agent/service/register" {
			t.Errorf("unexpected Consul registration request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode registration: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := consul.New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	id, err := Register(context.Background(), client, config.Config{
		ServiceName:    "custom-order",
		ServiceID:      "custom-order-instance",
		ServiceAddress: "10.0.0.5",
	}, "order-service", 8081, "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	if id != "custom-order-instance" {
		t.Fatalf("service ID = %q, want configured ID", id)
	}
	if got["Name"] != "custom-order" || got["Address"] != "10.0.0.5" || got["Port"] != float64(8081) {
		t.Fatalf("registered service = %#v", got)
	}
}
