package monitor

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type HTTPChecker struct {
	Target            string
	ExpectedStatusMin int
	ExpectedStatusMax int
	Timeout           time.Duration
}

func NewHTTPChecker(target string, timeout time.Duration) *HTTPChecker {
	return &HTTPChecker{
		Target:            target,
		ExpectedStatusMin: 200,
		ExpectedStatusMax: 299,
		Timeout:           timeout,
	}
}

func (h *HTTPChecker) Check(ctx context.Context) Result {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.Target, nil)
	if err != nil {
		return Result{
			Healthy: false,
			Latency: time.Since(start),
			Err:     fmt.Errorf("Error: HTTP Request couldn't made."),
		}
	}

	client := &http.Client{Timeout: h.Timeout}
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return Result{
			Healthy: false,
			Latency: latency,
			Err:     fmt.Errorf("Error: HTTP Request failed."),
		}
	}
	defer resp.Body.Close()

	healthy := resp.StatusCode >= h.ExpectedStatusMin && resp.StatusCode <= h.ExpectedStatusMax
	var checkErr error
	if !healthy {
		checkErr = fmt.Errorf("Error: Unexpected status code %d (Expected: %d-%d)", resp.StatusCode, h.ExpectedStatusMin, h.ExpectedStatusMax)
	}
	return Result{
		Healthy:    healthy,
		Latency:    latency,
		StatusCode: resp.StatusCode,
		Err:        checkErr,
	}
}
