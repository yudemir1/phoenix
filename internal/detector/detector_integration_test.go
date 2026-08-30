package detector

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/yudemir1/phoenix/internal/monitor"
)

// listenOn opens a TCP listener on addr, retrying briefly if the port isn't
// immediately reusable (can happen right after a previous listener on the
// same address was closed).
func listenOn(t *testing.T, addr string) net.Listener {
	t.Helper()
	var lastErr error
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("could not listen on %s: %v", addr, lastErr)
	return nil
}

// This test runs a real TCP listener on localhost to stand in for a
// monitored service, and drives it through monitor.TCPChecker + Detector
// exactly the way the real check loop would: it does NOT fabricate
// monitor.Result values. The "outage" is a real outage — the listener is
// actually closed — and "recovery" is a real listener coming back up on the
// same address.
func TestDetector_DetectsARealLocalhostServiceGoingDownAndRecovering(t *testing.T) {
	t.Run("real_localhost_service_going_down_and_recovering_drives_the_expected_state_transitions", func(t *testing.T) {
		const serviceName = "local-test-service"
		const threshold = 3

		ln := listenOn(t, "127.0.0.1:0")
		addr := ln.Addr().String()

		checker := monitor.NewTCPChecker(addr, 200*time.Millisecond)
		det := New(threshold)

		check := func() monitor.Result {
			return checker.Check(context.Background())
		}

		// 1. Service is up: should be (and stay) healthy.
		res := check()
		if !res.Healthy {
			t.Fatalf("setup: expected the freshly started listener to be reachable, err: %v", res.Err)
		}
		state, changed := det.RecordResult(serviceName, res)
		if state != StateHealthy || changed {
			t.Fatalf("after first healthy check: state=%s changed=%v, want %s/false", state, changed, StateHealthy)
		}

		// 2. Simulate a real outage by actually closing the listener.
		if err := ln.Close(); err != nil {
			t.Fatalf("failed to close listener to simulate outage: %v", err)
		}

		// First failure -> degraded, reported as a change.
		state, changed = det.RecordResult(serviceName, check())
		if state != StateDegraded || !changed {
			t.Errorf("after 1st failure: state=%s changed=%v, want %s/true", state, changed, StateDegraded)
		}

		// Second failure -> still degraded (threshold not reached yet), no change.
		state, changed = det.RecordResult(serviceName, check())
		if state != StateDegraded || changed {
			t.Errorf("after 2nd failure: state=%s changed=%v, want %s/false", state, changed, StateDegraded)
		}

		// Third failure reaches the threshold -> down, reported as a change.
		state, changed = det.RecordResult(serviceName, check())
		if state != StateDown || !changed {
			t.Errorf("after 3rd failure: state=%s changed=%v, want %s/true", state, changed, StateDown)
		}
		if got := det.GetState(serviceName); got != StateDown {
			t.Errorf("GetState() = %s, want %s", got, StateDown)
		}

		// 3. Simulate recovery: bring a real listener back up on the exact
		// same address the checker already points at.
		ln2 := listenOn(t, addr)
		defer ln2.Close()

		state, changed = det.RecordResult(serviceName, check())
		if state != StateHealthy || !changed {
			t.Errorf("after recovery: state=%s changed=%v, want %s/true", state, changed, StateHealthy)
		}
		if got := det.GetState(serviceName); got != StateHealthy {
			t.Errorf("GetState() after recovery = %s, want %s", got, StateHealthy)
		}
	})
}
