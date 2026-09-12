package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Files under testdata/ represent different kinds of configs: valid ones
// (single service, multiple services, inherited defaults) and invalid ones
// that each trigger one specific error branch in validate() and Load().

func TestLoad_valid_configs_load_without_error(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"single_http_service_config_loads_successfully", "testdata/valid_single_service.yml"},
		{"multiple_services_with_mixed_http_ping_tcp_load_successfully", "testdata/valid_multiple_services.yml"},
		{"service_without_check_interval_or_threshold_inherits_global_values", "testdata/valid_defaults_inherited.yml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.file)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg == nil {
				t.Fatal("cfg should not be nil")
			}
			if len(cfg.Services) == 0 {
				t.Fatal("at least one service should have been loaded")
			}
		})
	}
}

func TestLoad_each_service_in_a_multi_service_config_is_parsed_correctly(t *testing.T) {
	t.Run("each_services_check_type_and_recover_strategy_are_read_correctly", func(t *testing.T) {
		cfg, err := Load("testdata/valid_multiple_services.yml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(cfg.Services) != 3 {
			t.Fatalf("expected 3 services, got %d", len(cfg.Services))
		}

		want := map[string]struct {
			checkType string
			strategy  string
		}{
			"web-api-1":       {"http", "docker_restart"},
			"internal-worker": {"ping", "systemd_restart"},
			"db-proxy":        {"tcp", "custom_command"},
		}

		for _, s := range cfg.Services {
			w, ok := want[s.Name]
			if !ok {
				t.Fatalf("unexpected service name: %s", s.Name)
			}
			if s.CheckType != w.checkType {
				t.Errorf("%s: check_type = %q, want %q", s.Name, s.CheckType, w.checkType)
			}
			if s.Recover.Strategy != w.strategy {
				t.Errorf("%s: recover.strategy = %q, want %q", s.Name, s.Recover.Strategy, w.strategy)
			}
		}
	})
}

func TestLoad_global_defaults_are_applied_to_services_that_omit_them(t *testing.T) {
	t.Run("check_interval_and_failure_threshold_are_inherited_from_global_with_correct_value", func(t *testing.T) {
		cfg, err := Load("testdata/valid_defaults_inherited.yml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		s := cfg.Services[0]
		if s.CheckInterval != cfg.Global.CheckInterval {
			t.Errorf("check_interval was not inherited from global: got %s, want %s", s.CheckInterval, cfg.Global.CheckInterval)
		}
		if s.FailureThreshold != cfg.Global.FailureThreshold {
			t.Errorf("failure_threshold was not inherited from global: got %d, want %d", s.FailureThreshold, cfg.Global.FailureThreshold)
		}
		if cfg.Global.CheckInterval != 15*time.Second {
			t.Errorf("global.check_interval expected 15s, got %s", cfg.Global.CheckInterval)
		}
	})
}

func TestLoad_service_specific_check_interval_is_not_overridden(t *testing.T) {
	t.Run("service_level_check_interval_and_failure_threshold_are_preserved", func(t *testing.T) {
		cfg, err := Load("testdata/valid_multiple_services.yml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, s := range cfg.Services {
			if s.Name == "internal-worker" {
				if s.CheckInterval != 5*time.Second {
					t.Errorf("service-specific check_interval was overridden by global: got %s, want 5s", s.CheckInterval)
				}
				if s.FailureThreshold != 2 {
					t.Errorf("service-specific failure_threshold was overridden by global: got %d, want 2", s.FailureThreshold)
				}
			}
		}
	})
}

func TestLoad_invalid_configs_return_an_error(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		errContain string
	}{
		{"empty_services_list_is_rejected", "testdata/invalid_empty_services.yml", "at least one service"},
		{"empty_service_name_is_rejected", "testdata/invalid_missing_name.yml", "name' field is empty"},
		{"unknown_check_type_is_rejected", "testdata/invalid_bad_check_type.yml", "Non-valid check_type"},
		{"empty_target_field_is_rejected", "testdata/invalid_empty_target.yml", "target' field can't be empty"},
		{"empty_ssh_host_is_rejected", "testdata/invalid_missing_ssh_host.yml", "ssh.host field can't be empty"},
		{"unknown_recover_strategy_is_rejected", "testdata/invalid_bad_recover_strategy.yml", "Illegal recover.strategy"},
		{"malformed_yaml_syntax_returns_a_parse_error", "testdata/invalid_malformed_yaml.yml", "couldn't parsed"},
		{"nonexistent_file_path_returns_an_error", "testdata/does-not-exist.yml", "can't be read"},
		{"duplicate_service_names_are_rejected", "testdata/invalid_duplicate_service_name.yml", "duplicate service name"},
		{"custom_command_strategy_without_a_command_is_rejected", "testdata/invalid_custom_command_missing_command.yml", "recover.command can't be empty"},
		{"ssh_port_out_of_range_is_rejected", "testdata/invalid_ssh_port_out_of_range.yml", "ssh.port must be between 1 and 65535"},
		{"check_interval_left_at_zero_after_defaults_is_rejected", "testdata/invalid_zero_check_interval.yml", "check_interval must be greater than 0"},
		{"failure_threshold_left_at_zero_after_defaults_is_rejected", "testdata/invalid_zero_failure_threshold.yml", "failure_threshold must be greater than 0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.file)
			if err == nil {
				t.Fatalf("expected an error but got nil (cfg: %+v)", cfg)
			}
			if !strings.Contains(err.Error(), tc.errContain) {
				t.Errorf("error message should contain %q, but got: %q", tc.errContain, err.Error())
			}
			if cfg != nil {
				t.Errorf("cfg should be nil on error, got %+v", cfg)
			}
		})
	}
}

func TestLoad_recovery_policy_settings(t *testing.T) {
	t.Run("explicit_recovery_policy_values_are_read_from_the_file", func(t *testing.T) {
		cfg, err := Load("testdata/valid_recovery_policy.yml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if want := 10 * time.Minute; cfg.Global.RecoveryCooldown != want {
			t.Errorf("RecoveryCooldown = %s, want %s (is the yaml tag spelled correctly?)", cfg.Global.RecoveryCooldown, want)
		}
		if want := 7; cfg.Global.RecoveryMaxAttempts != want {
			t.Errorf("RecoveryMaxAttempts = %d, want %d (is the yaml tag spelled correctly?)", cfg.Global.RecoveryMaxAttempts, want)
		}
		if want := 2 * time.Hour; cfg.Global.RecoveryAttemptWindow != want {
			t.Errorf("RecoveryAttemptWindow = %s, want %s (is the yaml tag spelled correctly?)", cfg.Global.RecoveryAttemptWindow, want)
		}
	})

	t.Run("omitted_recovery_policy_values_fall_back_to_the_built_in_defaults", func(t *testing.T) {
		cfg, err := Load("testdata/valid_recovery_policy_defaults.yml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if want := 5 * time.Minute; cfg.Global.RecoveryCooldown != want {
			t.Errorf("RecoveryCooldown = %s, want the %s default", cfg.Global.RecoveryCooldown, want)
		}
		if want := 3; cfg.Global.RecoveryMaxAttempts != want {
			t.Errorf("RecoveryMaxAttempts = %d, want the %d default", cfg.Global.RecoveryMaxAttempts, want)
		}
		if want := 30 * time.Minute; cfg.Global.RecoveryAttemptWindow != want {
			t.Errorf("RecoveryAttemptWindow = %s, want the %s default", cfg.Global.RecoveryAttemptWindow, want)
		}
	})

	t.Run("the_shipped_example_config_loads_and_its_recovery_policy_is_actually_applied", func(t *testing.T) {
		cfg, err := Load("../../configs/phoenix.example.yml")
		if err != nil {
			t.Fatalf("the example config shipped with the repo must stay loadable: %v", err)
		}

		// Every recovery_* key the example file sets must survive into the
		// parsed config; a misspelled key there would silently fall back to
		// a default instead.
		raw, err := os.ReadFile("../../configs/phoenix.example.yml")
		if err != nil {
			t.Fatalf("could not read the example config: %v", err)
		}
		var doc struct {
			Global map[string]any `yaml:"global"`
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("could not parse the example config: %v", err)
		}

		known := map[string]bool{
			"check_interval": true, "failure_threshold": true, "ssh_timeout": true,
			"timeout": true, "recovery_cooldown": true, "recovery_max_attempts": true,
			"recovery_attempt_window": true,
		}
		for key := range doc.Global {
			if !known[key] {
				t.Errorf("example config sets global.%q, which no GlobalConfig field maps to — it is silently ignored", key)
			}
		}

		if cfg.Global.RecoveryMaxAttempts <= 0 {
			t.Errorf("RecoveryMaxAttempts = %d, want a positive value", cfg.Global.RecoveryMaxAttempts)
		}
	})

	t.Run("negative_recovery_policy_values_are_rejected", func(t *testing.T) {
		cases := []struct {
			name       string
			file       string
			errContain string
		}{
			{"negative_cooldown_is_rejected", "testdata/invalid_negative_recovery_cooldown.yml", "recovery_cooldown can't be negative"},
			{"negative_max_attempts_is_rejected", "testdata/invalid_negative_recovery_attempts.yml", "recovery_max_attempts can't be negative"},
			{"negative_attempt_window_is_rejected", "testdata/invalid_negative_recovery_window.yml", "recovery_attempt_window can't be negative"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cfg, err := Load(tc.file)
				if err == nil {
					t.Fatalf("expected an error but got nil (cfg: %+v)", cfg)
				}
				if !strings.Contains(err.Error(), tc.errContain) {
					t.Errorf("error message should contain %q, but got: %q", tc.errContain, err.Error())
				}
			})
		}
	})
}

// KnownFields(true) turns a key the config structs don't know about into a
// hard load error. Without it a misspelled key is silently ignored and the
// field keeps its zero value / default, which is how both
// "recocery_cooldown" and "recovery_attempt_windows" once slipped through.
func TestLoad_unknown_config_keys_are_rejected(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		errContain string
	}{
		{"a_misspelled_global_key_is_rejected_instead_of_silently_ignored", "testdata/invalid_unknown_global_field.yml", "recocery_cooldown"},
		{"an_unknown_service_level_key_is_rejected", "testdata/invalid_unknown_service_field.yml", "retry_count"},
		{"an_unknown_key_nested_under_ssh_is_rejected", "testdata/invalid_unknown_nested_field.yml", "passphrase"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.file)
			if err == nil {
				t.Fatalf("expected an error but got nil (cfg: %+v)", cfg)
			}
			if !strings.Contains(err.Error(), "not found in type") {
				t.Errorf("error = %q, want a yaml unknown-field error", err.Error())
			}
			if !strings.Contains(err.Error(), tc.errContain) {
				t.Errorf("error = %q, want it to name the offending key %q", err.Error(), tc.errContain)
			}
			if cfg != nil {
				t.Errorf("cfg should be nil on error, got %+v", cfg)
			}
		})
	}
}
