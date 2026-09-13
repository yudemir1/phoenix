package notifier

import (
	"context"
	"fmt"
	"time"
)

type EventType string

const (
	EventStateChanged      EventType = "state_changed"
	EventRecoverySucceeded EventType = "recovery_succeeded"
	EventRecoveryFailed    EventType = "recovery_failed"
	EventRecoverySkipped   EventType = "recovery_skipped"
)

type Event struct {
	Type     EventType
	Service  string
	State    string
	Strategy string
	Err      string
	Time     time.Time
}

type Notifier interface {
	Notify(ctx context.Context, ev Event) error
}

func (e Event) Text() string {
	switch e.Type {
	case EventStateChanged:
		return fmt.Sprintf("%s is now %s", e.Service, e.State)
	case EventRecoverySucceeded:
		return fmt.Sprintf("%s: recovery succeeded (%s)", e.Service, e.Strategy)
	case EventRecoveryFailed:
		return fmt.Sprintf("%s: recovery FAILED (%s): %s", e.Service, e.Strategy, e.Err)
	case EventRecoverySkipped:
		return fmt.Sprintf("%s: recovery skipped by policy: %s", e.Service, e.Err)
	default:
		return fmt.Sprintf("%s: %s", e.Service, e.Type)
	}
}
