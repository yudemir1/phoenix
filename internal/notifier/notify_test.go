package notifier

import (
	"strings"
	"testing"
)

func TestEvent_Text(t *testing.T) {
	cases := []struct {
		name     string
		event    Event
		contains []string
	}{
		{
			name:     "a_state_change_names_the_service_and_its_new_state",
			event:    Event{Type: EventStateChanged, Service: "web-api", State: "DOWN"},
			contains: []string{"web-api", "DOWN"},
		},
		{
			name:     "a_successful_recovery_names_the_service_and_the_strategy",
			event:    Event{Type: EventRecoverySucceeded, Service: "web-api", Strategy: "docker_restart"},
			contains: []string{"web-api", "docker_restart", "succeeded"},
		},
		{
			name:     "a_failed_recovery_includes_the_error_text",
			event:    Event{Type: EventRecoveryFailed, Service: "web-api", Strategy: "docker_restart", Err: "ssh: connection refused"},
			contains: []string{"web-api", "docker_restart", "ssh: connection refused"},
		},
		{
			name:     "a_policy_skip_explains_that_it_was_skipped_rather_than_failed",
			event:    Event{Type: EventRecoverySkipped, Service: "web-api", Err: "cooldown still active"},
			contains: []string{"web-api", "skipped", "cooldown still active"},
		},
		{
			name:     "an_unknown_event_type_still_produces_something_readable",
			event:    Event{Type: EventType("something_new"), Service: "web-api"},
			contains: []string{"web-api", "something_new"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.event.Text()
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("Text() = %q, want it to contain %q", got, want)
				}
			}
			// A formatting mistake such as a missing verb shows up as Go's
			// %!(EXTRA ...) or %!s(MISSING) marker; neither belongs in a
			// message a human is going to read.
			if strings.Contains(got, "%!") {
				t.Errorf("Text() = %q, which contains a formatting error marker", got)
			}
		})
	}
}
