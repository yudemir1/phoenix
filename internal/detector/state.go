package detector

type State int

const (
	StateHealthy State = iota
	StateDegraded
	StateDown
)

func (s State) String() string {
	switch s {
	case StateHealthy:
		return "HEALTHY"
	case StateDegraded:
		return "DEGRADED"
	case StateDown:
		return "DOWN"
	default:
		return "UNKNOWN"
	}
}
