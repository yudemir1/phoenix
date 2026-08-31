package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/monitor"
)

// fakeChecker is a test double for monitor.Checker: it returns results from
// a fixed sequence (the last one repeats once the sequence is exhausted)
// and records how many times it was called.
type fakeChecker struct {
	mu      sync.Mutex
	results []monitor.Result
	calls   int
}

func newFakeChecker(results ...monitor.Result) *fakeChecker {
	return &fakeChecker{results: results}
}

func (f *fakeChecker) Check(ctx context.Context) monitor.Result {
	f.mu.Lock()
	defer f.mu.Unlock()

	res := monitor.Result{Healthy: true}
	if len(f.results) > 0 {
		idx := f.calls
		if idx >= len(f.results) {
			idx = len(f.results) - 1
		}
		res = f.results[idx]
	}
	f.calls++
	return res
}

func (f *fakeChecker) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRunner_runOnce(t *testing.T) {
	t.Run("a_healthy_result_keeps_the_service_healthy_in_the_detector", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: true})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det)

		r.runOnce(context.Background())

		if got := det.GetState("svc"); got != detector.StateHealthy {
			t.Errorf("detector state = %s, want %s", got, detector.StateHealthy)
		}
	})

	t.Run("consecutive_failing_results_reach_down_after_the_configured_threshold", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det)

		r.runOnce(context.Background())
		r.runOnce(context.Background())
		if got := det.GetState("svc"); got != detector.StateDegraded {
			t.Fatalf("after 2 failures: detector state = %s, want %s", got, detector.StateDegraded)
		}

		r.runOnce(context.Background())
		if got := det.GetState("svc"); got != detector.StateDown {
			t.Errorf("after 3 failures: detector state = %s, want %s", got, detector.StateDown)
		}
	})

	t.Run("each_call_invokes_the_checker_exactly_once", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: true})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det)

		r.runOnce(context.Background())
		r.runOnce(context.Background())
		r.runOnce(context.Background())

		if got := checker.callCount(); got != 3 {
			t.Errorf("checker was called %d times, want 3", got)
		}
	})
}

func TestRunner_Start(t *testing.T) {
	t.Run("performs_periodic_checks_until_the_context_is_canceled", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: true})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{
			Name:          "svc",
			CheckInterval: 10 * time.Millisecond,
		}, checker, det)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			r.Start(ctx)
			close(done)
		}()

		// Let a handful of ticks fire before asking it to stop.
		time.Sleep(45 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Start did not return within 1s of the context being canceled")
		}

		if got := checker.callCount(); got < 2 {
			t.Errorf("checker was called %d times, want at least 2 during the run window", got)
		}
	})

	t.Run("returns_immediately_for_an_already_canceled_context_without_checking", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: true})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{
			Name:          "svc",
			CheckInterval: time.Hour, // long enough that a tick would never fire in this test
		}, checker, det)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		done := make(chan struct{})
		go func() {
			r.Start(ctx)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Start did not return promptly for an already-canceled context")
		}

		if got := checker.callCount(); got != 0 {
			t.Errorf("checker was called %d times, want 0 (context was already canceled)", got)
		}
	})
}
