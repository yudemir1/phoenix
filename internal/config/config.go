package config

import (
	"fmt"
	"os"
	"time"
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
}

type ServiceConfig struct {
	Name string `yaml:"name"`

	CheckType string `yaml:"check_type"`
	Target    string `yaml:"target"`

	CheckInterval    time.Duration `yaml:"check_interval,omitempty"`
	FailureThreshold int           `yaml:"failure_threshold,omitempty"`

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

