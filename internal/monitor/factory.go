package monitor

import (
	"fmt"
	"github.com/yudemir1/phoenix/internal/config"
)

func NewChecker(s config.ServiceConfig) (Checker, error) {
	switch s.CheckType {
	case "http":
		return NewHTTPChecker(s.Target, s.Timeout), nil
	case "ping":
		return NewPingChecker(s.Target, s.Timeout), nil
	case "tcp":
		return NewTCPChecker(s.Target, s.Timeout), nil
	default:
		return nil, fmt.Errorf("Error: Unknown check_type: %q", s.CheckType)
	}
}
