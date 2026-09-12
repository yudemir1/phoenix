package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/detector"
	"github.com/yudemir1/phoenix/internal/monitor"
	"github.com/yudemir1/phoenix/internal/runner"
	"github.com/yudemir1/phoenix/internal/healer"
)

func main() {
	cfg, err := config.Load("configs/phoenix.example.yml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Config couldn't loaded:", err)
		os.Exit(1)
	}

	fmt.Println("Phoenix starting...")

	// Ctrl+C (SIGINT) veya `kill` (SIGTERM) geldiğinde ctx otomatik iptal olur.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	det := detector.New(cfg.Global.FailureThreshold)
	h := healer.NewSSHHealer()

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