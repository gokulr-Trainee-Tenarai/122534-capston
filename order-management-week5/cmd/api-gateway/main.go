package main

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"order-management/authn"
	"order-management/config"
	"order-management/consul"
	"order-management/middleware"
	"order-management/servicehost"

	"github.com/gin-gonic/gin"
)

type gateway struct {
	registry       *consul.Client
	orderService   string
	staticOrderURL string
	client         *http.Client
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	if len(cfg.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must contain at least 32 characters")
	}
	portString, port, err := servicehost.Port(cfg.AppPort, "8080")
	if err != nil {
		log.Fatal(err)
	}

	var registry *consul.Client
	if cfg.ConsulAddress != "" {
		registry, err = consul.New(cfg.ConsulAddress)
		if err != nil {
			log.Fatalf("Consul configuration error: %v", err)
		}
	} else if cfg.Environment == "production" {
		log.Fatal("CONSUL_HTTP_ADDR is required in production mode")
	}

	orderServiceName := cfg.OrderServiceName
	if orderServiceName == "" {
		orderServiceName = "order-service"
	}
	app := &gateway{
		registry:       registry,
		orderService:   orderServiceName,
		staticOrderURL: cfg.OrderServiceURL,
		client:         &http.Client{Timeout: 30 * time.Second},
	}
	if registry == nil {
		app.staticOrderURL = cfg.OrderServiceURL
		if app.staticOrderURL == "" {
			app.staticOrderURL = "http://localhost:8081"
		}
	}

	router := gin.New()
	router.Use(middleware.RequestID(), middleware.Recovery(), middleware.SecurityHeaders(), middleware.AccessLog(), middleware.Metrics())
	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "api-gateway", "status": "live"})
	})
	router.GET("/health/ready", app.Ready)
	router.GET("/metrics", middleware.PrometheusHandler())
	router.GET("/graphiql", serveGraphiQL)
	router.GET("/graphiql/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/graphiql")
	})

	protected := authn.Middleware([]byte(cfg.JWTSecret))
	router.Any("/api/v1", protected, app.Proxy)
	router.Any("/api/v1/*path", protected, app.Proxy)
	router.POST("/graphql", protected, app.Proxy)

	registerCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	serviceID, err := servicehost.Register(registerCtx, registry, cfg, "api-gateway", port, "/health/ready")
	cancel()
	if err != nil {
		log.Fatalf("register API gateway with Consul: %v", err)
	}

	server := &http.Server{Addr: ":" + portString, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("API gateway listening on :%s", portString)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("API gateway server error: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("API gateway shutdown error: %v", err)
	}
	if registry != nil {
		deregisterCtx, deregisterCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := registry.Deregister(deregisterCtx, serviceID); err != nil {
			log.Printf("deregister API gateway from Consul: %v", err)
		}
		deregisterCancel()
	}
}

//go:embed graphiql.html
var graphiQLPage []byte

func serveGraphiQL(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", graphiQLPage)
}

func (g *gateway) orderURL(ctx context.Context) (string, error) {
	if g.registry != nil {
		return g.registry.Next(ctx, g.orderService)
	}
	if g.staticOrderURL == "" {
		return "", errors.New("order service URL is not configured")
	}
	return strings.TrimRight(g.staticOrderURL, "/"), nil
}

func (g *gateway) Ready(c *gin.Context) {
	if _, err := g.orderURL(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "error": "order service unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (g *gateway) Proxy(c *gin.Context) {
	baseURL, err := g.orderURL(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "order service unavailable"})
		return
	}
	target := baseURL + c.Request.URL.RequestURI()
	request, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not forward request"})
		return
	}
	for _, header := range []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"} {
		if value := c.GetHeader(header); value != "" {
			request.Header.Set(header, value)
		}
	}

	response, err := g.client.Do(request)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "order service unavailable"})
		return
	}
	defer response.Body.Close()
	for _, header := range []string{"Content-Type", "Cache-Control", "X-Request-ID"} {
		for _, value := range response.Header.Values(header) {
			c.Writer.Header().Add(header, value)
		}
	}
	c.Status(response.StatusCode)
	if _, err := io.Copy(c.Writer, response.Body); err != nil {
		log.Printf("event=gateway_response_copy_error request_id=%q error=%q", c.GetString("request_id"), err)
	}
}
