package main

import (
	"fmt"
	"context"
	"os"
	"github.com/yudemir1/phoenix/internal/config"
	"github.com/yudemir1/phoenix/internal/monitor"
	"github.com/yudemir1/phoenix/internal/detector"
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

		// main() içinde, checker test kodundan sonra:
	det := detector.New(3) // threshold: 3

	// web-api-1 için 4 kere üst üste başarısız simülasyonu
	for i := 1; i <= 4; i++ {
		fakeResult := monitor.Result{Healthy: false}
		state, changed := det.RecordResult("web-api-1", fakeResult)
		fmt.Printf("deneme %d -> state=%s changed=%v\n", i, state, changed)
	}
}