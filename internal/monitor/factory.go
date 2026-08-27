package monitor

import (
	"fmt"
	"github.com/yudemir1/phoenix/internal/config"
)

func NewChecker(s config.ServiceConfig) (Checker, error) {
	switch s.CheckType {
	case "http":
		return NewHTTPChecker(s.Target, s.HTTPTimeout), nil
	//case "ping":
		//return ...
	//case "tcp":
		// return ...
	default:
		return nil, fmt.Errorf("Error: Unknown check_type: %q", s.CheckType)
	}
}