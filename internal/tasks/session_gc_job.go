// Package tasks provides the global background slow-task queue singleton
// and the periodic session GC task.
package tasks

import (
	"time"

	"BrainForever/infra/zylog"
	"BrainForever/internal/config"
	"BrainForever/internal/session"
)

// ============================================================
// Registration
// ============================================================

// RegisterPeriodicSessionGC registers the session GC as a recurring task
// in the global bktask queue. Must be called after InitGlobal().
func RegisterPeriodicSessionGC(
	cfg config.SessionGCConfig,
	sessionManager *session.Manager,
	logger zylog.Logger,
) {
	if !cfg.Enabled {
		logger.Infof("✓ periodic session GC task disabled by config")
		return
	}

	interval := time.Duration(cfg.IntervalMinutes) * time.Minute
	err := TheBkTaskQueue().AddRecurring("session-gc", interval, func() error {
		sessionManager.GCOnce()
		return nil
	})
	if err != nil {
		logger.Errorf("failed to register session GC task. %v", err)
		return
	}
	logger.Infof("✓ session GC task registered (interval=%v)", interval)
}
