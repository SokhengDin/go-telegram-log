package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"telegram-logs/models"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	MainStream       = "telegram:logs:stream"
	DeadLetterStream = "telegram:logs:dlq"
	ConsumerGroup    = "telegram-workers"
	MaxRetries       = 3
)

// RedisQueue manages persistent message queue using Redis Streams
type RedisQueue struct {
	client        *redis.Client
	ctx           context.Context
	consumerName  string
	streamName    string
	dlqName       string
	groupName     string
	maxRetries    int
	retryBackoff  time.Duration
}

// QueueMessage represents a message in the queue
type QueueMessage struct {
	ID              string             `json:"id"`
	LogRequest      *models.LogRequest `json:"log_request"`
	Retries         int                `json:"retries"`
	CreatedAt       time.Time          `json:"created_at"`
	LastAttemptAt   time.Time          `json:"last_attempt_at,omitempty"`
	Error           string             `json:"error,omitempty"`
}

// NewRedisQueue creates a new Redis-based queue
func NewRedisQueue(redisAddr, redisPassword string, redisDB int, consumerName string) (*RedisQueue, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})

	ctx := context.Background()

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rq := &RedisQueue{
		client:       client,
		ctx:          ctx,
		consumerName: consumerName,
		streamName:   MainStream,
		dlqName:      DeadLetterStream,
		groupName:    ConsumerGroup,
		maxRetries:   MaxRetries,
		retryBackoff: 2 * time.Second,
	}

	// Create consumer group if it doesn't exist
	if err := rq.ensureConsumerGroup(); err != nil {
		return nil, err
	}

	log.Printf("✅ Redis Queue initialized (stream: %s, group: %s, consumer: %s)",
		rq.streamName, rq.groupName, rq.consumerName)

	return rq, nil
}

// ensureConsumerGroup creates the consumer group if it doesn't exist
func (rq *RedisQueue) ensureConsumerGroup() error {
	// Try to create the group
	err := rq.client.XGroupCreateMkStream(rq.ctx, rq.streamName, rq.groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}
	return nil
}

// Enqueue adds a message to the queue
func (rq *RedisQueue) Enqueue(jobID string, logReq *models.LogRequest) error {
	msg := QueueMessage{
		ID:         jobID,
		LogRequest: logReq,
		Retries:    0,
		CreatedAt:  time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Add to Redis Stream
	_, err = rq.client.XAdd(rq.ctx, &redis.XAddArgs{
		Stream: rq.streamName,
		Values: map[string]interface{}{
			"job_id":  jobID,
			"payload": string(data),
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to add message to stream: %w", err)
	}

	log.Printf("📥 Enqueued job %s to Redis stream", jobID)
	return nil
}

// Dequeue reads messages from the queue for processing
func (rq *RedisQueue) Dequeue(block time.Duration, count int64) ([]QueueMessage, []string, error) {
	// Read from consumer group
	streams, err := rq.client.XReadGroup(rq.ctx, &redis.XReadGroupArgs{
		Group:    rq.groupName,
		Consumer: rq.consumerName,
		Streams:  []string{rq.streamName, ">"},
		Count:    count,
		Block:    block,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return nil, nil, nil // No messages
		}
		return nil, nil, fmt.Errorf("failed to read from stream: %w", err)
	}

	var messages []QueueMessage
	var messageIDs []string

	for _, stream := range streams {
		for _, message := range stream.Messages {
			payload, ok := message.Values["payload"].(string)
			if !ok {
				log.Printf("⚠️ Invalid message format: %v", message.ID)
				continue
			}

			var qMsg QueueMessage
			if err := json.Unmarshal([]byte(payload), &qMsg); err != nil {
				log.Printf("⚠️ Failed to unmarshal message %s: %v", message.ID, err)
				continue
			}

			messages = append(messages, qMsg)
			messageIDs = append(messageIDs, message.ID)
		}
	}

	return messages, messageIDs, nil
}

// Ack acknowledges successful processing of a message
func (rq *RedisQueue) Ack(messageID string) error {
	err := rq.client.XAck(rq.ctx, rq.streamName, rq.groupName, messageID).Err()
	if err != nil {
		return fmt.Errorf("failed to ack message: %w", err)
	}
	return nil
}

// Retry requeues a message for retry or moves to DLQ if max retries exceeded
func (rq *RedisQueue) Retry(messageID string, msg QueueMessage, err error) error {
	msg.Retries++
	msg.LastAttemptAt = time.Now()
	msg.Error = err.Error()

	// If max retries exceeded, move to dead letter queue
	if msg.Retries > rq.maxRetries {
		log.Printf("💀 Job %s exceeded max retries (%d), moving to DLQ",
			msg.ID, rq.maxRetries)
		return rq.moveToDLQ(messageID, msg)
	}

	// Acknowledge the original message
	if ackErr := rq.Ack(messageID); ackErr != nil {
		log.Printf("⚠️ Failed to ack message during retry: %v", ackErr)
	}

	// Calculate backoff
	backoff := time.Duration(msg.Retries) * rq.retryBackoff

	log.Printf("🔄 Retrying job %s (attempt %d/%d) after %v: %v",
		msg.ID, msg.Retries, rq.maxRetries, backoff, err)

	// Wait for backoff period
	time.Sleep(backoff)

	// Re-enqueue the message
	data, marshalErr := json.Marshal(msg)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal retry message: %w", marshalErr)
	}

	_, addErr := rq.client.XAdd(rq.ctx, &redis.XAddArgs{
		Stream: rq.streamName,
		Values: map[string]interface{}{
			"job_id":  msg.ID,
			"payload": string(data),
		},
	}).Result()

	return addErr
}

// moveToDLQ moves a failed message to the dead letter queue
func (rq *RedisQueue) moveToDLQ(messageID string, msg QueueMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ message: %w", err)
	}

	// Add to DLQ stream
	_, err = rq.client.XAdd(rq.ctx, &redis.XAddArgs{
		Stream: rq.dlqName,
		Values: map[string]interface{}{
			"job_id":        msg.ID,
			"payload":       string(data),
			"failed_at":     time.Now().Format(time.RFC3339),
			"retries":       msg.Retries,
			"error":         msg.Error,
			"original_id":   messageID,
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to add to DLQ: %w", err)
	}

	// Acknowledge the original message
	return rq.Ack(messageID)
}

// GetQueueStats returns queue statistics
func (rq *RedisQueue) GetQueueStats() (map[string]interface{}, error) {
	// Get stream info
	info, err := rq.client.XInfoStream(rq.ctx, rq.streamName).Result()
	if err != nil {
		return nil, err
	}

	// Get DLQ length
	dlqLength, _ := rq.client.XLen(rq.ctx, rq.dlqName).Result()

	// Get pending messages count
	pending, _ := rq.client.XPending(rq.ctx, rq.streamName, rq.groupName).Result()

	return map[string]interface{}{
		"stream_length":    info.Length,
		"pending_messages": pending.Count,
		"dlq_length":       dlqLength,
		"first_entry_id":   info.FirstEntry.ID,
		"last_entry_id":    info.LastEntry.ID,
	}, nil
}

// Close closes the Redis connection
func (rq *RedisQueue) Close() error {
	log.Println("🛑 Closing Redis queue connection...")
	return rq.client.Close()
}
