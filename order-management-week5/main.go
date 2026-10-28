package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"order-management/authn"
	"order-management/config"
	"order-management/consul"
	"order-management/controller"
	"order-management/database"
	"order-management/middleware"
	"order-management/model"
	"order-management/repository"
	"order-management/service"
	"order-management/servicehost"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	if len(cfg.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must contain at least 32 characters")
	}
	port, portNumber, err := servicehost.Port(cfg.AppPort, "8081")
	if err != nil {
		log.Fatal(err)
	}

	var repo repository.OrderRepository
	var closeDB func()
	if cfg.DBHost != "" {
		db, err := database.Connect(cfg)
		if err != nil {
			log.Fatalf("database connection failed: %v", err)
		}
		if err := database.Migrate(db, &model.Order{}); err != nil {
			log.Fatalf("database migration failed: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			log.Fatalf("database pool initialization failed: %v", err)
		}
		repo = repository.NewGormOrderRepository(db)
		closeDB = func() { _ = sqlDB.Close() }
	} else if cfg.Environment != "development" && cfg.Environment != "test" {
		log.Fatal("DB_HOST is required outside development/test mode")
	} else {
		log.Println("DB_HOST empty: starting with in-memory repository for local API exploration")
		repo = repository.NewMemoryRepository()
	}
	if closeDB != nil {
		defer closeDB()
	}

	var consulClient *consul.Client
	if cfg.ConsulAddress != "" {
		consulClient, err = consul.New(cfg.ConsulAddress)
		if err != nil {
			log.Fatalf("Consul configuration error: %v", err)
		}
	}
	var productClient service.ProductClient
	if consulClient != nil {
		serviceName := cfg.ProductServiceName
		if serviceName == "" {
			serviceName = "product-service"
		}
		productClient = service.NewConsulProductClient(consulClient, serviceName, cfg.ProductTimeout, cfg.ProductRetries)
	} else {
		productClient = service.NewHTTPProductClient(cfg.ProductServiceURL, cfg.ProductTimeout, cfg.ProductRetries)
	}
	orderService := service.NewOrderService(repo, productClient)
	orderController := controller.NewOrderController(orderService)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery(), middleware.SecurityHeaders(), middleware.AccessLog(), middleware.Metrics())
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "live"}) })
	r.GET("/health/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := repo.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "error": "database unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	r.GET("/metrics", middleware.PrometheusHandler())

	api := r.Group("/api/v1")
	graphqlAPI := r.Group("")
	jwtAuth := authn.Middleware([]byte(cfg.JWTSecret))
	api.Use(jwtAuth)
	graphqlAPI.Use(jwtAuth)
	orderController.Register(api)
	controller.RegisterGraphQL(graphqlAPI, orderService)

	var registered bool
	var serviceID string
	if consulClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		serviceID, err = servicehost.Register(ctx, consulClient, cfg, "order-service", portNumber, "/health/ready")
		cancel()
		if err != nil {
			log.Fatalf("register order service with Consul: %v", err)
		}
		registered = true
	} else if cfg.Environment == "production" {
		log.Fatal("CONSUL_HTTP_ADDR is required in production mode")
	}

	srv := &http.Server{Addr: ":" + port, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("order API listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
	if registered {
		deregisterCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := consulClient.Deregister(deregisterCtx, serviceID); err != nil {
			log.Printf("deregister order service from Consul: %v", err)
		}
		cancel()
	}
}
