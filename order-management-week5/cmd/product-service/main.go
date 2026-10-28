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

	"order-management/config"
	"order-management/consul"
	"order-management/database"
	"order-management/middleware"
	"order-management/model"
	"order-management/productservice"
	"order-management/servicehost"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	if len(cfg.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must contain at least 32 characters")
	}
	if cfg.DBHost == "" {
		log.Fatal("DB_HOST is required for the product service")
	}

	portString, port, err := servicehost.Port(cfg.AppPort, "8082")
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("database pool initialization failed: %v", err)
	}
	defer sqlDB.Close()
	if err := database.Migrate(db, &model.Product{}); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}
	if err := productservice.BootstrapProduct(db, cfg.ProductBootstrapID, cfg.ProductBootstrapName, cfg.ProductBootstrapPrice); err != nil {
		log.Fatalf("bootstrap product failed: %v", err)
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

	router := gin.New()
	router.Use(middleware.RequestID(), middleware.Recovery(), middleware.SecurityHeaders(), middleware.AccessLog(), middleware.Metrics())
	router.GET("/metrics", middleware.PrometheusHandler())
	productservice.NewHandler(db, []byte(cfg.JWTSecret)).RegisterRoutes(router)

	registerCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	serviceID, err := servicehost.Register(registerCtx, registry, cfg, "product-service", port, "/health/ready")
	cancel()
	if err != nil {
		log.Fatalf("register product service with Consul: %v", err)
	}

	server := &http.Server{Addr: ":" + portString, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("product service listening on :%s", portString)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("product service server error: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("product service shutdown error: %v", err)
	}
	if registry != nil {
		deregisterCtx, deregisterCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := registry.Deregister(deregisterCtx, serviceID); err != nil {
			log.Printf("deregister product service from Consul: %v", err)
		}
		deregisterCancel()
	}
}
