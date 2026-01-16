package services

import (
	"context"
	"log"
	"sync"
	"time"
)

// RedisWorkerPool manages workers that process messages from Redis queue
type RedisWorkerPool struct {
	workers         int
	queue           *RedisQueue
	telegramService *TelegramService
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	pollInterval    time.Duration
}

// NewRedisWorkerPool creates a new Redis-backed worker pool
func NewRedisWorkerPool(workers int, queue *RedisQueue, telegramService *TelegramService) *RedisWorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	rwp := &RedisWorkerPool{
		workers:         workers,
		queue:           queue,
		telegramService: telegramService,
		ctx:             ctx,
		cancel:          cancel,
		pollInterval:    100 * time.Millisecond,
	}

	rwp.start()
	return rwp
}

// start initializes and starts all workers
func (rwp *RedisWorkerPool) start() {
	for i := 0; i < rwp.workers; i++ {
		rwp.wg.Add(1)
		go rwp.worker(i)
	}
	log.Printf("✅ Started Redis worker pool with %d workers", rwp.workers)
}

// worker processes jobs from Redis queue
func (rwp *RedisWorkerPool) worker(id int) {
	defer rwp.wg.Done()

	log.Printf("🚀 Worker %d started", id)

	for {
		select {
		case <-rwp.ctx.Done():
			log.Printf("🛑 Worker %d shutting down", id)
			return
		default:
			// Dequeue messages from Redis
			messages, messageIDs, err := rwp.queue.Dequeue(2*time.Second, 1)
			if err != nil {
				log.Printf("⚠️ Worker %d failed to dequeue: %v", id, err)
				time.Sleep(1 * time.Second)
				continue
			}

			if len(messages) == 0 {
				continue // No messages available
			}

			// Process each message
			for i, msg := range messages {
				rwp.processMessage(id, msg, messageIDs[i])
			}
		}
	}
}

// processMessage handles a single message
func (rwp *RedisWorkerPool) processMessage(workerID int, msg QueueMessage, messageID string) {
	log.Printf("🔄 Worker %d processing job %s (attempt %d/%d)",
		workerID, msg.ID, msg.Retries+1, rwp.queue.maxRetries+1)

	// Send log to Telegram
	_, err := rwp.telegramService.SendLog(rwp.ctx, msg.LogRequest)

	if err != nil {
		log.Printf("❌ Worker %d failed job %s: %v", workerID, msg.ID, err)

		// Retry with exponential backoff or move to DLQ
		if retryErr := rwp.queue.Retry(messageID, msg, err); retryErr != nil {
			log.Printf("⚠️ Worker %d failed to retry job %s: %v", workerID, msg.ID, retryErr)
		}
		return
	}

	// Success - acknowledge the message
	if ackErr := rwp.queue.Ack(messageID); ackErr != nil {
		log.Printf("⚠️ Worker %d failed to ack job %s: %v", workerID, msg.ID, ackErr)
		return
	}

	log.Printf("✅ Worker %d completed job %s", workerID, msg.ID)
}

// Shutdown gracefully shuts down the worker pool
func (rwp *RedisWorkerPool) Shutdown() {
	log.Println("🛑 Shutting down Redis worker pool...")
	rwp.cancel()
	rwp.wg.Wait()
	log.Println("✅ Redis worker pool shut down complete")
}

// GetStats returns worker pool statistics
func (rwp *RedisWorkerPool) GetStats() (map[string]interface{}, error) {
	queueStats, err := rwp.queue.GetQueueStats()
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"workers": rwp.workers,
	}

	// Merge queue stats
	for k, v := range queueStats {
		stats[k] = v
	}

	return stats, nil
}
