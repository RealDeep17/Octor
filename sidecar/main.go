package main

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/sidecar/internal/config"
	"github.com/webtor-io/sidecar/internal/handler"
	"github.com/webtor-io/sidecar/internal/stashdb"
	"github.com/webtor-io/sidecar/internal/tpdb"
)

func main() {
	// Configure logging
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	log.SetLevel(log.InfoLevel)
	log.Info("Starting Octor Sidecar (Go Rewrite)...")

	// Load configuration
	cfg := config.Load()

	// Initialize API Clients
	tpdbClient := tpdb.NewClient(cfg.ThePornDBAPIKey, cfg.TPDBBase)
	stashdbClient := stashdb.NewClient(cfg.StashDBAPIKey, cfg.StashDBEndpoint)

	// Create Handler
	h := handler.NewHandler(cfg, tpdbClient, stashdbClient)

	// Set Gin mode
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Set up router
	r := gin.New()
	r.Use(gin.Recovery())

	// Custom Logger Middleware
	r.Use(func(c *gin.Context) {
		log.Infof("Request: %s %s", c.Request.Method, c.Request.URL.String())
		c.Next()
	})

	// Register handlers
	r.GET("/", h.Metadata)
	r.GET("/settings", h.GetSettings)
	r.POST("/settings", h.PostSettings)

	// Run server
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Infof("Listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Server failed to run: %v", err)
	}
}
