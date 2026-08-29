package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPChecker_Check(t *testing.T) {
	t.Run("returns_healthy_true_for_2xx_status_code", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := NewHTTPChecker(srv.URL, time.Second)
		res := c.Check(context.Background())

		if !res.Healthy {
			t.Errorf("Healthy = false, want true (err: %v)", res.Err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want %d", res.StatusCode, http.StatusOK)
		}
		if res.Err != nil {
			t.Errorf("Err = %v, want nil", res.Err)
		}
	})

	t.Run("returns_healthy_false_for_status_code_outside_2xx_range", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewHTTPChecker(srv.URL, time.Second)
		res := c.Check(context.Background())

		if res.Healthy {
			t.Error("Healthy = true, want false for a 500 response")
		}
		if res.StatusCode != http.StatusInternalServerError {
			t.Errorf("StatusCode = %d, want %d", res.StatusCode, http.StatusInternalServerError)
		}
		if res.Err == nil {
			t.Error("Err = nil, want a non-nil error describing the unexpected status code")
		}
	})

	t.Run("returns_healthy_false_and_error_when_connection_fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		target := srv.URL
		srv.Close() // nothing is listening on this address anymore

		c := NewHTTPChecker(target, time.Second)
		res := c.Check(context.Background())

		if res.Healthy {
			t.Error("Healthy = true, want false when the connection is refused")
		}
		if res.Err == nil {
			t.Error("Err = nil, want a non-nil connection error")
		}
	})

	t.Run("returns_healthy_false_and_error_when_request_times_out", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := NewHTTPChecker(srv.URL, 20*time.Millisecond)
		res := c.Check(context.Background())

		if res.Healthy {
			t.Error("Healthy = true, want false when the handler exceeds the checker timeout")
		}
		if res.Err == nil {
			t.Error("Err = nil, want a non-nil timeout error")
		}
	})

	t.Run("returns_healthy_false_and_error_for_malformed_target_url", func(t *testing.T) {
		c := NewHTTPChecker("http://\x00invalid", time.Second)
		res := c.Check(context.Background())

		if res.Healthy {
			t.Error("Healthy = true, want false for a malformed target URL")
		}
		if res.StatusCode != 0 {
			t.Errorf("StatusCode = %d, want 0 (request was never sent)", res.StatusCode)
		}
		if res.Err == nil {
			t.Error("Err = nil, want a non-nil request-construction error")
		}
	})
}
