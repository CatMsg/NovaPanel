package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
)

type fakeDNSTransport struct {
	access  sync.Mutex
	resets  int
	err     error
	block   chan struct{}
	entered chan struct{}
}

func (t *fakeDNSTransport) Start(adapter.StartStage) error { return nil }
func (t *fakeDNSTransport) Close() error                   { return nil }
func (t *fakeDNSTransport) Type() string                   { return "udp" }
func (t *fakeDNSTransport) Tag() string                    { return "test" }
func (t *fakeDNSTransport) Dependencies() []string         { return nil }
func (t *fakeDNSTransport) Reset() {
	t.access.Lock()
	t.resets++
	t.access.Unlock()
}
func (t *fakeDNSTransport) Exchange(context.Context, *dns.Msg) (*dns.Msg, error) {
	if t.entered != nil {
		t.entered <- struct{}{}
	}
	if t.block != nil {
		<-t.block
	}
	return &dns.Msg{}, t.err
}
func (t *fakeDNSTransport) ExchangeAsync(ctx context.Context, message *dns.Msg, callback func(*dns.Msg, error)) {
	response, err := t.Exchange(ctx, message)
	callback(response, err)
}
func (t *fakeDNSTransport) resetCount() int {
	t.access.Lock()
	defer t.access.Unlock()
	return t.resets
}

func TestResilientUDPDNSTransportResetsIdleConnection(t *testing.T) {
	base := &fakeDNSTransport{}
	transport := &resilientUDPDNSTransport{
		DNSTransport: base,
		lastExchange: time.Now().Add(-udpDNSIdleResetInterval),
	}

	if _, err := transport.Exchange(context.Background(), &dns.Msg{}); err != nil {
		t.Fatal(err)
	}
	if got := base.resetCount(); got != 1 {
		t.Fatalf("expected one idle reset, got %d", got)
	}
}

func TestResilientUDPDNSTransportResetsAfterError(t *testing.T) {
	base := &fakeDNSTransport{err: errors.New("timeout")}
	transport := &resilientUDPDNSTransport{DNSTransport: base}

	if _, err := transport.Exchange(context.Background(), &dns.Msg{}); err == nil {
		t.Fatal("expected exchange error")
	}
	if got := base.resetCount(); got != 1 {
		t.Fatalf("expected one error reset, got %d", got)
	}
}

func TestResilientUDPDNSTransportDefersResetUntilConcurrentExchangesFinish(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	base := &fakeDNSTransport{err: errors.New("timeout"), block: release, entered: entered}
	transport := &resilientUDPDNSTransport{DNSTransport: base}
	done := make(chan struct{}, 2)

	for range 2 {
		go func() {
			_, _ = transport.Exchange(context.Background(), &dns.Msg{})
			done <- struct{}{}
		}()
	}
	<-entered
	<-entered
	if got := base.resetCount(); got != 0 {
		t.Fatalf("reset ran while exchanges were active: %d", got)
	}
	close(release)
	<-done
	<-done
	if got := base.resetCount(); got != 1 {
		t.Fatalf("expected one deferred reset, got %d", got)
	}
}

func TestResilientUDPDNSTransportResetsAfterAsyncError(t *testing.T) {
	base := &fakeDNSTransport{err: errors.New("timeout")}
	transport := &resilientUDPDNSTransport{DNSTransport: base}
	done := make(chan error, 1)

	transport.ExchangeAsync(context.Background(), &dns.Msg{}, func(_ *dns.Msg, err error) {
		done <- err
	})
	if err := <-done; err == nil {
		t.Fatal("expected exchange error")
	}
	if got := base.resetCount(); got != 1 {
		t.Fatalf("expected one async error reset, got %d", got)
	}
}
