package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/monitor"
)

type Runner struct {
	service  config.ServiceConfig
	checker  monitor.Checker
	detector *detector.Detector
}

func NewRunner(s config.ServiceConfig, checker monitor.Checker, det *detector.Detector) *Runner {
	return &Runner{
		service:  s,
		checker:  checker,
		detector: det,
	}
}

func (r *Runner) Start(ctx context.Context) {
	ticker := time.NewTicker(r.service.CheckInterval)
	defer ticker.Stop()

	fmt.Printf("[%s] monitoring started (every %s)\n", r.service.Name, r.service.CheckInterval)

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] monitoring stopped\n", r.service.Name)
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	result := r.checker.Check(ctx)
	newState, changed := r.detector.RecordResult(r.service.Name, result)

	if changed {
		fmt.Printf("[%s] state changed -> %s (err=%v)", r.service.Name, newState, result.Err)

		if newState.String() == "DOWN" {
			fmt.Printf("[%s] healer actions will be triggered (not implemented yet)\n", r.service.Name)
		}
	} else {
		fmt.Printf("[%s] control: healthy=%v latency=%s\n", r.service.Name, result.Healthy, result.Latency)
	}
}
