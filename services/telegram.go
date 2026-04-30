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
	bot             *bot.Bot
	redisQueue      *RedisQueue
	redisWorkerPool *RedisWorkerPool
}

func NewTelegramService(botToken string, redisQueue *RedisQueue, workers int) (*TelegramService, error) {
	opts := []bot.Option{}

	b, err := bot.New(botToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	service := &TelegramService{
		bot:        b,
		redisQueue: redisQueue,
	}

	// Initialize Redis worker pool for async processing
	service.redisWorkerPool = NewRedisWorkerPool(workers, redisQueue, service)

	return service, nil
}

// Shutdown gracefully shuts down the telegram service
func (s *TelegramService) Shutdown() {
	if s.redisWorkerPool != nil {
		s.redisWorkerPool.Shutdown()
	}
	if s.redisQueue != nil {
		s.redisQueue.Close()
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
	case models.MediaAlbum:
		return s.sendMediaGroup(ctx, chatID, logReq, parseMode)
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

func (s *TelegramService) sendMediaGroup(ctx context.Context, chatID int64, logReq *models.LogRequest, parseMode string) (int, error) {
	urls := logReq.MediaURLs
	if len(urls) == 0 && logReq.MediaURL != "" {
		urls = []string{logReq.MediaURL}
	}
	if len(urls) == 0 {
		return 0, fmt.Errorf("no media URLs provided for album")
	}

	media := make([]botModels.InputMedia, len(urls))
	for i, u := range urls {
		item := &botModels.InputMediaPhoto{
			Media: u,
		}
		if i == 0 {
			item.Caption = logReq.Caption
			item.ParseMode = botModels.ParseMode(parseMode)
		}
		media[i] = item
	}

	msgs, err := s.bot.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
		ChatID:          chatID,
		Media:           media,
		MessageThreadID: logReq.MessageThreadID,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to send media group: %w", err)
	}
	if len(msgs) == 0 {
		return 0, fmt.Errorf("no messages returned from media group")
	}
	return msgs[0].ID, nil
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

func (s *TelegramService) SendLogWithFile(ctx context.Context, chatIDStr string, mediaType models.MediaType, files []*multipart.FileHeader, caption, parseMode string, messageThreadID int) (int, error) {
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chat_id: %w", err)
	}

	if parseMode == "" {
		parseMode = "HTML"
	}

	if len(files) > 1 || mediaType == models.MediaAlbum {
		return s.sendMediaGroupFiles(ctx, chatID, files, caption, parseMode, messageThreadID)
	}

	file := files[0]
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

func (s *TelegramService) sendMediaGroupFiles(ctx context.Context, chatID int64, files []*multipart.FileHeader, caption, parseMode string, messageThreadID int) (int, error) {
	media := make([]botModels.InputMedia, len(files))
	for i, file := range files {
		f, err := file.Open()
		if err != nil {
			return 0, fmt.Errorf("failed to open file %s: %w", file.Filename, err)
		}
		fileData, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return 0, fmt.Errorf("failed to read file %s: %w", file.Filename, err)
		}
		item := &botModels.InputMediaPhoto{
			Media:           "attach://" + file.Filename,
			MediaAttachment: bytes.NewReader(fileData),
		}
		if i == 0 {
			item.Caption = caption
			item.ParseMode = botModels.ParseMode(parseMode)
		}
		media[i] = item
	}

	msgs, err := s.bot.SendMediaGroup(ctx, &bot.SendMediaGroupParams{
		ChatID:          chatID,
		Media:           media,
		MessageThreadID: messageThreadID,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to send media group: %w", err)
	}
	if len(msgs) == 0 {
		return 0, fmt.Errorf("no messages returned from media group")
	}
	return msgs[0].ID, nil
}

// SendLogAsync submits a log to Redis queue for async processing (fire-and-forget)
func (s *TelegramService) SendLogAsync(logReq *models.LogRequest, jobID string) error {
	return s.redisQueue.Enqueue(jobID, logReq)
}

// GetPoolStats returns worker pool statistics
func (s *TelegramService) GetPoolStats() (map[string]interface{}, error) {
	return s.redisWorkerPool.GetStats()
}
