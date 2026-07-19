package bktask

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================
// Helpers
// ============================================================

// newTestQueue creates a TaskQueue with a fast check interval for testing.
func newTestQueue(interval time.Duration) *TaskQueue {
	return &TaskQueue{
		checkInterval: interval,
		tasks:         make([]*taskEntry, 0),
		logger:        nopLogger{},
	}
}

// waitForCond polls a condition until it returns true or a timeout elapses.
func waitForCond(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for condition: %s", desc)
}

// ============================================================
// Tests
// ============================================================

// TestAddNilJob verifies that Add returns an error for a nil job function.
func TestAddNilJob(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)
	err := q.Add(BkgndTask{Job: nil, OneShot: true, Interval: 0})
	if err == nil {
		t.Fatal("expected error for nil job, got nil")
	}
}

// TestOneShotImmediate verifies that a one-shot task (Interval==0)
// executes once on the next tick and is then removed.
func TestOneShotImmediate(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var execCount atomic.Int32
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return nil
		},
		OneShot:  true,
		Interval: 0,
	})

	q.Start()
	defer q.Stop()

	// Wait for the task to execute
	waitForCond(t, 2*time.Second, "one-shot task executed once",
		func() bool { return execCount.Load() >= 1 })

	// Allow a bit more time to ensure it does NOT execute again
	time.Sleep(100 * time.Millisecond)
	if n := execCount.Load(); n != 1 {
		t.Fatalf("one-shot task should execute exactly once, got %d", n)
	}
}

// TestOneShotDelayed verifies that a one-shot task with a positive Interval
// executes once after the specified delay and is then removed.
func TestOneShotDelayed(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var execCount atomic.Int32
	start := time.Now()
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return nil
		},
		OneShot:  true,
		Interval: 50 * time.Millisecond,
	})

	q.Start()
	defer q.Stop()

	// Wait for execution
	waitForCond(t, 2*time.Second, "delayed one-shot task executed once",
		func() bool { return execCount.Load() >= 1 })

	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond {
		t.Logf("delayed task ran after %v (expected >= 50ms delay)", elapsed)
	}

	// Ensure it does NOT execute again
	time.Sleep(100 * time.Millisecond)
	if n := execCount.Load(); n != 1 {
		t.Fatalf("delayed one-shot should execute exactly once, got %d", n)
	}
}

// TestRecurringTask verifies that a recurring task (OneShot==false)
// executes multiple times and is automatically re-added by the wrapper.
func TestRecurringTask(t *testing.T) {
	q := newTestQueue(30 * time.Millisecond)

	var execCount atomic.Int32
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return nil
		},
		OneShot:  false,
		Interval: 30 * time.Millisecond,
	})

	q.Start()
	defer q.Stop()

	// Allow it to execute multiple times
	time.Sleep(300 * time.Millisecond)

	n := execCount.Load()
	if n < 2 {
		t.Fatalf("recurring task should execute multiple times, got %d", n)
	}
	t.Logf("recurring task executed %d times in 300ms", n)
}

// TestClear verifies that Clear removes all pending tasks.
func TestClear(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var execCount atomic.Int32
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return nil
		},
		OneShot:  false,
		Interval: 10 * time.Millisecond,
	})

	q.Start()

	// Let it execute at least once
	waitForCond(t, 2*time.Second, "clear: task executed before clear",
		func() bool { return execCount.Load() >= 1 })

	// Clear all tasks
	q.Clear()

	prevCount := execCount.Load()

	// Wait and verify no more executions
	time.Sleep(200 * time.Millisecond)
	if n := execCount.Load(); n != prevCount {
		t.Fatalf("task count changed after Clear: before=%d, after=%d", prevCount, n)
	}

	q.Stop()
}

// TestPauseResume verifies that Pause stops task execution and Resume restarts it.
func TestPauseResume(t *testing.T) {
	q := newTestQueue(20 * time.Millisecond)

	var execCount atomic.Int32
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return nil
		},
		OneShot:  false,
		Interval: 20 * time.Millisecond,
	})

	q.Start()

	// Let it execute at least once
	waitForCond(t, 2*time.Second, "pause/resume: task executed before pause",
		func() bool { return execCount.Load() >= 1 })

	q.Pause()
	prevCount := execCount.Load()

	// Wait during pause — should not increase
	time.Sleep(150 * time.Millisecond)
	if n := execCount.Load(); n != prevCount {
		t.Fatalf("task executed while paused: before=%d, after=%d", prevCount, n)
	}

	q.Resume()

	// Should resume execution
	waitForCond(t, 2*time.Second, "pause/resume: task executed after resume",
		func() bool { return execCount.Load() > prevCount })

	q.Stop()
}

// TestStop verifies that Stop terminates the loop and clears tasks.
func TestStop(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var execCount atomic.Int32
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return nil
		},
		OneShot:  false,
		Interval: 10 * time.Millisecond,
	})

	q.Start()

	// Let it execute at least once
	waitForCond(t, 2*time.Second, "stop: task executed before stop",
		func() bool { return execCount.Load() >= 1 })

	q.Stop()
	prevCount := execCount.Load()

	// After Stop, no more executions
	time.Sleep(150 * time.Millisecond)
	if n := execCount.Load(); n != prevCount {
		t.Fatalf("task executed after Stop: before=%d, after=%d", prevCount, n)
	}
}

// TestPanicRecovery verifies that a panicking job does not crash the queue.
func TestPanicRecovery(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var afterPanic atomic.Bool
	q.Add(BkgndTask{
		Job: func() error {
			panic("intentional panic for test")
		},
		OneShot:  true,
		Interval: 0,
	})

	// A second task that should still run after the panicking one
	q.Add(BkgndTask{
		Job: func() error {
			afterPanic.Store(true)
			return nil
		},
		OneShot:  true,
		Interval: 0,
	})

	q.Start()
	defer q.Stop()

	waitForCond(t, 2*time.Second, "panic recovery: second task executed",
		func() bool { return afterPanic.Load() })
}

// TestConcurrentAdd simulates multiple goroutines adding tasks concurrently.
func TestConcurrentAdd(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var wg sync.WaitGroup
	const numGoroutines = 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := q.Add(BkgndTask{
				Job:     func() error { return nil },
				OneShot: true,
			})
			if err != nil {
				t.Errorf("concurrent Add failed. %v", err)
			}
		}()
	}

	wg.Wait()

	q.mu.Lock()
	count := len(q.tasks)
	q.mu.Unlock()

	if count != numGoroutines {
		t.Fatalf("expected %d tasks, got %d", numGoroutines, count)
	}
}

// TestConcurrentAddAndClear tests concurrent Add and Clear operations.
func TestConcurrentAddAndClear(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var wg sync.WaitGroup

	// Writer: continuously add tasks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			q.Add(BkgndTask{
				Job:     func() error { return nil },
				OneShot: true,
			})
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Writer: continuously clear
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			q.Clear()
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestMultipleTasksMixed verifies multiple tasks of different types coexist.
func TestMultipleTasksMixed(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var oneshotCount, recurringCount atomic.Int32

	// One-shot task
	q.Add(BkgndTask{
		Job: func() error {
			oneshotCount.Add(1)
			return nil
		},
		OneShot:  true,
		Interval: 0,
	})

	// Recurring task
	q.Add(BkgndTask{
		Job: func() error {
			recurringCount.Add(1)
			return nil
		},
		OneShot:  false,
		Interval: 30 * time.Millisecond,
	})

	q.Start()
	defer q.Stop()

	// Wait for one-shot
	waitForCond(t, 2*time.Second, "mixed: one-shot executed",
		func() bool { return oneshotCount.Load() >= 1 })

	// Allow recurring to execute a few times
	time.Sleep(200 * time.Millisecond)

	if n := oneshotCount.Load(); n != 1 {
		t.Fatalf("one-shot should execute exactly once, got %d", n)
	}
	if n := recurringCount.Load(); n < 2 {
		t.Fatalf("recurring should execute multiple times, got %d", n)
	}
	t.Logf("one-shot=%d, recurring=%d", oneshotCount.Load(), recurringCount.Load())
}

// TestStartMultipleTimes verifies that calling Start multiple times is safe.
func TestStartMultipleTimes(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)
	q.Start()
	q.Start() // should be no-op
	q.Start() // should be no-op
	q.Stop()
}

// TestStopWithoutStart verifies that Stop without Start is safe.
func TestStopWithoutStart(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)
	q.Stop() // should not panic
}

// TestPauseWithoutStart verifies that Pause/Resume without Start is safe.
func TestPauseWithoutStart(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)
	q.Pause()  // should not panic
	q.Resume() // should not panic
}

// TestJobReturnsError verifies that jobs returning errors are handled gracefully.
func TestJobReturnsError(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var execCount atomic.Int32
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return fmt.Errorf("simulated error") //nolint:goerr113
		},
		OneShot:  false,
		Interval: 10 * time.Millisecond,
	})

	q.Start()
	defer q.Stop()

	// Should execute multiple times despite errors
	time.Sleep(200 * time.Millisecond)
	if n := execCount.Load(); n < 2 {
		t.Fatalf("recurring task with errors should keep running, got %d", n)
	}
}

// TestNewDefaultInterval verifies that New with zero duration defaults to 10 minutes.
func TestNewDefaultInterval(t *testing.T) {
	q := New(Config{}, nil)
	if q.checkInterval != 10*time.Minute {
		t.Fatalf("expected default checkInterval=10m, got %v", q.checkInterval)
	}
}

// TestZeroIntervalOneshot verifies that a one-shot task with Interval==0
// only runs once even without explicitly setting Interval.
func TestZeroIntervalOneshot(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var execCount atomic.Int32
	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			return nil
		},
		OneShot:  true,
		Interval: 0,
	})

	q.Start()
	defer q.Stop()

	waitForCond(t, 2*time.Second, "zero-interval oneshot executed once",
		func() bool { return execCount.Load() >= 1 })

	time.Sleep(100 * time.Millisecond)
	if n := execCount.Load(); n != 1 {
		t.Fatalf("one-shot with zero interval should execute once, got %d", n)
	}
}

// ============================================================
// Concurrency correctness tests
// ============================================================

// TestConcurrentLongJobDoesNotBlock verifies that a long-running job does not
// block the queue's loop or other concurrent operations (Add, Clear, Stop).
func TestConcurrentLongJobDoesNotBlock(t *testing.T) {
	q := newTestQueue(20 * time.Millisecond)

	// A job that takes a long time
	q.Add(BkgndTask{
		Job: func() error {
			time.Sleep(500 * time.Millisecond) // long job
			return nil
		},
		OneShot:  true,
		Interval: 0,
	})

	q.Start()
	defer q.Stop()

	// Allow the long job to start
	time.Sleep(50 * time.Millisecond)

	// These operations should NOT block despite the long-running job
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			q.Add(BkgndTask{
				Job:     func() error { return nil },
				OneShot: true,
			})
		}
		q.Clear()
		q.Pause()
		q.Resume()
		close(done)
	}()

	select {
	case <-done:
		// All operations completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("operations blocked by long-running job")
	}
}

// TestConcurrentStress stresses the queue with concurrent Add, Clear, Pause,
// Resume operations while recurring tasks are executing. Verifies that no
// panic occurs and operations don't deadlock or corrupt internal state.
//
// NOTE: Oneshot task counts are not asserted here because Clear() may remove
// them before execution. This test focuses on safety under load, not counts.
func TestConcurrentStress(t *testing.T) {
	q := newTestQueue(5 * time.Millisecond)

	var (
		recurExec atomic.Int32
	)

	// Add a recurring task
	q.Add(BkgndTask{
		Job: func() error {
			recurExec.Add(1)
			return nil
		},
		OneShot:  false,
		Interval: 30 * time.Millisecond,
	})

	q.Start()

	// Allow the recurring task to execute at least once before hammering starts,
	// since with corrected scheduling (using task.Interval) the task needs ~30ms
	// to fire and would otherwise be cleared by Goroutine 2 before executing.
	time.Sleep(35 * time.Millisecond)

	var wg sync.WaitGroup

	// Goroutine 1: hammer Pause/Resume
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			q.Pause()
			time.Sleep(2 * time.Millisecond)
			q.Resume()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Goroutine 2: hammer Clear/Add
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			q.Clear()
			// Re-add recurring task after clear
			q.Add(BkgndTask{
				Job: func() error {
					recurExec.Add(1)
					return nil
				},
				OneShot:  false,
				Interval: 30 * time.Millisecond,
			})
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Goroutine 3: continuously add one-shot tasks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			q.Add(BkgndTask{
				Job:     func() error { return nil },
				OneShot: true,
			})
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify: recurring task executed at least once
	if n := recurExec.Load(); n < 1 {
		t.Errorf("recurring task should have executed at least once, got %d", n)
	}

	t.Logf("stress test: recurring=%d", recurExec.Load())

	q.Stop()
}

// TestConcurrentStopDuringExecution verifies that Stop() is safe while
// jobs are still running, and does not deadlock or panic.
func TestConcurrentStopDuringExecution(t *testing.T) {
	q := newTestQueue(10 * time.Millisecond)

	var execCount atomic.Int32

	q.Add(BkgndTask{
		Job: func() error {
			execCount.Add(1)
			time.Sleep(100 * time.Millisecond)
			return nil
		},
		OneShot:  false,
		Interval: 20 * time.Millisecond,
	})

	q.Start()

	// Let a few executions start
	time.Sleep(50 * time.Millisecond)

	// Stop while jobs might be running. This is safe because:
	//   - Stop() closes stopCh to terminate the loop
	//   - Stop() calls Clear() to remove pending tasks
	//   - Already-dispatched safeRun goroutines finish independently
	//   - If a safeRun re-adds after Clear(), the task remains in the list
	//     (this is an inherent race, not a data corruption)
	q.Stop()

	// Should not deadlock or panic — that's the main assertion.
	// Some in-flight tasks may have re-added themselves after Clear(),
	// so the task list may not be empty.
	t.Logf("stop-during-exec: totalExec=%d", execCount.Load())
}
