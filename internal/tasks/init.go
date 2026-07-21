// Package tasks provides the global background slow-task queue singleton.
//
// The underlying queue implementation lives in infra/bktask. This package
// owns the global instance and exposes Init/Global/Stop for use in main.
package tasks

import "BrainForever/infra/bktask"

var theBkTaskQueue *bktask.TaskQueue

// InitTheBkTaskQueue initializes the global background task queue singleton
// with the given config and logger, then starts it.
// Must be called once during startup (typically in main).
// Subsequent calls are no-ops.
func InitTheBkTaskQueue(cfg bktask.Config, logger bktask.Logger) {
	if theBkTaskQueue != nil {
		return
	}
	theBkTaskQueue = bktask.New(cfg, logger)
	theBkTaskQueue.Start()
}

// TheBkTaskQueue returns the global TaskQueue instance previously initialized
// by InitTheBkTaskQueue. Returns nil if InitTheBkTaskQueue has not been called.
func TheBkTaskQueue() *bktask.TaskQueue {
	return theBkTaskQueue
}

// StopTheBkTaskQueue gracefully stops the global task queue and clears all tasks.
// Typically called via defer in main.
func StopTheBkTaskQueue() {
	if theBkTaskQueue != nil {
		theBkTaskQueue.Stop()
		theBkTaskQueue = nil
	}
}
