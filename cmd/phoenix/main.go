package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/healer"
	"github.com/yudemir1/phoenix/internal/monitor"
	"github.com/yudemir1/phoenix/internal/runner"
)

func main() {
	configPath := flag.String("config", "configs/phoenix.example.yml", "path to the Phoenix config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Config couldn't loaded:", err)
		os.Exit(1)
	}

	fmt.Println("Phoenix starting...")

	// Ctrl+C (SIGINT) veya `kill` (SIGTERM) geldiğinde ctx otomatik iptal olur.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	det := detector.New(cfg.Global.FailureThreshold)
	h := healer.NewPolicyHealer(healer.NewSSHHealer(), healer.Policy{
		Cooldown: cfg.Global.RecoveryCooldown,
		MaxAttempts: cfg.Global.RecoveryMaxAttempts,
		AttemptWindows: cfg.Global.RecoveryAttemptWindow,
	})

	var wg sync.WaitGroup

	for _, s := range cfg.Services {
		checker, err := monitor.NewChecker(s)
		if err != nil {
			fmt.Printf("checker oluşturulamadı (%s): %v\n", s.Name, err)
			continue
		}

		svcRunner := runner.NewRunner(s, checker, det, h)

		wg.Add(1)
		go func() {
			defer wg.Done()
			svcRunner.Start(ctx)
		}()
	}

	// Tüm goroutine'ler ctx iptal olup bitene kadar burada bekleriz.
	wg.Wait()
	fmt.Println("Phoenix durdu.")
}