package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"telegram-logs/config"
	"telegram-logs/handlers"
	"telegram-logs/middleware"
	"telegram-logs/services"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Validate required configuration
	if cfg.TelegramBotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}

	// Initialize Redis queue for persistent message storage
	redisQueue, err := services.NewRedisQueue(
		cfg.RedisAddr,
		cfg.RedisPassword,
		cfg.RedisDB,
		fmt.Sprintf("worker-%d", time.Now().Unix()),
	)
	if err != nil {
		log.Fatalf("Failed to initialize Redis queue: %v", err)
	}
	log.Printf("✅ Connected to Redis at %s", cfg.RedisAddr)

	// Initialize Telegram service with Redis-backed worker pool
	telegramService, err := services.NewTelegramService(cfg.TelegramBotToken, redisQueue, cfg.Workers)
	if err != nil {
		log.Fatalf("Failed to initialize Telegram service: %v", err)
	}
	log.Printf("✅ Initialized Telegram service with %d workers (Redis-backed)", cfg.Workers)

	// Initialize handlers
	logHandler := handlers.NewLogHandler(telegramService)

	// Initialize middleware
	authMiddleware := middleware.NewAuthMiddleware(cfg.JWTSecret)
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimit, cfg.RateLimitWindow)

	// Setup Fiber app with custom config for better performance
	app := fiber.New(fiber.Config{
		AppName:               "Telegram Logs API v1.0",
		ServerHeader:          "Fiber",
		DisableStartupMessage: false,
		Prefork:               false,
		BodyLimit:             4 * 1024 * 1024, // 4MB limit
	})

	// Global middleware
	app.Use(recover.New()) // Recover from panics
	app.Use(logger.New())  // Request logging

	// Public routes
	app.Get("/health", logHandler.HealthCheck)

	// API v1 routes
	api := app.Group("/api/v1")

	// Protected routes with JWT and rate limiting
	api.Use(authMiddleware.ValidateJWT())
	api.Use(rateLimiter.Limit())
	api.Post("/log", logHandler.SendLog)
	api.Get("/stats", logHandler.GetStats)

	// Graceful shutdown handler
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start server in goroutine
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("🚀 Starting Telegram Logs API on http://localhost%s", addr)
	log.Printf("📝 Health check: http://localhost%s/health", addr)
	log.Printf("📨 Log endpoint: http://localhost%s/api/v1/log", addr)
	log.Printf("📊 Stats endpoint: http://localhost%s/api/v1/stats", addr)

	go func() {
		if err := app.Listen(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-ctx.Done()
	log.Println("🛑 Shutting down gracefully...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown Fiber server
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Printf("⚠️ Server forced to shutdown: %v", err)
	}

	// Shutdown Telegram service (wait for workers to finish)
	telegramService.Shutdown()

	log.Println("✅ Server exited gracefully")
}
