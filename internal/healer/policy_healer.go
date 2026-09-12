package healer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yudemir1/phoenix/internal/config"
)

var (
	// ErrCooldownActive means a recovery for this service ran too recently
	ErrCooldownActive = errors.New("recovery skipped: cooldown still active")
	// ErrMaxAttemptsExceeded means recovery kept being requested for this
	// service without it staying healthy, so we stopped retrying.
	ErrMaxAttemptsExceeded = errors.New("recovery skipped: too many attempts")
)

// policy limits how often recovery may run for a single service
type Policy struct {
	Cooldown       time.Duration
	MaxAttempts    int
	AttemptWindows time.Duration
}

type PolicyHealer struct {
	inner    Healer
	policy   Policy
	now      func() time.Time
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func NewPolicyHealer(inner Healer, p Policy) *PolicyHealer {
	return &PolicyHealer{
		inner:    inner,
		policy:   p,
		now:      time.Now,
		attempts: make(map[string][]time.Time),
	}
}

func (p *PolicyHealer) Heal(ctx context.Context, s config.ServiceConfig) error {
	if err := p.reserve(s.Name); err != nil {
		return err
	}
	return p.inner.Heal(ctx, s)
}

func (p *PolicyHealer) reserve(serviceName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	attempts := p.recent(serviceName, now)

	if len(attempts) > 0 && p.policy.Cooldown > 0 {
		since := now.Sub(attempts[len(attempts)-1])
		if since < p.policy.Cooldown {
			return fmt.Errorf("%w (%s left)", ErrCooldownActive, p.policy.Cooldown-since)
		}
	}

	if p.policy.MaxAttempts > 0 && len(attempts) >= p.policy.MaxAttempts {
		return fmt.Errorf("%w (%d in the last %s)", ErrMaxAttemptsExceeded, len(attempts), p.policy.AttemptWindows)
	}

	p.attempts[serviceName] = append(attempts, now)
	return nil
}

func (p *PolicyHealer) recent(serviceName string, now time.Time) []time.Time {
	attempts := p.attempts[serviceName]
	if p.policy.AttemptWindows <= 0 {
		return attempts
	}

	cutoff := now.Add(-p.policy.AttemptWindows)
	kept := make([]time.Time, 0, len(attempts))
	for _, at := range attempts {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	return kept
}
