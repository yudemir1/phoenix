package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/healer"
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
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det, &fakeHealer{}, nil)

		r.runOnce(context.Background())

		if got := det.GetState("svc"); got != detector.StateHealthy {
			t.Errorf("detector state = %s, want %s", got, detector.StateHealthy)
		}
	})

	t.Run("consecutive_failing_results_reach_down_after_the_configured_threshold", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		det := detector.New(3)
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det, &fakeHealer{}, nil)

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
		r := NewRunner(config.ServiceConfig{Name: "svc"}, checker, det, &fakeHealer{}, nil)

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
		}, checker, det, &fakeHealer{}, nil)

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
		}, checker, det, &fakeHealer{}, nil)

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
		r := NewRunner(downService, checker, det, h, nil)

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
		r := NewRunner(downService, checker, det, h, nil)

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
		r := NewRunner(downService, checker, det, h, nil)

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
		r := NewRunner(downService, checker, det, h, nil)

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
		r := NewRunner(downService, checker, det, h, nil)

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
		r := NewRunner(downService, checker, det, h, nil)

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
		r := NewRunner(downService, checker, det, nil, nil)

		r.runOnce(context.Background())

		if got := det.GetState("svc"); got != detector.StateDown {
			t.Errorf("detector state = %s, want %s", got, detector.StateDown)
		}
	})
}

// newCapturingLogger returns a logger that writes into buf, so tests can
// assert on what was logged and at which level.
func newCapturingLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level}))
}

func TestRunner_logging(t *testing.T) {
	downService := config.ServiceConfig{
		Name:          "svc",
		CheckInterval: time.Hour,
		Recover:       config.Recover{Strategy: "docker_restart", Target: "svc-container"},
	}

	// driveToDown records enough failures to push the service into DOWN,
	// which is what triggers the heal path.
	driveToDown := func(r *Runner, threshold int) {
		for i := 0; i < threshold; i++ {
			r.runOnce(context.Background())
		}
	}

	t.Run("a_recovery_skipped_by_policy_is_logged_as_a_warning_not_an_error", func(t *testing.T) {
		var buf bytes.Buffer
		checker := newFakeChecker(monitor.Result{Healthy: false})
		h := &fakeHealer{returnErr: fmt.Errorf("%w (2m left)", healer.ErrCooldownActive)}
		r := NewRunner(downService, checker, detector.New(1), h, newCapturingLogger(&buf, slog.LevelDebug))

		driveToDown(r, 1)

		out := buf.String()
		if !strings.Contains(out, "recovery skipped by policy") {
			t.Errorf("log did not mention the policy skip:\n%s", out)
		}
		if strings.Contains(out, "recovery failed") {
			t.Errorf("a cooldown skip must not be logged as a failure:\n%s", out)
		}
		if !strings.Contains(out, "level=WARN") {
			t.Errorf("policy skip should be logged at WARN:\n%s", out)
		}
		if strings.Contains(out, "level=ERROR") {
			t.Errorf("policy skip must not produce an ERROR line:\n%s", out)
		}
	})

	t.Run("a_recovery_that_actually_failed_is_logged_as_an_error", func(t *testing.T) {
		var buf bytes.Buffer
		checker := newFakeChecker(monitor.Result{Healthy: false})
		h := &fakeHealer{returnErr: errors.New("ssh: connection refused")}
		r := NewRunner(downService, checker, detector.New(1), h, newCapturingLogger(&buf, slog.LevelDebug))

		driveToDown(r, 1)

		out := buf.String()
		if !strings.Contains(out, "recovery failed") {
			t.Errorf("log did not report the failure:\n%s", out)
		}
		if !strings.Contains(out, "level=ERROR") {
			t.Errorf("a real recovery failure should be logged at ERROR:\n%s", out)
		}
	})

	t.Run("a_successful_recovery_is_logged_at_info_with_the_strategy", func(t *testing.T) {
		var buf bytes.Buffer
		checker := newFakeChecker(monitor.Result{Healthy: false})
		r := NewRunner(downService, checker, detector.New(1), &fakeHealer{}, newCapturingLogger(&buf, slog.LevelDebug))

		driveToDown(r, 1)

		out := buf.String()
		if !strings.Contains(out, "recovery completed") {
			t.Errorf("log did not report the successful recovery:\n%s", out)
		}
		if !strings.Contains(out, "strategy=docker_restart") {
			t.Errorf("log should carry the recovery strategy:\n%s", out)
		}
	})

	t.Run("a_transition_into_down_is_logged_at_warn", func(t *testing.T) {
		var buf bytes.Buffer
		checker := newFakeChecker(monitor.Result{Healthy: false})
		r := NewRunner(downService, checker, detector.New(1), &fakeHealer{}, newCapturingLogger(&buf, slog.LevelDebug))

		driveToDown(r, 1)

		out := buf.String()
		if !strings.Contains(out, "state changed") || !strings.Contains(out, "state=DOWN") {
			t.Errorf("log did not report the state change:\n%s", out)
		}
		if !strings.Contains(out, "level=WARN") {
			t.Errorf("a transition into DOWN should be logged at WARN:\n%s", out)
		}
	})

	t.Run("routine_checks_stay_silent_at_the_default_info_level", func(t *testing.T) {
		var buf bytes.Buffer
		checker := newFakeChecker(monitor.Result{Healthy: true})
		// Info level: a healthy check that changes nothing must produce no
		// output at all, otherwise every service floods the log each tick.
		r := NewRunner(downService, checker, detector.New(3), &fakeHealer{}, newCapturingLogger(&buf, slog.LevelInfo))

		for i := 0; i < 5; i++ {
			r.runOnce(context.Background())
		}

		if out := buf.String(); out != "" {
			t.Errorf("routine checks should be invisible at INFO, got:\n%s", out)
		}
	})

	t.Run("routine_checks_are_visible_when_debug_is_enabled", func(t *testing.T) {
		var buf bytes.Buffer
		checker := newFakeChecker(monitor.Result{Healthy: true})
		r := NewRunner(downService, checker, detector.New(3), &fakeHealer{}, newCapturingLogger(&buf, slog.LevelDebug))

		r.runOnce(context.Background())

		out := buf.String()
		if !strings.Contains(out, "check completed") || !strings.Contains(out, "level=DEBUG") {
			t.Errorf("routine check should appear at DEBUG:\n%s", out)
		}
	})

	t.Run("every_log_line_carries_the_service_name", func(t *testing.T) {
		var buf bytes.Buffer
		checker := newFakeChecker(monitor.Result{Healthy: false})
		r := NewRunner(downService, checker, detector.New(1), &fakeHealer{}, newCapturingLogger(&buf, slog.LevelDebug))

		driveToDown(r, 1)

		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if !strings.Contains(line, "service=svc") {
				t.Errorf("log line is missing the service attribute: %q", line)
			}
		}
	})

	t.Run("a_nil_logger_is_replaced_by_a_discard_logger_instead_of_panicking", func(t *testing.T) {
		checker := newFakeChecker(monitor.Result{Healthy: false})
		r := NewRunner(downService, checker, detector.New(1), &fakeHealer{}, nil)

		// Must not panic on a nil *slog.Logger.
		r.runOnce(context.Background())

		if r.logger == nil {
			t.Error("runner.logger should have been replaced with a discard logger")
		}
	})
}
