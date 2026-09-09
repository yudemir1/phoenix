package healer

import (
	"fmt"
	"context"
	"github.com/yudemir1/phoenix/internal/config"
)

type Healer interface {
	Heal(ctx context.Context, s config.ServiceConfig) error
}

func buildRecoveryCommand(r config.Recover) (string, error) {
	switch r.Strategy {
	case "docker_restart":
		return fmt.Sprintf("docker restart %s", r.Target), nil
	case "systemd_restart":
		return fmt.Sprintf("sudo systemctl restart %s", r.Target), nil
	case "custom_command":
		if r.Command == "" {
			return "", fmt.Errorf("Error: custom command strategy requires a non-empty string")
		}
		return r.Command, nil
	default:
		return "", fmt.Errorf("Unknown recover strategy %q", r.Strategy)
	}
}