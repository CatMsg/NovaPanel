package core

import (
	"context"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	sbDNS "github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

const udpDNSIdleResetInterval = 30 * time.Second

// resilientUDPDNSTransport prevents a silently expired UDP flow from being
// reused indefinitely. Resets are deferred until no exchange is in flight.
type resilientUDPDNSTransport struct {
	adapter.DNSTransport

	access       sync.Mutex
	active       int
	lastExchange time.Time
	resetPending bool
}

func registerResilientUDPTransport(registry *sbDNS.TransportRegistry) {
	sbDNS.RegisterTransport[option.RemoteDNSServerOptions](registry, C.DNSTypeUDP, newResilientUDPTransport)
}

func newResilientUDPTransport(ctx context.Context, logger log.ContextLogger, tag string, options option.RemoteDNSServerOptions) (adapter.DNSTransport, error) {
	base, err := transport.NewUDP(ctx, logger, tag, options)
	if err != nil {
		return nil, err
	}
	return &resilientUDPDNSTransport{DNSTransport: base}, nil
}

func (t *resilientUDPDNSTransport) Exchange(ctx context.Context, message *dns.Msg) (*dns.Msg, error) {
	t.beginExchange(time.Now())
	response, err := t.DNSTransport.Exchange(ctx, message)
	t.endExchange(time.Now(), err)
	return response, err
}

func (t *resilientUDPDNSTransport) beginExchange(now time.Time) {
	t.access.Lock()
	defer t.access.Unlock()

	if t.active == 0 && (t.resetPending || (!t.lastExchange.IsZero() && now.Sub(t.lastExchange) >= udpDNSIdleResetInterval)) {
		t.DNSTransport.Reset()
		t.resetPending = false
	}
	t.active++
}

func (t *resilientUDPDNSTransport) endExchange(now time.Time, exchangeErr error) {
	t.access.Lock()
	defer t.access.Unlock()

	t.active--
	t.lastExchange = now
	if exchangeErr != nil {
		t.resetPending = true
	}
	if t.active == 0 && t.resetPending {
		t.DNSTransport.Reset()
		t.resetPending = false
	}
}
