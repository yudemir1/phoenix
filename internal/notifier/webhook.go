package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultWebhookTimeout = 5 * time.Second

type WebhookNotifier struct {
	url    string
	client *http.Client
}

func NewWebhookNotifier(url string, timeout time.Duration) *WebhookNotifier {
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	return &WebhookNotifier{
		url:    url,
		client: &http.Client{Timeout: timeout},
	}
}

func (w *WebhookNotifier) Notify(ctx context.Context, ev Event) error {
	payload := map[string]string{
		"text":     ev.Text(),
		"type":     string(ev.Type),
		"service":  ev.Service,
		"state":    ev.State,
		"strategy": ev.Strategy,
		"error":    ev.Err,
		"time":     ev.Time.UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("Error: could not encode notification: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Error: could not build notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("Error: could not deliver notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Error: notification endpoint returned %d", resp.StatusCode)
	}
	return nil
}
