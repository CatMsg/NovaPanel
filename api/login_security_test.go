package api

import (
	"testing"
	"time"
)

func TestLoginFailureTrackerUsesRollingWindow(t *testing.T) {
	const remoteIP = "192.0.2.10"
	loginFailures.Lock()
	loginFailures.entries = map[string]loginFailureState{
		remoteIP: {attempts: []time.Time{time.Now().Add(-loginFailureWindow - time.Second)}},
	}
	loginFailures.Unlock()
	t.Cleanup(func() { clearLoginFailures(remoteIP) })

	for range loginFailureLimit - 1 {
		recordLoginFailure(remoteIP, nil)
	}
	loginFailures.Lock()
	state := loginFailures.entries[remoteIP]
	loginFailures.Unlock()
	if len(state.attempts) != loginFailureLimit-1 || state.notified {
		t.Fatalf("unexpected state before threshold: %#v", state)
	}

	recordLoginFailure(remoteIP, nil)
	loginFailures.Lock()
	state = loginFailures.entries[remoteIP]
	loginFailures.Unlock()
	if len(state.attempts) != loginFailureLimit || !state.notified {
		t.Fatalf("unexpected state at threshold: %#v", state)
	}
}
