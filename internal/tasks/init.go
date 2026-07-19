// Package tasks provides the global background slow-task queue singleton.
//
// The underlying queue implementation lives in infra/bktask. This package
// owns the global instance and exposes Init/Global/Stop for use in main.
package tasks

import "BrainForever/infra/bktask"

var globalQueue *bktask.TaskQueue

// InitGlobal initializes the global background task queue singleton
// with the given config and logger, then starts it.
// Must be called once during startup (typically in main).
// Subsequent calls are no-ops.
func InitGlobal(cfg bktask.Config, logger bktask.Logger) {
	if globalQueue != nil {
		return
	}
	globalQueue = bktask.New(cfg, logger)
	globalQueue.Start()
}

// Global returns the global TaskQueue instance previously initialized
// by InitGlobal. Returns nil if InitGlobal has not been called.
func Global() *bktask.TaskQueue {
	return globalQueue
}

// StopGlobal gracefully stops the global task queue and clears all tasks.
// Typically called via defer in main.
func StopGlobal() {
	if globalQueue != nil {
		globalQueue.Stop()
		globalQueue = nil
	}
}
