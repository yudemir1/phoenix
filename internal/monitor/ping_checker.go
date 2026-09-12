package monitor

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

type PingChecker struct {
	Target  string
	Timeout time.Duration
}

func NewPingChecker(target string, timeout time.Duration) *PingChecker {
	return &PingChecker{
		Target:  target,
		Timeout: timeout,
	}
}

func (p *PingChecker) Check(ctx context.Context) Result {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	// -c 1: sends a single packet, -W: per-reply wait time (unit depends
	// on the platform, see pingWaitArg).
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", pingWaitArg(runtime.GOOS, p.Timeout), p.Target)
	err := cmd.Run()
	latency := time.Since(start)

	if err != nil {
		return Result{
			Healthy: false,
			Latency: latency,
			Err:     fmt.Errorf("ping failed: %w", err),
		}
	}

	return Result{
		Healthy: true,
		Latency: latency,
	}
}

// pingWaitArg returns the value for ping's -W flag in the unit expected by
// the given platform's ping binary: iputils ping (Linux and most others)
// takes seconds, while BSD-family ping (macOS, FreeBSD, OpenBSD, NetBSD)
// takes milliseconds. goos is passed in (rather than read from runtime
// directly) so this logic can be unit tested for every platform branch
// regardless of which OS the tests actually run on.
func pingWaitArg(goos string, timeout time.Duration) string {
	switch goos {
	case "darwin", "freebsd", "openbsd", "netbsd":
		ms := int(timeout.Milliseconds())
		if ms < 1 {
			ms = 1
		}
		return fmt.Sprintf("%d", ms)
	default:
		sec := int(timeout.Seconds())
		if sec < 1 {
			sec = 1
		}
		return fmt.Sprintf("%d", sec)
	}
}
