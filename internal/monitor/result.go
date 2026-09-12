package monitor

import (
	"time"
)

type Result struct {
	Healthy    bool
	Latency    time.Duration
	StatusCode int
	Err        error
}
