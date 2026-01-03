package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"telegram-logs/models"

	"github.com/go-telegram/bot"
	botModels "github.com/go-telegram/bot/models"
)

type TelegramService struct {
	bot        *bot.Bot
	workerPool *WorkerPool
}

func NewTelegramService(botToken string, workers int, queueSize int) (*TelegramService, error) {
	opts := []bot.Option{}

	b, err := bot.New(botToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	// Initialize worker pool for async processing
	workerPool := NewWorkerPool(workers, queueSize)

	return &TelegramService{
		bot:        b,
		workerPool: workerPool,
	}, nil
}

// Shutdown gracefully shuts down the telegram service
func (s *TelegramService) Shutdown() {
	if s.workerPool != nil {
		s.workerPool.Shutdown()
	}
}

func (s *TelegramService) SendLog(ctx context.Context, logReq *models.LogRequest) (int, error) {
	chatID, err := strconv.ParseInt(logReq.ChatID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chat_id: %w", err)
	}

	parseMode := logReq.ParseMode
	if parseMode == "" {
		parseMode = "HTML"
	}

	mediaType := logReq.MediaType
	if mediaType == "" {
		mediaType = models.MediaText
	}

	switch mediaType {
	case models.MediaPhoto:
		return s.sendPhoto(ctx, chatID, logReq, parseMode)
	case models.MediaDocument:
		return s.sendDocument(ctx, chatID, logReq, parseMode)
	case models.MediaVideo:
		return s.sendVideo(ctx, chatID, logReq, parseMode)
	default:
		return s.sendText(ctx, chatID, logReq, parseMode)
	}
}

func (s *TelegramService) sendText(ctx context.Context, chatID int64, logReq *models.LogRequest, parseMode string) (int, error) {
	msg, err := s.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          chatID,
		Text:            logReq.Message,
		ParseMode:       botModels.ParseMode(parseMode),
		MessageThreadID: logReq.MessageThreadID,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to send message: %w", err)
	}
	return msg.ID, nil
}

func (s *TelegramService) sendPhoto(ctx context.Context, chatID int64, logReq *models.LogRequest, parseMode string) (int, error) {
	photo := &botModels.InputFileString{Data: logReq.MediaURL}
	msg, err := s.bot.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:          chatID,
		Photo:           photo,
		Caption:         logReq.Caption,
		ParseMode:       botModels.ParseMode(parseMode),
		MessageThreadID: logReq.MessageThreadID,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to send photo: %w", err)
	}
	return msg.ID, nil
}

func (s *TelegramService) sendDocument(ctx context.Context, chatID int64, logReq *models.LogRequest, parseMode string) (int, error) {
	document := &botModels.InputFileString{Data: logReq.MediaURL}
	msg, err := s.bot.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:          chatID,
		Document:        document,
		Caption:         logReq.Caption,
		ParseMode:       botModels.ParseMode(parseMode),
		MessageThreadID: logReq.MessageThreadID,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to send document: %w", err)
	}
	return msg.ID, nil
}

func (s *TelegramService) sendVideo(ctx context.Context, chatID int64, logReq *models.LogRequest, parseMode string) (int, error) {
	video := &botModels.InputFileString{Data: logReq.MediaURL}
	msg, err := s.bot.SendVideo(ctx, &bot.SendVideoParams{
		ChatID:          chatID,
		Video:           video,
		Caption:         logReq.Caption,
		ParseMode:       botModels.ParseMode(parseMode),
		MessageThreadID: logReq.MessageThreadID,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to send video: %w", err)
	}
	return msg.ID, nil
}

func (s *TelegramService) SendLogWithFile(ctx context.Context, chatIDStr string, mediaType models.MediaType, file *multipart.FileHeader, caption, parseMode string, messageThreadID int) (int, error) {
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chat_id: %w", err)
	}

	if parseMode == "" {
		parseMode = "HTML"
	}

	f, err := file.Open()
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	fileData, err := io.ReadAll(f)
	if err != nil {
		return 0, fmt.Errorf("failed to read file: %w", err)
	}

	inputFile := &botModels.InputFileUpload{
		Filename: file.Filename,
		Data:     bytes.NewReader(fileData),
	}

	switch mediaType {
	case models.MediaPhoto:
		msg, err := s.bot.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID:          chatID,
			Photo:           inputFile,
			Caption:         caption,
			ParseMode:       botModels.ParseMode(parseMode),
			MessageThreadID: messageThreadID,
		})
		if err != nil {
			return 0, fmt.Errorf("failed to send photo: %w", err)
		}
		return msg.ID, nil

	case models.MediaDocument:
		msg, err := s.bot.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:          chatID,
			Document:        inputFile,
			Caption:         caption,
			ParseMode:       botModels.ParseMode(parseMode),
			MessageThreadID: messageThreadID,
		})
		if err != nil {
			return 0, fmt.Errorf("failed to send document: %w", err)
		}
		return msg.ID, nil

	case models.MediaVideo:
		msg, err := s.bot.SendVideo(ctx, &bot.SendVideoParams{
			ChatID:          chatID,
			Video:           inputFile,
			Caption:         caption,
			ParseMode:       botModels.ParseMode(parseMode),
			MessageThreadID: messageThreadID,
		})
		if err != nil {
			return 0, fmt.Errorf("failed to send video: %w", err)
		}
		return msg.ID, nil

	default:
		return 0, fmt.Errorf("invalid media type for file upload: %s", mediaType)
	}
}

// SendLogAsync submits a log to the worker pool for async processing (fire-and-forget)
func (s *TelegramService) SendLogAsync(logReq *models.LogRequest, jobID string) error {
	job := Job{
		ID:      jobID,
		LogReq:  logReq,
		Service: s,
		Result:  nil, // Fire and forget - no result channel
	}
	return s.workerPool.Submit(job)
}

// GetPoolStats returns worker pool statistics
func (s *TelegramService) GetPoolStats() PoolStats {
	return s.workerPool.Stats()
}
