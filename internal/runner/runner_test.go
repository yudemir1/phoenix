package runner

import (
	"context"
	"errors"
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

// fakeHealer is a test double for healer.Healer: it records which services
// it was asked to heal and returns a canned error.
type fakeHealer struct {
	mu        sync.Mutex
	healed    []string
	returnErr error
}

func (f *fakeHealer) Heal(ctx context.Context, s config.ServiceConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healed = append(f.healed, s.Name)
	return f.returnErr
}

func (f *fakeHealer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.healed)
}

func (f *fakeHealer) healedServices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.healed...)
}

func TestRunner_runOnce(t *testing.T) {
	t.Run("a_healthy_result_keeps_the_service_healthy_in_the_detector", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: true})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det, &fakeHealer{})

		r.runOnce(context.Background())

		if got := det.GetState("svc"); got != detector.StateHealthy {
			t.Errorf("detector state = %s, want %s", got, detector.StateHealthy)
		}
	})

	t.Run("consecutive_failing_results_reach_down_after_the_configured_threshold", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det, &fakeHealer{})

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
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det, &fakeHealer{})

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
		}, checker, det, &fakeHealer{})

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
		}, checker, det, &fakeHealer{})

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

func TestRunner_healing(t *testing.T) {
	downService := config.ServiceConfig{
		Name:    "svc",
		Recover: config.Recover{Strategy: "docker_restart", Target: "svc-container"},
	}

	t.Run("triggers_the_healer_when_the_service_transitions_to_down", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(3)
		h := &fakeHealer{}
		r := NewRunner(downService, checker, det, h)

		for i := 0; i < 3; i++ {
			r.runOnce(context.Background())
		}

		if got := h.callCount(); got != 1 {
			t.Fatalf("healer was called %d times, want 1", got)
		}
		if got := h.healedServices(); len(got) != 1 || got[0] != "svc" {
			t.Errorf("healer was handed %v, want [svc]", got)
		}
	})

	t.Run("does_not_trigger_the_healer_again_while_the_service_is_already_down", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(3)
		h := &fakeHealer{}
		r := NewRunner(downService, checker, det, h)

		// Three failures reach DOWN; the following failures keep it there
		// without producing a new state change.
		for i := 0; i < 8; i++ {
			r.runOnce(context.Background())
		}

		if got := h.callCount(); got != 1 {
			t.Errorf("healer was called %d times, want 1 (only on the transition into DOWN)", got)
		}
	})

	t.Run("does_not_trigger_the_healer_while_the_service_is_only_degraded", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(3)
		h := &fakeHealer{}
		r := NewRunner(downService, checker, det, h)

		r.runOnce(context.Background())
		r.runOnce(context.Background())

		if got := det.GetState("svc"); got != detector.StateDegraded {
			t.Fatalf("detector state = %s, want %s", got, detector.StateDegraded)
		}
		if got := h.callCount(); got != 0 {
			t.Errorf("healer was called %d times, want 0 while only degraded", got)
		}
	})

	t.Run("does_not_trigger_the_healer_for_healthy_results", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: true})
		det := detector.New(3)
		h := &fakeHealer{}
		r := NewRunner(downService, checker, det, h)

		for i := 0; i < 5; i++ {
			r.runOnce(context.Background())
		}

		if got := h.callCount(); got != 0 {
			t.Errorf("healer was called %d times, want 0 for a healthy service", got)
		}
	})

	t.Run("triggers_the_healer_again_after_the_service_recovers_and_goes_down_once_more", func(t *testing.T) {
		checker := newFakeChecker(
			monitor.Result{Healthy: false}, // -> DOWN (threshold 1), heal #1
			monitor.Result{Healthy: true},  // -> HEALTHY
			monitor.Result{Healthy: false}, // -> DOWN again, heal #2
		)
		det := detector.New(1)
		h := &fakeHealer{}
		r := NewRunner(downService, checker, det, h)

		for i := 0; i < 3; i++ {
			r.runOnce(context.Background())
		}

		if got := h.callCount(); got != 2 {
			t.Errorf("healer was called %d times, want 2 (one per transition into DOWN)", got)
		}
	})

	t.Run("keeps_running_when_the_healer_returns_an_error", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(1)
		h := &fakeHealer{returnErr: errors.New("ssh: connection refused")}
		r := NewRunner(downService, checker, det, h)

		r.runOnce(context.Background()) // -> DOWN, healer fails
		r.runOnce(context.Background()) // must still run normally afterwards

		if got := h.callCount(); got != 1 {
			t.Errorf("healer was called %d times, want 1", got)
		}
		if got := checker.callCount(); got != 2 {
			t.Errorf("checker was called %d times, want 2 (the loop kept going after the failed heal)", got)
		}
	})

	t.Run("a_nil_healer_is_skipped_instead_of_panicking", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(1)
		r := NewRunner(downService, checker, det, nil)

		r.runOnce(context.Background())

		if got := det.GetState("svc"); got != detector.StateDown {
			t.Errorf("detector state = %s, want %s", got, detector.StateDown)
		}
	})
}
