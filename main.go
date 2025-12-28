package main

import (
	"fmt"
	"log"
	"telegram-logs/config"
	"telegram-logs/handlers"
	"telegram-logs/middleware"
	"telegram-logs/services"

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

	// Initialize services with goroutine support
	telegramService, err := services.NewTelegramService(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("Failed to initialize Telegram service: %v", err)
	}

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

	// Start server
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("🚀 Starting Telegram Logs API on http://localhost%s", addr)
	log.Printf("📝 Health check: http://localhost%s/health", addr)
	log.Printf("📨 Log endpoint: http://localhost%s/api/v1/log", addr)

	if err := app.Listen(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
