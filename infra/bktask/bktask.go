// Package bktask provides a periodic background task queue for slow, infrequent tasks.
//
// The queue runs on a configurable check interval. Tasks are represented by BkgndTask
// structs containing a job function, a one-shot flag, and an interval duration.
//
//   - OneShot == false: recurring task. The queue automatically re-adds the task after
//     each execution. The interval is the delay between consecutive runs.
//   - OneShot == true: the task executes once (after Interval delay if > 0,
//     or on the next tick if == 0), then is removed.
//
// Every task's job is internally wrapped with fixed logging (start/complete/fail)
// and the re-adding logic for recurring tasks, so the user only needs to Add() once.
//
// All public methods are safe for concurrent use.
package bktask

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================
// Logger interface (minimal, to keep the package decoupled)
// ============================================================

// Logger is the logging interface required by TaskQueue.
// Any logger with Infof/Errorf methods (e.g. zylog.Logger, slog.Logger)
// satisfies it without additional adapter code.
type Logger interface {
	Infof(format string, args ...any)
	Errorf(format string, args ...any)
}

// ============================================================
// Config
// ============================================================

// Config configures a TaskQueue.
type Config struct {
	// CheckInterval is how often the queue checks for due tasks.
	// If zero or negative, defaults to 10 minutes.
	CheckInterval time.Duration

	// WorkerCount limits concurrent task executions.
	// 0 or negative means unlimited (all tasks run in their own goroutine).
	WorkerCount int

	// QueueSize is the maximum number of queued tasks.
	// 0 or negative means unlimited.
	QueueSize int
}

// ============================================================
// BkgndTask
// ============================================================

// BkgndTask represents a background task with its scheduling parameters.
//
//   - OneShot == false: recurring — runs every Interval, automatically re-added.
//   - OneShot == true: one-shot — runs once after Interval delay, then removed.
//   - Interval == 0 (and OneShot == true): executes on the next check tick.
type BkgndTask struct {
	Name     string        // Optional human-readable name for logging
	Job      func() error  // The job function to execute
	OneShot  bool          // true: one-shot; false: recurring
	Interval time.Duration // Delay between executions (recurring) or before execution (one-shot)
}

// ============================================================
// taskEntry (internal)
// ============================================================

// taskEntry wraps a BkgndTask with its scheduled execution time.
type taskEntry struct {
	task    BkgndTask
	nextRun time.Time // Scheduled execution time
}

// ============================================================
// TaskQueue
// ============================================================

// TaskQueue manages a collection of background tasks with periodic checking.
//
// Lifecycle:
//  1. Create with New().
//  2. Start() begins the periodic loop.
//  3. Add() enqueues tasks (may be called before or after Start).
//  4. Pause()/Resume() temporarily suspend/resume execution.
//  5. Stop() halts the loop and clears all tasks.
//  6. Clear() removes all tasks without stopping the loop.
type TaskQueue struct {
	mu            sync.RWMutex
	checkInterval time.Duration
	tasks         []*taskEntry
	ticker        *time.Ticker
	running       bool
	paused        bool
	stopCh        chan struct{}
	logger        Logger
	semaphore     chan struct{} // nil = unlimited; buffered chan of size WorkerCount
}

// New creates a new TaskQueue with the given Config and a logger.
// If cfg.CheckInterval is zero or negative, defaults to 10 minutes.
// If cfg.WorkerCount > 0, a semaphore limits concurrent executions.
// If cfg.QueueSize > 0, the external queue capacity hint is logged.
// If logger is nil, all log output is silently discarded.
func New(cfg Config, logger Logger) *TaskQueue {
	checkInterval := cfg.CheckInterval
	if checkInterval <= 0 {
		checkInterval = 10 * time.Minute
	}

	if logger == nil {
		logger = nopLogger{}
	}

	var semaphore chan struct{}
	if cfg.WorkerCount > 0 {
		semaphore = make(chan struct{}, cfg.WorkerCount)
	}

	q := &TaskQueue{
		checkInterval: checkInterval,
		tasks:         make([]*taskEntry, 0),
		logger:        logger,
		semaphore:     semaphore,
	}

	logger.Infof("bktask: queue created, checkInterval=%v, workers=%d, queueSize=%d",
		checkInterval, cfg.WorkerCount, cfg.QueueSize)
	return q
}

// ============================================================
// Public methods
// ============================================================

// Add enqueues a new task.
//
// For recurring tasks (OneShot == false), the queue wraps the job with
// logging and automatically re-adds the task after each execution. The user
// only needs to call Add() once.
//
// Returns an error if the job function is nil.
func (q *TaskQueue) Add(task BkgndTask) error {
	if task.Job == nil {
		return fmt.Errorf("bktask: job function is nil")
	}

	if task.Interval < 0 {
		task.Interval = 0
	}

	entry := &taskEntry{task: task}
	now := time.Now()

	if task.OneShot && task.Interval == 0 {
		// One-shot, no delay: execute on the very next tick
		entry.nextRun = now
	} else {
		// One-shot with delay or recurring: schedule after the specified interval
		entry.nextRun = now.Add(task.Interval)
	}

	q.mu.Lock()
	q.tasks = append(q.tasks, entry)
	q.mu.Unlock()

	q.logger.Infof("bktask: task added (name=%q, oneShot=%v, interval=%v, nextRun=%s)",
		task.Name, task.OneShot, task.Interval, entry.nextRun.Format(time.RFC3339))
	return nil
}

// AddOneShot is a convenience method that creates a one-shot task with the given
// name, delay and job function and adds it to the queue.
//
// It is equivalent to calling Add with BkgndTask{Name: name, Job: job, OneShot: true, Interval: delay}.
// Returns an error if the job function is nil.
func (q *TaskQueue) AddOneShot(name string, delay time.Duration, job func() error) error {
	return q.Add(BkgndTask{
		Name:     name,
		Job:      job,
		OneShot:  true,
		Interval: delay,
	})
}

// AddRecurring is a convenience method that creates a recurring task with the given
// name, interval and job function and adds it to the queue.
//
// It is equivalent to calling Add with BkgndTask{Name: name, Job: job, OneShot: false, Interval: interval}.
// Returns an error if the job function is nil.
func (q *TaskQueue) AddRecurring(name string, interval time.Duration, job func() error) error {
	return q.Add(BkgndTask{
		Name:     name,
		Job:      job,
		OneShot:  false,
		Interval: interval,
	})
}

// Clear removes all tasks from the queue without stopping the loop.
func (q *TaskQueue) Clear() {
	q.mu.Lock()
	q.tasks = make([]*taskEntry, 0)
	q.mu.Unlock()

	q.logger.Infof("bktask: all tasks cleared")
}

// Start begins the periodic task checking loop. If already running, this is a no-op.
func (q *TaskQueue) Start() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.running {
		return
	}

	q.running = true
	q.paused = false
	q.stopCh = make(chan struct{})
	q.ticker = time.NewTicker(q.checkInterval)

	go q.loop()

	q.logger.Infof("bktask: started")
}

// Stop halts the periodic loop and clears all tasks. After Stop, the queue
// cannot be restarted; use Start() to create a new loop.
func (q *TaskQueue) Stop() {
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return
	}
	q.running = false
	close(q.stopCh)
	q.ticker.Stop()
	q.mu.Unlock()

	q.Clear()
	q.logger.Infof("bktask: stopped")
}

// Pause suspends task execution. The loop continues running but will skip
// task checks until Resume() is called. No-op if already paused or not running.
func (q *TaskQueue) Pause() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.running || q.paused {
		return
	}
	q.paused = true
	q.logger.Infof("bktask: paused")
}

// Resume restores task execution after a pause. No-op if not paused or not running.
func (q *TaskQueue) Resume() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.running || !q.paused {
		return
	}
	q.paused = false
	q.logger.Infof("bktask: resumed")
}

// ============================================================
// Internal loop
// ============================================================

// loop is the main event loop. It runs in a separate goroutine.
func (q *TaskQueue) loop() {
	for {
		select {
		case <-q.stopCh:
			q.logger.Infof("bktask: loop exiting")
			return

		case <-q.ticker.C:
			q.checkAndRun()
		}
	}
}

// checkAndRun inspects all tasks and executes any that are due.
// Due tasks are removed from the queue and executed asynchronously
// so they do not block each other.
func (q *TaskQueue) checkAndRun() {
	// ---- Phase 1: snapshot under lock ----
	q.mu.Lock()

	if q.paused || !q.running {
		q.mu.Unlock()
		return
	}

	now := time.Now()

	// Partition tasks into: due (to execute), not-due (to keep).
	var dueEntries []*taskEntry
	keepEntries := make([]*taskEntry, 0, len(q.tasks))

	for _, entry := range q.tasks {
		if !now.Before(entry.nextRun) {
			dueEntries = append(dueEntries, entry)
		} else {
			keepEntries = append(keepEntries, entry)
		}
	}

	q.tasks = keepEntries
	q.mu.Unlock()

	// ---- Phase 2: execute jobs asynchronously ----
	for _, entry := range dueEntries {
		go q.safeRun(entry)
	}
}

// safeRun wraps the user's job with fixed logging and automatic re-adding
// for recurring tasks. One-shot tasks are executed once and then forgotten.
// If the queue has a semaphore, it acquires a slot before running and
// releases it after completion.
func (q *TaskQueue) safeRun(entry *taskEntry) {
	// Acquire semaphore slot (if configured).
	// This blocks if all workers are busy, effectively limiting concurrency.
	if q.semaphore != nil {
		q.semaphore <- struct{}{}
	}

	// Release semaphore slot on exit.
	defer func() {
		if q.semaphore != nil {
			<-q.semaphore
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			q.logger.Errorf("bktask: job panicked. %v", r)
		}
	}()

	q.logger.Infof("bktask: executing task (name=%q, oneShot=%v, interval=%v)",
		entry.task.Name, entry.task.OneShot, entry.task.Interval)

	err := entry.task.Job()

	if err != nil {
		q.logger.Errorf("bktask: job failed (name=%q, oneShot=%v, interval=%v). %v",
			entry.task.Name, entry.task.OneShot, entry.task.Interval, err)
	} else {
		q.logger.Infof("bktask: task completed (name=%q, oneShot=%v, interval=%v)",
			entry.task.Name, entry.task.OneShot, entry.task.Interval)
	}

	// Recurring task: re-add to the queue for the next cycle.
	if !entry.task.OneShot {
		q.Add(BkgndTask{
			Name:     entry.task.Name,
			Job:      entry.task.Job,
			OneShot:  false,
			Interval: entry.task.Interval,
		})
	}
}

// ============================================================
// nopLogger
// ============================================================

// nopLogger silently discards all log output.
type nopLogger struct{}

func (nopLogger) Infof(format string, args ...any)  {}
func (nopLogger) Errorf(format string, args ...any) {}
