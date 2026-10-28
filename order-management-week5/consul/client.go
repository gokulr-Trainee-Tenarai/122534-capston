package consul

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	address string
	http    *http.Client
	next    atomic.Uint64
	cacheMu sync.RWMutex
	cache   map[string]cachedInstances
}

type cachedInstances struct {
	values    []Instance
	expiresAt time.Time
}

type Instance struct {
	ID      string
	Address string
	Port    int
}

type registration struct {
	ID      string `json:"ID"`
	Name    string `json:"Name"`
	Address string `json:"Address"`
	Port    int    `json:"Port"`
	Check   *check `json:"Check,omitempty"`
}

type check struct {
	HTTP                           string `json:"HTTP"`
	Interval                       string `json:"Interval"`
	Timeout                        string `json:"Timeout"`
	DeregisterCriticalServiceAfter string `json:"DeregisterCriticalServiceAfter"`
}

type serviceEntry struct {
	Node struct {
		Address string
	}
	Service struct {
		ID      string
		Address string
		Port    int
	}
}

func New(address string) (*Client, error) {
	if strings.TrimSpace(address) == "" {
		return nil, fmt.Errorf("Consul address is required")
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Consul address")
	}
	return &Client{
		address: strings.TrimRight(address, "/"),
		http:    &http.Client{Timeout: 3 * time.Second},
		cache:   make(map[string]cachedInstances),
	}, nil
}

func (c *Client) Register(ctx context.Context, id, name, address string, port int, healthPath string) error {
	item := registration{
		ID:      id,
		Name:    name,
		Address: address,
		Port:    port,
	}
	if healthPath != "" {
		item.Check = &check{
			HTTP:                           fmt.Sprintf("http://%s%s", net.JoinHostPort(address, strconv.Itoa(port)), healthPath),
			Interval:                       "10s",
			Timeout:                        "2s",
			DeregisterCriticalServiceAfter: "1m",
		}
	}
	body, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode Consul registration: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.address+"/v1/agent/service/register", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Consul registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("register service with Consul: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Consul service registration returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Deregister(ctx context.Context, id string) error {
	endpoint := c.address + "/v1/agent/service/deregister/" + url.PathEscape(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Consul deregistration request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("deregister service from Consul: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Consul service deregistration returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Discover(ctx context.Context, name string) ([]Instance, error) {
	c.cacheMu.RLock()
	cached, ok := c.cache[name]
	c.cacheMu.RUnlock()
	if ok && time.Now().Before(cached.expiresAt) {
		return append([]Instance(nil), cached.values...), nil
	}

	endpoint := c.address + "/v1/health/service/" + url.PathEscape(name) + "?passing=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Consul discovery request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discover service %q from Consul: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Consul service discovery returned HTTP %d", resp.StatusCode)
	}
	var entries []serviceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode Consul service discovery response: %w", err)
	}
	instances := make([]Instance, 0, len(entries))
	for _, entry := range entries {
		address := entry.Service.Address
		if address == "" {
			address = entry.Node.Address
		}
		if address == "" || entry.Service.Port <= 0 {
			continue
		}
		instances = append(instances, Instance{
			ID:      entry.Service.ID,
			Address: "http://" + net.JoinHostPort(address, strconv.Itoa(entry.Service.Port)),
			Port:    entry.Service.Port,
		})
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no healthy instances registered for service %q", name)
	}
	c.cacheMu.Lock()
	c.cache[name] = cachedInstances{values: append([]Instance(nil), instances...), expiresAt: time.Now().Add(2 * time.Second)}
	c.cacheMu.Unlock()
	return instances, nil
}

func (c *Client) Next(ctx context.Context, name string) (string, error) {
	instances, err := c.Discover(ctx, name)
	if err != nil {
		return "", err
	}
	index := c.next.Add(1) - 1
	return instances[index%uint64(len(instances))].Address, nil
}
