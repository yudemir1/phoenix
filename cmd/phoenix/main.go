package main

import (
	"fmt"
	"os"
	"github.com/yudemir1/phoenix/internal/config"
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
}
