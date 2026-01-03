package services

import (
	"context"
	"log"
	"sync"
	"telegram-logs/models"
)

// Job represents a task to be processed by workers
type Job struct {
	ID      string
	LogReq  *models.LogRequest
	Service *TelegramService
	Result  chan JobResult
}

// JobResult contains the result of a job execution
type JobResult struct {
	MessageID int
	Error     error
}

// WorkerPool manages a pool of workers for concurrent job processing
type WorkerPool struct {
	workers    int
	jobQueue   chan Job
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	jobResults map[string]JobResult
}

// NewWorkerPool creates a new worker pool with specified number of workers
func NewWorkerPool(workers int, queueSize int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	wp := &WorkerPool{
		workers:    workers,
		jobQueue:   make(chan Job, queueSize),
		ctx:        ctx,
		cancel:     cancel,
		jobResults: make(map[string]JobResult),
	}

	wp.start()
	return wp
}

// start initializes and starts all workers
func (wp *WorkerPool) start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
	log.Printf("✅ Started worker pool with %d workers", wp.workers)
}

// worker processes jobs from the queue
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.ctx.Done():
			log.Printf("🛑 Worker %d shutting down", id)
			return
		case job := <-wp.jobQueue:
			wp.processJob(job, id)
		}
	}
}

// processJob executes a single job
func (wp *WorkerPool) processJob(job Job, workerID int) {
	log.Printf("🔄 Worker %d processing job %s", workerID, job.ID)

	messageID, err := job.Service.SendLog(wp.ctx, job.LogReq)

	result := JobResult{
		MessageID: messageID,
		Error:     err,
	}

	// Store result
	wp.mu.Lock()
	wp.jobResults[job.ID] = result
	wp.mu.Unlock()

	// Send result to channel if provided
	if job.Result != nil {
		select {
		case job.Result <- result:
		case <-wp.ctx.Done():
		}
	}

	if err != nil {
		log.Printf("❌ Worker %d failed job %s: %v", workerID, job.ID, err)
	} else {
		log.Printf("✅ Worker %d completed job %s (msg_id: %d)", workerID, job.ID, messageID)
	}
}

// Submit adds a job to the queue (non-blocking)
func (wp *WorkerPool) Submit(job Job) error {
	select {
	case wp.jobQueue <- job:
		return nil
	case <-wp.ctx.Done():
		return context.Canceled
	default:
		return ErrQueueFull
	}
}

// SubmitAndWait submits a job and waits for the result
func (wp *WorkerPool) SubmitAndWait(job Job) JobResult {
	resultChan := make(chan JobResult, 1)
	job.Result = resultChan

	if err := wp.Submit(job); err != nil {
		return JobResult{Error: err}
	}

	return <-resultChan
}

// GetResult retrieves a job result by ID
func (wp *WorkerPool) GetResult(jobID string) (JobResult, bool) {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	result, exists := wp.jobResults[jobID]
	return result, exists
}

// Shutdown gracefully shuts down the worker pool
func (wp *WorkerPool) Shutdown() {
	log.Println("🛑 Shutting down worker pool...")
	wp.cancel()
	close(wp.jobQueue)
	wp.wg.Wait()
	log.Println("✅ Worker pool shut down complete")
}

// Stats returns current worker pool statistics
func (wp *WorkerPool) Stats() PoolStats {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	return PoolStats{
		Workers:       wp.workers,
		QueueSize:     len(wp.jobQueue),
		QueueCapacity: cap(wp.jobQueue),
		CompletedJobs: len(wp.jobResults),
	}
}

// PoolStats contains worker pool statistics
type PoolStats struct {
	Workers       int
	QueueSize     int
	QueueCapacity int
	CompletedJobs int
}

// Custom errors
var (
	ErrQueueFull = &PoolError{Message: "worker queue is full"}
)

type PoolError struct {
	Message string
}

func (e *PoolError) Error() string {
	return e.Message
}
