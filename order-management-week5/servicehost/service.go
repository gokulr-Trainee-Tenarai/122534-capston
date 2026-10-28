package servicehost

import (
	"context"
	"fmt"
	"strconv"

	"order-management/config"
	"order-management/consul"
)

func Port(configured, fallback string) (string, int, error) {
	value := configured
	if value == "" {
		value = fallback
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid APP_PORT %q", value)
	}
	return value, port, nil
}

func Register(ctx context.Context, client *consul.Client, cfg config.Config, defaultName string, port int, healthPath string) (string, error) {
	if client == nil {
		return "", nil
	}
	name := cfg.ServiceName
	if name == "" {
		name = defaultName
	}
	id := cfg.ServiceID
	if id == "" {
		id = fmt.Sprintf("%s-%d", name, port)
	}
	address := cfg.ServiceAddress
	if address == "" {
		address = "127.0.0.1"
	}
	if err := client.Register(ctx, id, name, address, port, healthPath); err != nil {
		return "", err
	}
	return id, nil
}
