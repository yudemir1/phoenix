package detector

import (
	"github.com/yudemir1/phoenix/internal/monitor"
	"sync"
)

type serviceStatus struct {
	ConsecutiveFailures int
	CurrentState        State
}

type Detector struct {
	mu        sync.Mutex
	threshold int
	statuses  map[string]*serviceStatus
}

func New(threshold int) *Detector {
	return &Detector{
		threshold: threshold,
		statuses:  make(map[string]*serviceStatus),
	}
}

func (d *Detector) RecordResult(serviceName string, result monitor.Result) (newState State, changed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	status, exists := d.statuses[serviceName]
	if !exists {
		status = &serviceStatus{CurrentState: StateHealthy}
		d.statuses[serviceName] = status
	}
	previousState := status.CurrentState

	if result.Healthy {
		status.ConsecutiveFailures = 0
		status.CurrentState = StateHealthy
	} else {
		status.ConsecutiveFailures++
		if status.ConsecutiveFailures >= d.threshold {
			status.CurrentState = StateDown
		} else {
			status.CurrentState = StateDegraded
		}
	}

	return status.CurrentState, status.CurrentState != previousState
}

func (d *Detector) GetState(serviceName string) State {
	d.mu.Lock()
	defer d.mu.Unlock()

	status, exists := d.statuses[serviceName]
	if !exists {
		return StateHealthy
	}
	return status.CurrentState
}
