package main

import (
	"fmt"
	"context"
	"os"
	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/monitor"
)

func main() {
	cfg, err := config.Load("configs/phoenix.example.yml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Config couldn't loaded:", err)
		os.Exit(1)
	}
	fmt.Println("Phoenix starting...")
	fmt.Printf("Global check interval: %s, threshold: %d\n", cfg.Global.CheckInterval, cfg.Global.FailureThreshold)
	for _, s := range cfg.Services {
		fmt.Printf("- %-16s type:%-5s target:%-30s recovery:%s\n", s.Name, s.CheckType, s.Target, s.Recover.Strategy)
	}
	for _, s := range cfg.Services {
	checker, err := monitor.NewChecker(s)
	if err != nil {
		fmt.Println("checker oluşturulamadı:", err)
		continue
	}

	result := checker.Check(context.Background())
	fmt.Printf("%s -> healthy=%v latency=%s status=%d err=%v\n", s.Name, result.Healthy, result.Latency, result.StatusCode, result.Err)
	}
}