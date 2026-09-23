package core

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestConnTrackerSessionTrafficAndClose(t *testing.T) {
	tracker := NewConnTracker()
	client, peer := net.Pipe()
	defer peer.Close()

	info := &ConnectionInfo{
		ID: "session-1", Conn: client, Inbound: "test-in", Outbound: "direct",
		User: "alice", Type: "tcp", Destination: "example.com:443", StartedAt: time.Now(),
	}
	tracker.trackConnection(info.ID, info)
	wrapped := tracker.createWrappedConn(client, info.ID)

	go func() { _, _ = peer.Write([]byte("down")) }()
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(wrapped, buffer); err != nil {
		t.Fatal(err)
	}
	readDone := make(chan struct{})
	go func() {
		_, _ = io.ReadFull(peer, make([]byte, 2))
		close(readDone)
	}()
	if _, err := wrapped.Write([]byte("up")); err != nil {
		t.Fatal(err)
	}
	<-readDone

	sessions := tracker.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].Upload != 4 || sessions[0].Download != 2 {
		t.Fatalf("unexpected traffic counters: upload=%d download=%d", sessions[0].Upload, sessions[0].Download)
	}

	closed := make(chan bool, 1)
	go func() { closed <- tracker.CloseSession(info.ID) }()
	select {
	case ok := <-closed:
		if !ok {
			t.Fatal("session was not closed")
		}
	case <-time.After(time.Second):
		t.Fatal("closing a wrapped session deadlocked")
	}
	if len(tracker.Sessions()) != 0 {
		t.Fatal("closed session remained tracked")
	}
}

func TestConnTrackerResetDoesNotDeadlock(t *testing.T) {
	tracker := NewConnTracker()
	client, peer := net.Pipe()
	defer peer.Close()
	info := &ConnectionInfo{ID: "session-1", Conn: client, Inbound: "test-in", StartedAt: time.Now()}
	tracker.trackConnection(info.ID, info)
	wrapped := tracker.createWrappedConn(client, info.ID)
	info.Conn = wrapped

	done := make(chan struct{})
	go func() {
		tracker.Reset()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tracker reset deadlocked")
	}
}

func TestNormalizeTrackedSourceForMieruBridge(t *testing.T) {
	SetMieruBridgeInboundTag("mieru-main")
	t.Cleanup(func() { SetMieruBridgeInboundTag("") })

	tests := []struct {
		name    string
		inbound string
		source  string
		want    string
	}{
		{name: "mieru ipv4 loopback", inbound: "mieru-main", source: "127.0.0.1:51000", want: ""},
		{name: "mieru ipv6 loopback", inbound: "mieru-main", source: "[::1]:51000", want: ""},
		{name: "mieru mapped loopback", inbound: "mieru-main", source: "[::ffff:127.0.0.1]:51000", want: ""},
		{name: "mieru public source", inbound: "mieru-main", source: "203.0.113.8:51000", want: "203.0.113.8:51000"},
		{name: "ordinary socks loopback", inbound: "socks-in", source: "127.0.0.1:51000", want: "127.0.0.1:51000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeTrackedSource(tt.inbound, tt.source); got != tt.want {
				t.Fatalf("normalizeTrackedSource(%q, %q) = %q, want %q", tt.inbound, tt.source, got, tt.want)
			}
		})
	}
}
