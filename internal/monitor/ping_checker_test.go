package monitor

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// PingChecker shells out to the system "ping" binary, so these tests are
// skipped on machines where it isn't available (e.g. minimal CI containers).
func requirePingBinary(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skip("system 'ping' binary not found, skipping")
	}
}

func TestPingChecker_Check(t *testing.T) {
	t.Run("returns_healthy_true_for_reachable_loopback_target", func(t *testing.T) {
		requirePingBinary(t)

		c := NewPingChecker("127.0.0.1", 2*time.Second)
		res := c.Check(context.Background())

		if !res.Healthy {
			t.Errorf("Healthy = false, want true (err: %v)", res.Err)
		}
		if res.Err != nil {
			t.Errorf("Err = %v, want nil", res.Err)
		}
	})

	t.Run("returns_healthy_false_and_error_for_unreachable_target", func(t *testing.T) {
		requirePingBinary(t)

		// 203.0.113.1 is inside TEST-NET-3 (RFC 5737), reserved for
		// documentation: it is never routed, so no reply ever arrives
		// and the checker deterministically times out.
		c := NewPingChecker("203.0.113.1", time.Second)
		res := c.Check(context.Background())

		if res.Healthy {
			t.Error("Healthy = true, want false for an unreachable target")
		}
		if res.Err == nil {
			t.Error("Err = nil, want a non-nil ping error")
		}
	})
}

func TestPingWaitArg(t *testing.T) {
	cases := []struct {
		name    string
		goos    string
		timeout time.Duration
		want    string
	}{
		{"linux_uses_seconds", "linux", 5 * time.Second, "5"},
		{"darwin_uses_milliseconds", "darwin", 5 * time.Second, "5000"},
		{"freebsd_uses_milliseconds", "freebsd", 250 * time.Millisecond, "250"},
		{"sub_second_timeout_on_linux_floors_to_1_second", "linux", 200 * time.Millisecond, "1"},
		{"sub_millisecond_timeout_on_darwin_floors_to_1_millisecond", "darwin", 500 * time.Microsecond, "1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pingWaitArg(tc.goos, tc.timeout)
			if got != tc.want {
				t.Errorf("pingWaitArg(%q, %s) = %q, want %q", tc.goos, tc.timeout, got, tc.want)
			}
		})
	}
}
