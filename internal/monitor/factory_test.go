package monitor

import (
	"testing"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
)

func TestNewChecker(t *testing.T) {
	cases := []struct {
		name      string
		checkType string
		wantType  Checker
	}{
		{"http_check_type_returns_an_http_checker", "http", &HTTPChecker{}},
		{"tcp_check_type_returns_a_tcp_checker", "tcp", &TCPChecker{}},
		{"ping_check_type_returns_a_ping_checker", "ping", &PingChecker{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := config.ServiceConfig{CheckType: tc.checkType, Target: "irrelevant", Timeout: time.Second}
			checker, err := NewChecker(s)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			switch tc.wantType.(type) {
			case *HTTPChecker:
				if _, ok := checker.(*HTTPChecker); !ok {
					t.Errorf("got %T, want *HTTPChecker", checker)
				}
			case *TCPChecker:
				if _, ok := checker.(*TCPChecker); !ok {
					t.Errorf("got %T, want *TCPChecker", checker)
				}
			case *PingChecker:
				if _, ok := checker.(*PingChecker); !ok {
					t.Errorf("got %T, want *PingChecker", checker)
				}
			}
		})
	}

	t.Run("unknown_check_type_returns_an_error", func(t *testing.T) {
		s := config.ServiceConfig{CheckType: "udp", Target: "irrelevant"}
		checker, err := NewChecker(s)

		if err == nil {
			t.Fatal("expected an error for an unknown check_type, got nil")
		}
		if checker != nil {
			t.Errorf("checker should be nil on error, got %+v", checker)
		}
	})
}
