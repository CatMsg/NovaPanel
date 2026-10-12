package route

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

func TestNetworkManagerStartedLifecycle(t *testing.T) {
	r, platform, router, scope := newStartedTestManager(t, false)
	if err := r.Start(adapter.StartStateInitialize, scope); err != nil {
		t.Fatal(err)
	}
	platform.monitor.emit()
	select {
	case <-platform.updates:
		t.Fatal("interface update dispatched before PostStart")
	default:
	}
	if got := router.resets.Load(); got != 0 {
		t.Fatalf("reset before PostStart: %d", got)
	}
	if err := r.Start(adapter.StartStatePostStart, scope); err != nil {
		t.Fatal(err)
	}
	finishStartedTestUpdate(r, <-platform.updates)
	if got := router.resets.Load(); got != 1 {
		t.Fatalf("pending initial reset=%d, want 1", got)
	}
	platform.monitor.emit()
	finishStartedTestUpdate(r, <-platform.updates)
	if got := router.resets.Load(); got != 2 {
		t.Fatalf("post-start reset=%d, want 2", got)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if platform.monitor.closed.Load() != 1 || platform.monitor.unregistered.Load() != 1 {
		t.Fatal("Scope.Close did not unregister and close the interface monitor exactly once")
	}
}

func TestNetworkManagerStartedConcurrentCallback(t *testing.T) {
	for i := 0; i < 64; i++ {
		r, platform, router, scope := newStartedTestManager(t, false)
		if err := r.Start(adapter.StartStateInitialize, scope); err != nil {
			t.Fatal(err)
		}
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			if err := r.Start(adapter.StartStatePostStart, scope); err != nil {
				t.Error(err)
			}
		}()
		go func() { defer workers.Done(); platform.monitor.emit() }()
		workers.Wait()
		for {
			update := <-platform.updates
			if update.ctx.Err() != nil {
				close(update.release)
				continue
			}
			finishStartedTestUpdate(r, update)
			break
		}
		if got := router.resets.Load(); got != 1 {
			t.Fatalf("iteration %d: reset=%d, want 1", i, got)
		}
		if err := scope.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNetworkManagerStartedCloseCancelsCallback(t *testing.T) {
	r, platform, router, scope := newStartedTestManager(t, true)
	if err := r.Start(adapter.StartStateInitialize, scope); err != nil {
		t.Fatal(err)
	}
	platform.monitor.emit()
	if err := r.Start(adapter.StartStatePostStart, scope); err != nil {
		t.Fatal(err)
	}
	update := <-platform.updates
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if update.ctx.Err() != context.Canceled {
		t.Fatal("Scope.Close did not cancel pending callback")
	}
	// Scope.Close joins resetRunAccess, so no explicit drain is needed.
	if got := router.resets.Load(); got != 0 {
		t.Fatalf("cancelled callback reset network: %d", got)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if platform.monitor.closed.Load() != 1 || platform.monitor.unregistered.Load() != 1 {
		t.Fatal("repeated Scope.Close repeated monitor cleanup")
	}
}

type startedTestUpdate struct {
	ctx     context.Context
	release chan struct{}
}

func finishStartedTestUpdate(r *NetworkManager, update startedTestUpdate) {
	close(update.release)
	r.resetRunAccess.Lock()
	r.resetRunAccess.Unlock()
}

func newStartedTestManager(t *testing.T, waitForCancel bool) (*NetworkManager, *startedTestPlatform, *startedTestRouter, *adapter.Scope) {
	t.Helper()
	platform := &startedTestPlatform{monitor: &startedTestMonitor{}, updates: make(chan startedTestUpdate), waitForCancel: waitForCancel}
	router := &startedTestRouter{}
	ctx := pause.WithDefaultManager(context.Background())
	ctx = service.ContextWith[adapter.PlatformInterface](ctx, platform)
	ctx = service.ContextWith[adapter.Router](ctx, router)
	ctx = service.ContextWith[adapter.EndpointManager](ctx, startedTestEndpoints{})
	ctx = service.ContextWith[adapter.InboundManager](ctx, startedTestInbounds{})
	ctx = service.ContextWith[adapter.OutboundManager](ctx, startedTestOutbounds{})
	logger := log.NewNOPFactory().Logger()
	scope := adapter.NewScope(ctx, logger)
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Error(err)
		}
	})
	r, err := NewNetworkManager(ctx, logger, option.RouteOptions{}, option.DNSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return r, platform, router, scope
}

type startedTestPlatform struct {
	adapter.PlatformInterface
	monitor       *startedTestMonitor
	updates       chan startedTestUpdate
	waitForCancel bool
}

func (p *startedTestPlatform) UsePlatformDefaultInterfaceMonitor() bool { return true }
func (p *startedTestPlatform) CreateDefaultInterfaceMonitor(logger.Logger) tun.DefaultInterfaceMonitor {
	return p.monitor
}
func (p *startedTestPlatform) UsePlatformNetworkInterfaces() bool { return false }
func (p *startedTestPlatform) UsePlatformWIFIMonitor() bool       { return true }
func (p *startedTestPlatform) ReadWIFIState(ctx context.Context) adapter.WIFIState {
	update := startedTestUpdate{ctx: ctx, release: make(chan struct{})}
	select {
	case p.updates <- update:
	case <-ctx.Done():
		return adapter.WIFIState{}
	}
	if p.waitForCancel {
		<-ctx.Done()
	} else {
		select {
		case <-update.release:
		case <-ctx.Done():
		}
	}
	return adapter.WIFIState{}
}

type startedTestMonitor struct {
	tun.DefaultInterfaceMonitor
	callback     tun.DefaultInterfaceUpdateCallback
	closed       atomic.Int32
	unregistered atomic.Int32
}

func (m *startedTestMonitor) RegisterCallback(callback tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	m.callback = callback
	return nil
}
func (m *startedTestMonitor) UnregisterCallback(*list.Element[tun.DefaultInterfaceUpdateCallback]) {
	m.unregistered.Add(1)
}
func (m *startedTestMonitor) Start() error { return nil }
func (m *startedTestMonitor) Close() error { m.closed.Add(1); return nil }
func (m *startedTestMonitor) DefaultInterface() *control.Interface {
	return &control.Interface{Index: 1, Name: "test-interface", Flags: net.FlagUp}
}
func (m *startedTestMonitor) emit() { m.callback(m.DefaultInterface(), 0) }

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
