package route

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

func TestNetworkManagerStartedLifecycle(t *testing.T) {
	r, platform, router := newStartedTestManager(t, false)
	if err := r.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	<-awaitStartedTestUpdate(platform).Done()
	if got := router.resets.Load(); got != 0 {
		t.Fatalf("initial callback reset network before PostStart: %d", got)
	}
	if err := r.Start(adapter.StartStatePostStart); err != nil {
		t.Fatal(err)
	}
	platform.monitor.emit()
	<-awaitStartedTestUpdate(platform).Done()
	if got := router.resets.Load(); got != 1 {
		t.Fatalf("callback after PostStart must reset network once: %d", got)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !platform.monitor.closed.Load() {
		t.Fatal("Close did not close the interface monitor")
	}
}

func TestNetworkManagerStartedConcurrentCallback(t *testing.T) {
	for i := 0; i < 64; i++ {
		r, platform, router := newStartedTestManager(t, false)
		if err := r.Start(adapter.StartStateInitialize); err != nil {
			t.Fatal(err)
		}
		// Receiving the context releases the callback's WIFI read, but does not
		// order its later started read against the concurrent PostStart write.
		updateContext := awaitStartedTestUpdate(platform)
		if err := r.Start(adapter.StartStatePostStart); err != nil {
			t.Fatal(err)
		}
		<-updateContext.Done()
		before := router.resets.Load()
		platform.monitor.emit()
		<-awaitStartedTestUpdate(platform).Done()
		if got := router.resets.Load(); got != before+1 {
			t.Fatalf("iteration %d: post-start callback resets=%d, want %d", i, got, before+1)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNetworkManagerStartedCloseCancelsCallback(t *testing.T) {
	r, platform, router := newStartedTestManager(t, true)
	if err := r.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(adapter.StartStatePostStart); err != nil {
		t.Fatal(err)
	}
	updateContext := awaitStartedTestUpdate(platform)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	<-updateContext.Done()
	// Close cancels, rather than joins, the callback in this upstream version.
	// Drain it explicitly before checking that cancellation suppressed reset.
	r.interfaceUpdateRunAccess.Lock()
	r.interfaceUpdateRunAccess.Unlock()
	if got := router.resets.Load(); got != 0 {
		t.Fatalf("cancelled callback reset network during Close: %d", got)
	}
	r.interfaceUpdateAccess.Lock()
	pending := r.interfaceUpdateCancel != nil
	r.interfaceUpdateAccess.Unlock()
	if pending || !platform.monitor.closed.Load() {
		t.Fatal("Close left the callback cancellation or monitor active")
	}
}

func awaitStartedTestUpdate(platform *startedTestPlatform) context.Context {
	return <-platform.updates
}

func newStartedTestManager(t *testing.T, waitForCancel bool) (*NetworkManager, *startedTestPlatform, *startedTestRouter) {
	t.Helper()
	platform := &startedTestPlatform{
		monitor:       &startedTestMonitor{},
		updates:       make(chan context.Context),
		waitForCancel: waitForCancel,
	}
	router := &startedTestRouter{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctx = pause.WithDefaultManager(ctx)
	ctx = service.ContextWith[adapter.PlatformInterface](ctx, platform)
	ctx = service.ContextWith[adapter.Router](ctx, router)
	ctx = service.ContextWith[adapter.EndpointManager](ctx, startedTestEndpoints{})
	ctx = service.ContextWith[adapter.InboundManager](ctx, startedTestInbounds{})
	ctx = service.ContextWith[adapter.OutboundManager](ctx, startedTestOutbounds{})
	r, err := NewNetworkManager(ctx, logger.NOP(), option.RouteOptions{}, option.DNSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return r, platform, router
}

type startedTestPlatform struct {
	adapter.PlatformInterface
	monitor       *startedTestMonitor
	updates       chan context.Context
	waitForCancel bool
}

func (p *startedTestPlatform) UsePlatformDefaultInterfaceMonitor() bool { return true }
func (p *startedTestPlatform) CreateDefaultInterfaceMonitor(logger.Logger) tun.DefaultInterfaceMonitor {
	return p.monitor
}
func (p *startedTestPlatform) UsePlatformNetworkInterfaces() bool { return false }
func (p *startedTestPlatform) UsePlatformWIFIMonitor() bool       { return true }
func (p *startedTestPlatform) ReadWIFIState(ctx context.Context) adapter.WIFIState {
	select {
	case p.updates <- ctx:
	case <-ctx.Done():
		return adapter.WIFIState{}
	}
	if p.waitForCancel {
		<-ctx.Done()
	}
	return adapter.WIFIState{}
}

type startedTestMonitor struct {
	tun.DefaultInterfaceMonitor
	callback tun.DefaultInterfaceUpdateCallback
	closed   atomic.Bool
}

func (m *startedTestMonitor) RegisterCallback(callback tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	m.callback = callback
	return nil
}
func (m *startedTestMonitor) Start() error {
	m.emit()
	return nil
}
func (m *startedTestMonitor) Close() error {
	m.closed.Store(true)
	return nil
}
func (m *startedTestMonitor) DefaultInterface() *control.Interface { return nil }
func (m *startedTestMonitor) emit() {
	m.callback(&control.Interface{Index: 1, Name: "test-interface", Flags: net.FlagUp}, 0)
}

type startedTestRouter struct {
	adapter.Router
	resets atomic.Int32
}

func (r *startedTestRouter) ResetNetwork() { r.resets.Add(1) }

type startedTestEndpoints struct{ adapter.EndpointManager }

func (startedTestEndpoints) Endpoints() []adapter.Endpoint { return nil }

type startedTestInbounds struct{ adapter.InboundManager }

func (startedTestInbounds) Inbounds() []adapter.Inbound { return nil }

type startedTestOutbounds struct{ adapter.OutboundManager }

func (startedTestOutbounds) Outbounds() []adapter.Outbound { return nil }
