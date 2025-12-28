package middleware

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.RWMutex
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}

	// Cleanup old entries periodically using goroutine
	go rl.cleanup()

	return rl
}

func (rl *RateLimiter) Limit() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get client identifier (IP or user ID from JWT)
		clientID := c.IP()

		// Try to get user from JWT claims
		if claims := c.Locals("user_claims"); claims != nil {
			if claimsMap, ok := claims.(jwt.MapClaims); ok {
				if sub, ok := claimsMap["sub"].(string); ok {
					clientID = sub
				}
			}
		}

		rl.mutex.Lock()
		defer rl.mutex.Unlock()

		now := time.Now()
		windowStart := now.Add(-rl.window)

		// Get request history for this client
		requests, exists := rl.requests[clientID]
		if !exists {
			requests = []time.Time{}
		}

		// Filter out requests outside the current window
		validRequests := []time.Time{}
		for _, reqTime := range requests {
			if reqTime.After(windowStart) {
				validRequests = append(validRequests, reqTime)
			}
		}

		// Check if limit exceeded
		if len(validRequests) >= rl.limit {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"success": false,
				"error":   "Rate limit exceeded. Please try again later.",
			})
		}

		// Add current request
		validRequests = append(validRequests, now)
		rl.requests[clientID] = validRequests

		return c.Next()
	}
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mutex.Lock()
		now := time.Now()
		windowStart := now.Add(-rl.window)

		for clientID, requests := range rl.requests {
			validRequests := []time.Time{}
			for _, reqTime := range requests {
				if reqTime.After(windowStart) {
					validRequests = append(validRequests, reqTime)
				}
			}

			if len(validRequests) == 0 {
				delete(rl.requests, clientID)
			} else {
				rl.requests[clientID] = validRequests
			}
		}
		rl.mutex.Unlock()
	}
}
