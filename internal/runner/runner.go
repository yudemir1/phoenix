package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/healer"
	"github.com/yudemir1/phoenix/internal/monitor"
)

type Runner struct {
	service  config.ServiceConfig
	checker  monitor.Checker
	detector *detector.Detector
	healer   healer.Healer
}

func NewRunner(s config.ServiceConfig, checker monitor.Checker, det *detector.Detector, h healer.Healer) *Runner {
	return &Runner{
		service:  s,
		checker:  checker,
		detector: det,
		healer:   h,
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
		fmt.Printf("[%s] state changed -> %s (err=%v)\n", r.service.Name, newState, result.Err)

		if newState == detector.StateDown {
			r.heal(ctx)
		}
	} else {
		fmt.Printf("[%s] control: healthy=%v latency=%s\n", r.service.Name, result.Healthy, result.Latency)
	}
}

func (r *Runner) heal(ctx context.Context) {
	if r.healer == nil {
		fmt.Printf("[%s] no healer configured, skipping recovery\n", r.service.Name)
		return
	}

	fmt.Printf("[%s] triggering recovery (%s)\n", r.service.Name, r.service.Recover.Strategy)
	if err := r.healer.Heal(ctx, r.service); err != nil {
		fmt.Printf("[%s] recovery failed: %v\n", r.service.Name, err)
		return
	}
	fmt.Printf("[%s] recovery command completed\n", r.service.Name)
}
