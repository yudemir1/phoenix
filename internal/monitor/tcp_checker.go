package monitor

import (
	"context"
	"fmt"
	"net"
	"time"
)

type TCPChecker struct {
	Target  string //as an host:port example: 10.0.0.5:5050
	Timeout time.Duration
}

func NewTCPChecker(target string, timeout time.Duration) *TCPChecker {
	return &TCPChecker{
		Target:  target,
		Timeout: timeout,
	}
}

func (t *TCPChecker) Check(ctx context.Context) Result {
	start := time.Now()

	dialer := net.Dialer{Timeout: t.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", t.Target)
	latency := time.Since(start)

	if err != nil {
		return Result{
			Healthy: false,
			Latency: latency,
			Err:     fmt.Errorf("tcp connection failed: %w", err),
		}
	}
	defer conn.Close()

	return Result{
		Healthy: true,
		Latency: latency,
	}
}
