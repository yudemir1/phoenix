package healer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
)

// recordingHealer is a test double for the Healer that PolicyHealer wraps:
// it counts how many calls actually made it through the policy.
type recordingHealer struct {
	mu        sync.Mutex
	healed    []string
	returnErr error
}

func (r *recordingHealer) Heal(ctx context.Context, s config.ServiceConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healed = append(r.healed, s.Name)
	return r.returnErr
}

func (r *recordingHealer) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.healed)
}

// fakeClock lets the tests move time forward instantly instead of sleeping
// through real cooldowns and windows.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestPolicyHealer(p Policy, inner Healer) (*PolicyHealer, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	ph := NewPolicyHealer(inner, p)
	ph.now = clock.now
	return ph, clock
}

func svc(name string) config.ServiceConfig {
	return config.ServiceConfig{
		Name:    name,
		Recover: config.Recover{Strategy: "docker_restart", Target: name + "-container"},
	}
}

func TestPolicyHealer_Cooldown(t *testing.T) {
	t.Run("the_first_recovery_for_a_service_is_always_allowed", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, _ := newTestPolicyHealer(Policy{Cooldown: 5 * time.Minute, MaxAttempts: 3, AttemptWindows: 30 * time.Minute}, inner)

		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := inner.callCount(); got != 1 {
			t.Errorf("inner healer was called %d times, want 1", got)
		}
	})

	t.Run("a_second_recovery_inside_the_cooldown_is_refused_without_reaching_the_inner_healer", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: 5 * time.Minute, MaxAttempts: 3, AttemptWindows: 30 * time.Minute}, inner)

		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("first heal: unexpected error: %v", err)
		}

		clock.advance(2 * time.Minute)
		err := ph.Heal(context.Background(), svc("a"))

		if !errors.Is(err, ErrCooldownActive) {
			t.Errorf("err = %v, want it to wrap ErrCooldownActive", err)
		}
		if got := inner.callCount(); got != 1 {
			t.Errorf("inner healer was called %d times, want 1 (the refused call must not reach it)", got)
		}
	})

	t.Run("recovery_is_allowed_again_once_the_cooldown_has_passed", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: 5 * time.Minute, MaxAttempts: 0, AttemptWindows: 30 * time.Minute}, inner)

		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("first heal: unexpected error: %v", err)
		}

		clock.advance(5 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("second heal after the cooldown: unexpected error: %v", err)
		}

		if got := inner.callCount(); got != 2 {
			t.Errorf("inner healer was called %d times, want 2", got)
		}
	})

	t.Run("a_zero_cooldown_disables_the_cooldown_check", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, _ := newTestPolicyHealer(Policy{Cooldown: 0, MaxAttempts: 0, AttemptWindows: 30 * time.Minute}, inner)

		for i := 0; i < 3; i++ {
			if err := ph.Heal(context.Background(), svc("a")); err != nil {
				t.Fatalf("heal %d: unexpected error: %v", i+1, err)
			}
		}
		if got := inner.callCount(); got != 3 {
			t.Errorf("inner healer was called %d times, want 3", got)
		}
	})
}

func TestPolicyHealer_MaxAttempts(t *testing.T) {
	t.Run("recovery_stops_once_max_attempts_inside_the_window_is_reached", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: 5 * time.Minute, MaxAttempts: 3, AttemptWindows: 60 * time.Minute}, inner)

		for i := 0; i < 3; i++ {
			if err := ph.Heal(context.Background(), svc("a")); err != nil {
				t.Fatalf("heal %d: unexpected error: %v", i+1, err)
			}
			clock.advance(6 * time.Minute)
		}

		err := ph.Heal(context.Background(), svc("a"))
		if !errors.Is(err, ErrMaxAttemptsExceeded) {
			t.Errorf("err = %v, want it to wrap ErrMaxAttemptsExceeded", err)
		}
		if got := inner.callCount(); got != 3 {
			t.Errorf("inner healer was called %d times, want 3", got)
		}
	})

	t.Run("a_zero_max_attempts_means_unlimited_attempts", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: time.Minute, MaxAttempts: 0, AttemptWindows: time.Hour}, inner)

		for i := 0; i < 10; i++ {
			if err := ph.Heal(context.Background(), svc("a")); err != nil {
				t.Fatalf("heal %d: unexpected error: %v", i+1, err)
			}
			clock.advance(2 * time.Minute)
		}
		if got := inner.callCount(); got != 10 {
			t.Errorf("inner healer was called %d times, want 10", got)
		}
	})

	t.Run("attempts_that_age_out_of_the_window_free_up_new_recoveries", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: 5 * time.Minute, MaxAttempts: 2, AttemptWindows: 30 * time.Minute}, inner)

		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("heal 1: unexpected error: %v", err)
		}
		clock.advance(6 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("heal 2: unexpected error: %v", err)
		}

		// Third attempt is over the limit while both earlier ones are still
		// inside the 30m window.
		clock.advance(6 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); !errors.Is(err, ErrMaxAttemptsExceeded) {
			t.Fatalf("heal 3: err = %v, want ErrMaxAttemptsExceeded", err)
		}

		// Move past the window so both recorded attempts expire.
		clock.advance(31 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Errorf("heal after the window elapsed: unexpected error: %v", err)
		}
		if got := inner.callCount(); got != 3 {
			t.Errorf("inner healer was called %d times, want 3", got)
		}
	})

	t.Run("a_refused_attempt_does_not_consume_one_of_the_allowed_attempts", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: 10 * time.Minute, MaxAttempts: 2, AttemptWindows: 15 * time.Minute}, inner)

		// t+0: allowed, recorded.
		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("heal 1: unexpected error: %v", err)
		}

		// t+11m: allowed, recorded. The t+0 attempt is still in the window.
		clock.advance(11 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("heal 2: unexpected error: %v", err)
		}

		// t+16m: refused by the cooldown. The t+0 attempt has now aged out of
		// the 15m window, so only the t+11m attempt should remain recorded.
		clock.advance(5 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); !errors.Is(err, ErrCooldownActive) {
			t.Fatalf("heal 3: err = %v, want ErrCooldownActive", err)
		}

		// t+22m: cooldown has passed and only one attempt (t+11m) is inside
		// the window, so this must be allowed.
		clock.advance(6 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Errorf("heal 4: unexpected error: %v (a refused attempt must not count towards MaxAttempts)", err)
		}
		if got := inner.callCount(); got != 3 {
			t.Errorf("inner healer was called %d times, want 3", got)
		}
	})
}

func TestPolicyHealer_PerService(t *testing.T) {
	t.Run("services_are_rate_limited_independently_of_each_other", func(t *testing.T) {
		inner := &recordingHealer{}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: 5 * time.Minute, MaxAttempts: 1, AttemptWindows: 30 * time.Minute}, inner)

		if err := ph.Heal(context.Background(), svc("a")); err != nil {
			t.Fatalf("service a: unexpected error: %v", err)
		}
		// Service b has its own budget, so it is allowed immediately.
		if err := ph.Heal(context.Background(), svc("b")); err != nil {
			t.Fatalf("service b: unexpected error: %v", err)
		}

		clock.advance(6 * time.Minute)
		if err := ph.Heal(context.Background(), svc("a")); !errors.Is(err, ErrMaxAttemptsExceeded) {
			t.Errorf("service a: err = %v, want ErrMaxAttemptsExceeded", err)
		}
		if got := inner.callCount(); got != 2 {
			t.Errorf("inner healer was called %d times, want 2", got)
		}
	})
}

func TestPolicyHealer_InnerError(t *testing.T) {
	t.Run("an_error_from_the_wrapped_healer_is_returned_unchanged", func(t *testing.T) {
		sentinel := errors.New("ssh: connection refused")
		inner := &recordingHealer{returnErr: sentinel}
		ph, _ := newTestPolicyHealer(Policy{Cooldown: 5 * time.Minute, MaxAttempts: 3, AttemptWindows: 30 * time.Minute}, inner)

		err := ph.Heal(context.Background(), svc("a"))
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, want the inner healer's error", err)
		}
	})

	t.Run("a_failed_recovery_still_counts_as_an_attempt", func(t *testing.T) {
		inner := &recordingHealer{returnErr: errors.New("boom")}
		ph, clock := newTestPolicyHealer(Policy{Cooldown: time.Minute, MaxAttempts: 2, AttemptWindows: time.Hour}, inner)

		_ = ph.Heal(context.Background(), svc("a"))
		clock.advance(2 * time.Minute)
		_ = ph.Heal(context.Background(), svc("a"))
		clock.advance(2 * time.Minute)

		if err := ph.Heal(context.Background(), svc("a")); !errors.Is(err, ErrMaxAttemptsExceeded) {
			t.Errorf("err = %v, want ErrMaxAttemptsExceeded (failed recoveries must still be counted)", err)
		}
	})
}

func TestPolicyHealer_Concurrency(t *testing.T) {
	t.Run("concurrent_heal_calls_for_many_services_are_race_free", func(t *testing.T) {
		inner := &recordingHealer{}
		// Real clock here: fakeClock is not safe to share across goroutines.
		ph := NewPolicyHealer(inner, Policy{Cooldown: time.Hour, MaxAttempts: 1, AttemptWindows: time.Hour})

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_ = ph.Heal(context.Background(), svc("a"))
			}()
			go func() {
				defer wg.Done()
				_ = ph.Heal(context.Background(), svc("b"))
			}()
		}
		wg.Wait()

		// With MaxAttempts=1 and a one-hour cooldown, each service must have
		// gotten through exactly once no matter how the calls interleaved.
		if got := inner.callCount(); got != 2 {
			t.Errorf("inner healer was called %d times, want 2 (once per service)", got)
		}
	})
}
