package handlers

import (
	"log"
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

	form, err := c.MultipartForm()
	if err == nil && form != nil && len(form.File["files"]) > 0 {
		files := form.File["files"]
		messageID, err := h.telegramService.SendLogWithFile(c.Context(), chatID, mediaType, files, caption, parseMode, messageThreadID)
		if err != nil {
			log.Printf("ERROR SendLogWithFile chat_id=%s media_type=%s: %v", chatID, mediaType, err)
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
	mediaURLs := c.Request().PostArgs().PeekMulti("media_urls")
	var mediaURLList []string
	for _, u := range mediaURLs {
		mediaURLList = append(mediaURLList, string(u))
	}

	if mediaType != models.MediaText && mediaURL == "" && len(mediaURLList) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Success: false,
			Error:   "media_url, media_urls, or files is required for non-text media types",
		})
	}

	if len(mediaURLList) > 1 {
		mediaType = models.MediaAlbum
	}

	logReq := &models.LogRequest{
		ChatID:          chatID,
		Message:         message,
		MediaType:       mediaType,
		MediaURL:        mediaURL,
		MediaURLs:       mediaURLList,
		Caption:         caption,
		ParseMode:       parseMode,
		MessageThreadID: messageThreadID,
	}

	// Async mode - fire and forget
	if async {
		jobID := uuid.New().String()
		err := h.telegramService.SendLogAsync(logReq, jobID)
		if err != nil {
			log.Printf("ERROR SendLogAsync chat_id=%s job_id=%s: %v", chatID, jobID, err)
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
		log.Printf("ERROR SendLog chat_id=%s media_type=%s: %v", chatID, mediaType, err)
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
		log.Printf("ERROR GetStats: %v", err)
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
