package monitor

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPChecker_Check(t *testing.T) {
	t.Run("returns_healthy_true_when_target_port_is_open", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("could not start test listener: %v", err)
		}
		defer ln.Close()

		c := NewTCPChecker(ln.Addr().String(), time.Second)
		res := c.Check(context.Background())

		if !res.Healthy {
			t.Errorf("Healthy = false, want true (err: %v)", res.Err)
		}
		if res.Err != nil {
			t.Errorf("Err = %v, want nil", res.Err)
		}
	})

	t.Run("returns_healthy_false_and_error_when_connection_is_refused", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("could not start test listener: %v", err)
		}
		target := ln.Addr().String()
		ln.Close() // nothing is listening on this port anymore

		c := NewTCPChecker(target, time.Second)
		res := c.Check(context.Background())

		if res.Healthy {
			t.Error("Healthy = true, want false when nothing listens on the port")
		}
		if res.Err == nil {
			t.Error("Err = nil, want a non-nil connection error")
		}
	})

	t.Run("returns_healthy_false_and_error_when_context_is_already_canceled", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("could not start test listener: %v", err)
		}
		defer ln.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		c := NewTCPChecker(ln.Addr().String(), time.Second)
		res := c.Check(ctx)

		if res.Healthy {
			t.Error("Healthy = true, want false for an already-canceled context")
		}
		if res.Err == nil {
			t.Error("Err = nil, want a non-nil context-canceled error")
		}
	})
}
