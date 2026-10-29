package sub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testInternalSourceURL(rawURL string) string {
	return strings.Replace(rawURL, "http://", "internal+http://", 1)
}

func TestLinkServiceContextCancelsExternalSubscriptionFetch(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)

	links := json.RawMessage(`[{"type":"sub","uri":"` + testInternalSourceURL(server.URL) + `"}]`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := (&LinkService{}).GetLinksContext(ctx, &links, "all", "")
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("external subscription request did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("canceled link fetch error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceling the subscription context did not stop the external fetch")
	}
}
