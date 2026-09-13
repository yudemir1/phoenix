package runner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/healer"
	"github.com/yudemir1/phoenix/internal/monitor"
	"github.com/yudemir1/phoenix/internal/notifier"
)

type Runner struct {
	service  config.ServiceConfig
	checker  monitor.Checker
	detector *detector.Detector
	healer   healer.Healer
	logger   *slog.Logger
	notifier notifier.Notifier
}

func NewRunner(s config.ServiceConfig, checker monitor.Checker, det *detector.Detector, h healer.Healer, logger *slog.Logger, n notifier.Notifier) *Runner {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Runner{
		service:  s,
		checker:  checker,
		detector: det,
		healer:   h,
		logger:   logger.With("service", s.Name),
		notifier: n,
	}
}

func (r *Runner) Start(ctx context.Context) {
	ticker := time.NewTicker(r.service.CheckInterval)
	defer ticker.Stop()

	r.logger.Info("monitoring started", "interval", r.service.CheckInterval, "check_type", r.service.CheckType)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("monitoring stopped")
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	result := r.checker.Check(ctx)
	newState, changed := r.detector.RecordResult(r.service.Name, result)

	if !changed {
		r.logger.Debug("check completed", "healthy", result.Healthy, "latency", result.Latency)
		return
	}

	level := slog.LevelInfo
	if newState != detector.StateHealthy {
		level = slog.LevelWarn
	}
	r.logger.Log(ctx, level, "state changed", "state", newState.String(), "err", result.Err)

	r.notify(ctx, notifier.Event{
		Type:    notifier.EventStateChanged,
		Service: r.service.Name,
		State:   newState.String(),
		Err:     errText(result.Err),
	})

	if newState == detector.StateDown {
		r.heal(ctx)
	}
}

func (r *Runner) heal(ctx context.Context) {
	if r.healer == nil {
		r.logger.Warn("no healer configured, skipping recovery")
		return
	}

	err := r.healer.Heal(ctx, r.service)
	switch {
	case err == nil:
		r.logger.Info("recovery completed", "strategy", r.service.Recover.Strategy, "target", r.service.Recover.Target)
		r.notify(ctx, notifier.Event{
			Type:     notifier.EventRecoverySucceeded,
			Service:  r.service.Name,
			Strategy: r.service.Recover.Strategy,
		})

	case errors.Is(err, healer.ErrCooldownActive), errors.Is(err, healer.ErrMaxAttemptsExceeded):
		r.logger.Warn("recovery skipped by policy", "reason", err)
		r.notify(ctx, notifier.Event{
			Type:     notifier.EventRecoverySkipped,
			Service:  r.service.Name,
			Strategy: r.service.Recover.Strategy,
			Err:      errText(err),
		})

	default:
		r.logger.Error("recovery failed", "strategy", r.service.Recover.Strategy, "err", err)
		r.notify(ctx, notifier.Event{
			Type:     notifier.EventRecoveryFailed,
			Service:  r.service.Name,
			Strategy: r.service.Recover.Strategy,
			Err:      errText(err),
		})
	}
}

func (r *Runner) notify(ctx context.Context, ev notifier.Event) {
	if r.notifier == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	if err := r.notifier.Notify(ctx, ev); err != nil {
		r.logger.Warn("could not deliver notification", "event", string(ev.Type), "err", err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
