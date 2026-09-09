package healer

import (
	"strings"
	"testing"

	"github.com/yudemir1/phoenix/internal/config"
)

func TestBuildRecoveryCommand(t *testing.T) {
	t.Run("docker_restart_strategy_builds_a_docker_restart_command", func(t *testing.T) {
		cmd, err := buildRecoveryCommand(config.Recover{Strategy: "docker_restart", Target: "web-api-container"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "docker restart web-api-container"; cmd != want {
			t.Errorf("cmd = %q, want %q", cmd, want)
		}
	})

	t.Run("systemd_restart_strategy_builds_a_systemctl_restart_command", func(t *testing.T) {
		cmd, err := buildRecoveryCommand(config.Recover{Strategy: "systemd_restart", Target: "worker.service"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "sudo systemctl restart worker.service"; cmd != want {
			t.Errorf("cmd = %q, want %q", cmd, want)
		}
	})

	t.Run("custom_command_strategy_returns_the_command_as_is", func(t *testing.T) {
		cmd, err := buildRecoveryCommand(config.Recover{Strategy: "custom_command", Command: "/usr/local/bin/restart-proxy.sh"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "/usr/local/bin/restart-proxy.sh"; cmd != want {
			t.Errorf("cmd = %q, want %q", cmd, want)
		}
	})

	t.Run("custom_command_strategy_without_a_command_returns_an_error", func(t *testing.T) {
		_, err := buildRecoveryCommand(config.Recover{Strategy: "custom_command"})
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "non-empty") {
			t.Errorf("error = %q, want it to mention a non-empty command", err.Error())
		}
	})

	t.Run("unknown_strategy_returns_an_error", func(t *testing.T) {
		_, err := buildRecoveryCommand(config.Recover{Strategy: "reboot_host"})
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "Unknown recover strategy") {
			t.Errorf("error = %q, want it to mention the unknown strategy", err.Error())
		}
	})
}
