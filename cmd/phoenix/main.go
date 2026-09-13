package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/healer"
	"github.com/yudemir1/phoenix/internal/monitor"
	"github.com/yudemir1/phoenix/internal/notifier"
	"github.com/yudemir1/phoenix/internal/runner"
)

func main() {
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	configPath := flag.String("config", "configs/phoenix.example.yml", "path to the Phoenix config file")
	flag.Parse()

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*logLevel)); err != nil {
		fmt.Fprintf(os.Stderr, "invalid -log-level %q: %v\n", *logLevel, err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Config couldn't loaded:", err)
		os.Exit(1)
	}

	logger.Info("Phoenix starting...", "services", len(cfg.Services), "config", *configPath)

	// Ctrl+C (SIGINT) veya `kill` (SIGTERM) geldiğinde ctx otomatik iptal olur.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	det := detector.New(cfg.Global.FailureThreshold)
	var sshHealer *healer.SSHHealer
	if cfg.Global.InsecureSkipHostKeyVerify {
		logger.Warn("SSH host key verification is disabled; recovery commands can be sent to an impostor host")
		sshHealer = healer.NewInsecureSSHHealer(cfg.Global.SSHTimeout)
	} else {
		sshHealer, err = healer.NewSSHHealer(cfg.Global.KnownHostsPath, cfg.Global.SSHTimeout)
		if err != nil {
			logger.Error("could not set up SSH host key verification", "known_hosts", cfg.Global.KnownHostsPath, "err", err)
			os.Exit(1)
		}
	}

	h := healer.NewPolicyHealer(sshHealer, healer.Policy{
		Cooldown:       cfg.Global.RecoveryCooldown,
		MaxAttempts:    cfg.Global.RecoveryMaxAttempts,
		AttemptWindows: cfg.Global.RecoveryAttemptWindow,
	})

	var notif notifier.Notifier
	if cfg.Global.WebhookURL != "" {
		notif = notifier.NewWebhookNotifier(cfg.Global.WebhookURL, cfg.Global.WebhookTimeout)
		logger.Info("notifications enabled", "timeout", cfg.Global.WebhookTimeout)
	}
	var wg sync.WaitGroup

	for _, s := range cfg.Services {
		checker, err := monitor.NewChecker(s)
		if err != nil {
			logger.Info("could not create checker", "service", s.Name, "err", err)
			continue
		}

		svcRunner := runner.NewRunner(s, checker, det, h, logger, notif)

		wg.Add(1)
		go func() {
			defer wg.Done()
			svcRunner.Start(ctx)
		}()
	}

	// Tüm goroutine'ler ctx iptal olup bitene kadar burada bekleriz.
	wg.Wait()
	logger.Info("phoenix stopped")
}
