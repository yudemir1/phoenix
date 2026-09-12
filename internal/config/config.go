package config

import (
	"fmt"
	"os"
	"time"
	"bytes"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Global   GlobalConfig    `yaml:"global"`
	Services []ServiceConfig `yaml:"services"`
}

type GlobalConfig struct {
	CheckInterval    time.Duration `yaml:"check_interval"`
	FailureThreshold int           `yaml:"failure_threshold"`
	SSHTimeout       time.Duration `yaml:"ssh_timeout"`
	Timeout          time.Duration `yaml:"timeout"`

	RecoveryCooldown      time.Duration `yaml:"recovery_cooldown"`
	RecoveryMaxAttempts   int           `yaml:"recovery_max_attempts"`
	RecoveryAttemptWindow time.Duration `yaml:"recovery_attempt_window"`
}

type ServiceConfig struct {
	Name string `yaml:"name"`

	CheckType string `yaml:"check_type"`
	Target    string `yaml:"target"`

	CheckInterval    time.Duration `yaml:"check_interval,omitempty"`
	FailureThreshold int           `yaml:"failure_threshold,omitempty"`
	Timeout          time.Duration `yaml:"timeout,omitempty"`

	SSH     SSHConfig `yaml:"ssh"`
	Recover Recover   `yaml:"recover"`
}

type SSHConfig struct {
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
	User    string `yaml:"user"`
	KeyPath string `yaml:"key_path"`
}

type Recover struct {
	Strategy string `yaml:"strategy"` // "docker_restart" | "systemd_restart" | "custom_command"
	Target   string `yaml:"target"`
	Command  string `yaml:"command,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Error: Config file can't be read. (%s): %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("Error: Config couldn't parsed: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("Config couldn't be validated: %w", err)
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Global.RecoveryCooldown == 0 {
		c.Global.RecoveryCooldown = 5 * time.Minute
	}
	if c.Global.RecoveryMaxAttempts == 0 {
		c.Global.RecoveryMaxAttempts = 3
	}
	if c.Global.RecoveryAttemptWindow == 0 {
		c.Global.RecoveryAttemptWindow = 30 * time.Minute
	}

	for i := range c.Services {
		s := &c.Services[i]
		if s.CheckInterval == 0 {
			s.CheckInterval = c.Global.CheckInterval
		}
		if s.FailureThreshold == 0 {
			s.FailureThreshold = c.Global.FailureThreshold
		}
		if s.Timeout == 0 {
			s.Timeout = c.Global.Timeout
		}
	}
}

func (c *Config) validate() error {
	if len(c.Services) == 0 {
		return fmt.Errorf("Error: at least one service should be declared")
	}
	validCheckTypes := map[string]bool{"http": true, "ping": true, "tcp": true}
	validStrategies := map[string]bool{"docker_restart": true, "systemd_restart": true, "custom_command": true}
	seenNames := map[string]bool{}

	if c.Global.RecoveryCooldown < 0 {
		return fmt.Errorf("Error: global.recovery_cooldown can't be negative")
	}
	if c.Global.RecoveryMaxAttempts < 0 {
		return fmt.Errorf("Error: global.recovery_max_attempts can't be negative")
	}
	if c.Global.RecoveryAttemptWindow < 0 {
		return fmt.Errorf("Error: global.recovery_attempt_window can't be negative")
	}

	for _, s := range c.Services {
		if s.Name == "" {
			return fmt.Errorf("Error: A service's 'name' field is empty.")
		}
		if seenNames[s.Name] {
			return fmt.Errorf("Error: Service %q: duplicate service name", s.Name)
		}
		seenNames[s.Name] = true

		if !validCheckTypes[s.CheckType] {
			return fmt.Errorf("Error: Service %q: Non-valid check_type %q", s.Name, s.CheckType)
		}
		if s.Target == "" {
			return fmt.Errorf("Error: Service %q: 'target' field can't be empty", s.Name)
		}
		if s.SSH.Host == "" {
			return fmt.Errorf("Error: Service %q: ssh.host field can't be empty", s.Name)
		}
		if s.SSH.Port < 1 || s.SSH.Port > 65535 {
			return fmt.Errorf("Error: Service %q: ssh.port must be between 1 and 65535, got %d", s.Name, s.SSH.Port)
		}
		if !validStrategies[s.Recover.Strategy] {
			return fmt.Errorf("Error: Service %q: Illegal recover.strategy %q", s.Name, s.Recover.Strategy)
		}
		if s.Recover.Strategy == "custom_command" && s.Recover.Command == "" {
			return fmt.Errorf("Error: Service %q: recover.command can't be empty when recover.strategy is custom_command", s.Name)
		}
		if s.CheckInterval <= 0 {
			return fmt.Errorf("Error: Service %q: check_interval must be greater than 0 (set it on the service or on global)", s.Name)
		}
		if s.FailureThreshold <= 0 {
			return fmt.Errorf("Error: Service %q: failure_threshold must be greater than 0 (set it on the service or on global)", s.Name)
		}
	}
	return nil //config is valid
}
