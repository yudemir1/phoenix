package monitor

import (
	"context"
	"fmt"
	"time"
	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
)

type Runner struct {
	service	config.ServiceConfig
	checker	Checker
	detector	*detector.Detector
}

func NewRunner(s config.ServiceConfig, checker Checker, det *detector.Detector) *Runner {
	return &Runner {
		service: s,
		checker: checker,
		detector: det,
	}
}

func (r *Runner) Start(ctx context.Context) {
	ticker := time.NewTicker(r.service.CheckInterval)
	defer ticker.Stop()

	fmt.Printf("[%s] monitoring started (every %s)\n", r.service.Name, r.service.CheckInterval)

	for {
		select {
		case <- ctx.Done():
			fmt.Printf("[%s] monitoring stopped\n", r.service.Name)
			return
		case <- ticker.C:
			r.runOnce(ctx)
		}
	}
}

//func (r *Runner) runOnce(ctx context.Context)