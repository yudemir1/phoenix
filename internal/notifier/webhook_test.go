package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// capturedRequest holds what the fake endpoint received.
type capturedRequest struct {
	method      string
	contentType string
	body        map[string]string
}

// startCapturingEndpoint serves as the webhook target: it records the request
// it receives and replies with status.
func startCapturingEndpoint(t *testing.T, status int) (*httptest.Server, *capturedRequest) {
	t.Helper()

	got := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.contentType = r.Header.Get("Content-Type")

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("could not read request body: %v", err)
		}
		if err := json.Unmarshal(raw, &got.body); err != nil {
			t.Errorf("request body is not the expected JSON object: %v (body: %s)", err, raw)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	return srv, got
}

func TestWebhookNotifier_Notify(t *testing.T) {
	t.Run("posts_json_to_the_configured_url", func(t *testing.T) {
		srv, got := startCapturingEndpoint(t, http.StatusOK)
		n := NewWebhookNotifier(srv.URL, time.Second)

		err := n.Notify(context.Background(), Event{
			Type:     EventRecoveryFailed,
			Service:  "web-api",
			Strategy: "docker_restart",
			Err:      "ssh: connection refused",
			Time:     time.Date(2026, 9, 13, 2, 30, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got.method != http.MethodPost {
			t.Errorf("method = %s, want POST", got.method)
		}
		if !strings.Contains(got.contentType, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", got.contentType)
		}
	})

	t.Run("the_payload_carries_a_text_field_so_slack_style_webhooks_render_it", func(t *testing.T) {
		srv, got := startCapturingEndpoint(t, http.StatusOK)
		n := NewWebhookNotifier(srv.URL, time.Second)

		ev := Event{Type: EventStateChanged, Service: "web-api", State: "DOWN"}
		if err := n.Notify(context.Background(), ev); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got.body["text"] == "" {
			t.Error(`payload has no "text" field; Slack incoming webhooks render only that field`)
		}
		if want := ev.Text(); got.body["text"] != want {
			t.Errorf("text = %q, want %q", got.body["text"], want)
		}
	})

	t.Run("the_payload_also_carries_the_structured_fields_a_custom_endpoint_needs", func(t *testing.T) {
		srv, got := startCapturingEndpoint(t, http.StatusOK)
		n := NewWebhookNotifier(srv.URL, time.Second)

		err := n.Notify(context.Background(), Event{
			Type:     EventRecoverySkipped,
			Service:  "db-proxy",
			State:    "DOWN",
			Strategy: "custom_command",
			Err:      "cooldown still active",
			Time:     time.Date(2026, 9, 13, 2, 30, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := map[string]string{
			"type":     string(EventRecoverySkipped),
			"service":  "db-proxy",
			"state":    "DOWN",
			"strategy": "custom_command",
			"error":    "cooldown still active",
		}
		for key, wantValue := range want {
			if got.body[key] != wantValue {
				t.Errorf("payload[%q] = %q, want %q", key, got.body[key], wantValue)
			}
		}
		if got.body["time"] != "2026-09-13T02:30:00Z" {
			t.Errorf("payload[time] = %q, want an RFC3339 UTC timestamp", got.body["time"])
		}
	})

	t.Run("a_non_2xx_response_is_reported_as_an_error", func(t *testing.T) {
		srv, _ := startCapturingEndpoint(t, http.StatusInternalServerError)
		n := NewWebhookNotifier(srv.URL, time.Second)

		err := n.Notify(context.Background(), Event{Type: EventStateChanged, Service: "web-api", State: "DOWN"})
		if err == nil {
			t.Fatal("expected an error for a 500 response, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error = %q, want it to mention the status code", err.Error())
		}
	})

	t.Run("an_unreachable_endpoint_is_reported_as_an_error", func(t *testing.T) {
		srv, _ := startCapturingEndpoint(t, http.StatusOK)
		url := srv.URL
		srv.Close() // nothing is listening anymore

		n := NewWebhookNotifier(url, time.Second)
		if err := n.Notify(context.Background(), Event{Type: EventStateChanged, Service: "web-api"}); err == nil {
			t.Error("expected a delivery error, got nil")
		}
	})

	t.Run("a_slow_endpoint_is_cut_off_by_the_configured_timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		n := NewWebhookNotifier(srv.URL, 50*time.Millisecond)

		start := time.Now()
		err := n.Notify(context.Background(), Event{Type: EventStateChanged, Service: "web-api"})
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected a timeout error, got nil")
		}
		if elapsed > 400*time.Millisecond {
			t.Errorf("took %s: the 50ms timeout does not seem to be in effect", elapsed)
		}
	})

	t.Run("an_already_canceled_context_stops_the_delivery", func(t *testing.T) {
		srv, _ := startCapturingEndpoint(t, http.StatusOK)
		n := NewWebhookNotifier(srv.URL, time.Second)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := n.Notify(ctx, Event{Type: EventStateChanged, Service: "web-api"}); err == nil {
			t.Error("expected the canceled context to abort delivery, got nil")
		}
	})

	t.Run("a_zero_timeout_falls_back_to_the_built_in_default", func(t *testing.T) {
		n := NewWebhookNotifier("https://example.invalid/hook", 0)
		if n.client.Timeout != defaultWebhookTimeout {
			t.Errorf("client timeout = %s, want the %s default", n.client.Timeout, defaultWebhookTimeout)
		}
	})
}
