package detector

import (
	"sync"
	"testing"

	"github.com/yudemir1/phoenix/internal/monitor"
)

func healthy() monitor.Result  { return monitor.Result{Healthy: true} }
func unhealthy() monitor.Result { return monitor.Result{Healthy: false} }

func TestDetector_GetState(t *testing.T) {
	t.Run("unknown_service_defaults_to_healthy_before_any_result_is_recorded", func(t *testing.T) {
		d := New(3)
		if got := d.GetState("never-seen"); got != StateHealthy {
			t.Errorf("GetState() = %s, want %s", got, StateHealthy)
		}
	})

	t.Run("get_state_reflects_the_most_recently_recorded_result", func(t *testing.T) {
		d := New(3)
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", unhealthy())

		if got := d.GetState("svc"); got != StateDown {
			t.Errorf("GetState() = %s, want %s", got, StateDown)
		}
	})
}

func TestDetector_RecordResult(t *testing.T) {
	t.Run("a_single_healthy_result_keeps_the_service_healthy_and_reports_no_change", func(t *testing.T) {
		d := New(3)
		state, changed := d.RecordResult("svc", healthy())

		if state != StateHealthy {
			t.Errorf("state = %s, want %s", state, StateHealthy)
		}
		if changed {
			t.Error("changed = true, want false (service was already healthy)")
		}
	})

	t.Run("failures_below_the_threshold_transition_to_degraded_not_down", func(t *testing.T) {
		d := New(3)
		state, changed := d.RecordResult("svc", unhealthy())

		if state != StateDegraded {
			t.Errorf("state = %s, want %s", state, StateDegraded)
		}
		if !changed {
			t.Error("changed = false, want true (service went from healthy to degraded)")
		}
	})

	t.Run("repeated_failures_still_below_threshold_stay_degraded_and_report_no_change", func(t *testing.T) {
		d := New(3)
		d.RecordResult("svc", unhealthy())
		state, changed := d.RecordResult("svc", unhealthy())

		if state != StateDegraded {
			t.Errorf("state = %s, want %s", state, StateDegraded)
		}
		if changed {
			t.Error("changed = true, want false (service was already degraded)")
		}
	})

	t.Run("reaching_the_failure_threshold_transitions_to_down", func(t *testing.T) {
		d := New(3)
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", unhealthy())
		state, changed := d.RecordResult("svc", unhealthy())

		if state != StateDown {
			t.Errorf("state = %s, want %s", state, StateDown)
		}
		if !changed {
			t.Error("changed = false, want true (service went from degraded to down)")
		}
	})

	t.Run("further_failures_once_down_report_no_further_change", func(t *testing.T) {
		d := New(3)
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", unhealthy())
		state, changed := d.RecordResult("svc", unhealthy())

		if state != StateDown {
			t.Errorf("state = %s, want %s", state, StateDown)
		}
		if changed {
			t.Error("changed = true, want false (service was already down)")
		}
	})

	t.Run("threshold_of_one_marks_down_on_the_very_first_failure_skipping_degraded", func(t *testing.T) {
		d := New(1)
		state, changed := d.RecordResult("svc", unhealthy())

		if state != StateDown {
			t.Errorf("state = %s, want %s", state, StateDown)
		}
		if !changed {
			t.Error("changed = false, want true")
		}
	})

	t.Run("a_single_healthy_result_immediately_recovers_from_down_to_healthy", func(t *testing.T) {
		d := New(2)
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", unhealthy()) // now down

		state, changed := d.RecordResult("svc", healthy())

		if state != StateHealthy {
			t.Errorf("state = %s, want %s", state, StateHealthy)
		}
		if !changed {
			t.Error("changed = false, want true (service recovered from down to healthy)")
		}
	})

	t.Run("a_healthy_result_resets_the_consecutive_failure_count", func(t *testing.T) {
		d := New(3)
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", unhealthy())
		d.RecordResult("svc", healthy()) // resets the counter

		// Two more failures should only reach degraded again, not down,
		// because the counter was reset by the healthy result above.
		d.RecordResult("svc", unhealthy())
		state, _ := d.RecordResult("svc", unhealthy())

		if state != StateDegraded {
			t.Errorf("state = %s, want %s", state, StateDegraded)
		}
	})

	t.Run("services_are_tracked_independently_by_name", func(t *testing.T) {
		d := New(2)
		d.RecordResult("svc-a", unhealthy())
		d.RecordResult("svc-a", unhealthy()) // svc-a is now down

		stateB, _ := d.RecordResult("svc-b", healthy())

		if stateB != StateHealthy {
			t.Errorf("svc-b state = %s, want %s (unaffected by svc-a)", stateB, StateHealthy)
		}
		if got := d.GetState("svc-a"); got != StateDown {
			t.Errorf("svc-a state = %s, want %s", got, StateDown)
		}
	})

	t.Run("concurrent_record_result_calls_do_not_race", func(t *testing.T) {
		d := New(5)
		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				d.RecordResult("svc-a", unhealthy())
			}()
			go func() {
				defer wg.Done()
				d.RecordResult("svc-b", healthy())
			}()
		}
		wg.Wait()

		// Only asserting that this ran without the race detector firing
		// and without a deadlock/panic; the exact interleaved end-state
		// is not deterministic under concurrent writes to the same key.
		_ = d.GetState("svc-a")
		_ = d.GetState("svc-b")
	})
}
