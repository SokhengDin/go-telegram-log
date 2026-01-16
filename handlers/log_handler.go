package handlers

import (
	"strconv"
	"telegram-logs/models"
	"telegram-logs/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type LogHandler struct {
	telegramService *services.TelegramService
}

func NewLogHandler(telegramService *services.TelegramService) *LogHandler {
	return &LogHandler{
		telegramService: telegramService,
	}
}

func (h *LogHandler) SendLog(c *fiber.Ctx) error {
	chatID := c.FormValue("chat_id")
	if chatID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   "chat_id is required",
		})
	}

	message := c.FormValue("message")
	caption := c.FormValue("caption")
	mediaType := models.MediaType(c.FormValue("media_type"))
	parseMode := c.FormValue("parse_mode")
	async := c.FormValue("async") == "true" // Check if async mode requested

	messageThreadID, _ := strconv.Atoi(c.FormValue("message_thread_id"))

	if mediaType == "" {
		mediaType = models.MediaText
	}

	file, err := c.FormFile("file")
	if err == nil {
		// File uploads are always synchronous (need to process file)
		messageID, err := h.telegramService.SendLogWithFile(c.Context(), chatID, mediaType, file, caption, parseMode, messageThreadID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
				Success: false,
				Error:   "Failed to send log: " + err.Error(),
			})
		}
		return c.Status(fiber.StatusOK).JSON(models.LogResponse{
			Success:   true,
			Message:   "Log sent successfully",
			MessageID: messageID,
		})
	}

	if mediaType == models.MediaText && message == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   "message is required for text media type",
		})
	}

	mediaURL := c.FormValue("media_url")
	if mediaType != models.MediaText && mediaURL == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   "media_url or file is required for non-text media types",
		})
	}

	logReq := &models.LogRequest{
		ChatID:          chatID,
		Message:         message,
		MediaType:       mediaType,
		MediaURL:        mediaURL,
		Caption:         caption,
		ParseMode:       parseMode,
		MessageThreadID: messageThreadID,
	}

	// Async mode - fire and forget
	if async {
		jobID := uuid.New().String()
		err := h.telegramService.SendLogAsync(logReq, jobID)
		if err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(models.ErrorResponse{
				Success: false,
				Error:   "Failed to queue log: " + err.Error(),
			})
		}
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"success": true,
			"message": "Log queued for sending",
			"job_id":  jobID,
		})
	}

	// Synchronous mode - wait for result
	messageID, err := h.telegramService.SendLog(c.Context(), logReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Success: false,
			Error:   "Failed to send log: " + err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(models.LogResponse{
		Success:   true,
		Message:   "Log sent successfully",
		MessageID: messageID,
	})
}

func (h *LogHandler) HealthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "healthy",
		"service": "telegram-logs-api",
	})
}

func (h *LogHandler) GetStats(c *fiber.Ctx) error {
	stats, err := h.telegramService.GetPoolStats()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Success: false,
			Error:   "Failed to get stats: " + err.Error(),
		})
	}

	result := fiber.Map{"success": true}
	for k, v := range stats {
		result[k] = v
	}
	return c.JSON(result)
}
