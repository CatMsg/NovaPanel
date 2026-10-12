package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	panelLog "github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

var errManagedStart = errors.New("synthetic start failure")

type lifecycleTestComponent struct {
	adapter.Outbound
	tag       string
	deps      []string
	address   string
	ctx       context.Context
	failStage *adapter.StartStage
	stages    []adapter.StartStage
	closes    atomic.Int32
	trace     *[]string
	listener  net.Listener
	closeErr  error
}

func (c *lifecycleTestComponent) Type() string           { return "test" }
func (c *lifecycleTestComponent) Tag() string            { return c.tag }
func (c *lifecycleTestComponent) Dependencies() []string { return c.deps }
func (c *lifecycleTestComponent) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	c.stages = append(c.stages, stage)
	if c.trace != nil {
		*c.trace = append(*c.trace, fmt.Sprintf("%s:%s", stage, c.tag))
	}
	if stage == adapter.StartStateInitialize {
		scope.Add(func() error {
			c.closes.Add(1)
			if c.trace != nil {
				*c.trace = append(*c.trace, "close:"+c.tag)
			}
			var err error
			if c.listener != nil {
				err = c.listener.Close()
			}
			return errors.Join(err, c.closeErr)
		})
	}
	if stage == adapter.StartStateStart && c.address != "" {
		var err error
		c.listener, err = net.Listen("tcp", c.address)
		if err != nil {
			return err
		}
	}
	if c.failStage != nil && *c.failStage == stage {
		return errManagedStart
	}
	return nil
}

func lifecycleTestSet(t *testing.T) (*componentSet[*lifecycleTestComponent], *adapter.Scope) {
	t.Helper()
	logger := log.NewNOPFactory().Logger()
	scope := adapter.NewScope(context.Background(), logger)
	set := newComponentSet[*lifecycleTestComponent]("test", logger)
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, stage := range adapter.ListStartStages {
		if err := set.start(stage, scope, false); err != nil {
			t.Fatal(err)
		}
	}
	return set, scope
}

func addLifecycleTestComponent(set *componentSet[*lifecycleTestComponent], component *lifecycleTestComponent) error {
	return set.create(context.Background(), func(ctx context.Context) (*lifecycleTestComponent, error) { component.ctx = ctx; return component, nil }, nil)
}

func lifecycleTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func requireLifecyclePortReleased(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("port %s was not released: %v", address, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestComponentScopeRepeatedReplacementRemoval(t *testing.T) {
	set, scope := lifecycleTestSet(t)
	address := lifecycleTestAddress(t)
	var previous *lifecycleTestComponent
	for range 32 {
		current := &lifecycleTestComponent{tag: "listener", address: address}
		if err := addLifecycleTestComponent(set, current); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(current.stages, adapter.ListStartStages) {
			t.Fatalf("dynamic stages: %v", current.stages)
		}
		if previous != nil && (previous.closes.Load() != 1 || previous.ctx.Err() != context.Canceled) {
			t.Fatal("replacement did not cancel and close previous instance exactly once")
		}
		if len(set.entries) != 1 {
			t.Fatal("removed scopes were retained")
		}
		if err := set.remove("listener", nil); err != nil {
			t.Fatal(err)
		}
		if current.closes.Load() != 1 || current.ctx.Err() != context.Canceled {
			t.Fatal("remove did not close instance")
		}
		requireLifecyclePortReleased(t, address)
		if err := addLifecycleTestComponent(set, &lifecycleTestComponent{tag: "listener", address: address}); err != nil {
			t.Fatal(err)
		}
		previous, _ = set.get("listener")
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if previous.closes.Load() != 1 {
		t.Fatal("scope cleanup was repeated")
	}
	if len(set.values()) != 0 {
		t.Fatal("closed manager retained entries")
	}
	if err := addLifecycleTestComponent(set, &lifecycleTestComponent{tag: "late"}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("create after close: %v", err)
	}
	requireLifecyclePortReleased(t, address)
}

func TestComponentScopeFailedStartAndReplacement(t *testing.T) {
	for _, stage := range adapter.ListStartStages {
		t.Run(stage.String(), func(t *testing.T) {
			set, scope := lifecycleTestSet(t)
			address := lifecycleTestAddress(t)
			old := &lifecycleTestComponent{tag: "listener", address: address}
			if err := addLifecycleTestComponent(set, old); err != nil {
				t.Fatal(err)
			}
			failed := &lifecycleTestComponent{tag: "listener", address: address, failStage: &stage}
			if err := addLifecycleTestComponent(set, failed); !errors.Is(err, errManagedStart) {
				t.Fatalf("failed start: %v", err)
			}
			if old.closes.Load() != 1 || failed.closes.Load() != 1 {
				t.Fatal("replacement/failing instance not closed exactly once")
			}
			if failed.ctx.Err() != context.Canceled {
				t.Fatal("failed start context not canceled")
			}
			if _, found := set.get("listener"); found {
				t.Fatal("failed instance published")
			}
			requireLifecyclePortReleased(t, address)
			recovered := &lifecycleTestComponent{tag: "listener", address: address}
			if err := addLifecycleTestComponent(set, recovered); err != nil {
				t.Fatal(err)
			}
			if err := scope.Close(); err != nil {
				t.Fatal(err)
			}
			if recovered.closes.Load() != 1 || failed.closes.Load() != 1 {
				t.Fatal("shutdown leaked or repeated cleanup")
			}
			requireLifecyclePortReleased(t, address)
		})
	}
}

func TestComponentScopeConstructorAndValidationFailure(t *testing.T) {
	set, _ := lifecycleTestSet(t)
	old := &lifecycleTestComponent{tag: "old"}
	if err := addLifecycleTestComponent(set, old); err != nil {
		t.Fatal(err)
	}
	var failedContext context.Context
	err := set.create(context.Background(), func(ctx context.Context) (*lifecycleTestComponent, error) {
		failedContext = ctx
		return nil, errManagedStart
	}, nil)
	if !errors.Is(err, errManagedStart) || failedContext.Err() != context.Canceled {
		t.Fatal("constructor failure did not cancel its owned context")
	}
	candidate := &lifecycleTestComponent{tag: "old"}
	err = set.create(context.Background(), func(ctx context.Context) (*lifecycleTestComponent, error) { candidate.ctx = ctx; return candidate, nil }, func(*lifecycleTestComponent) error { return errManagedStart })
	if !errors.Is(err, errManagedStart) || candidate.ctx.Err() != context.Canceled {
		t.Fatal("validation failure context not canceled")
	}
	if found, _ := set.get("old"); found != old || old.closes.Load() != 0 {
		t.Fatal("validation failure destroyed the live instance")
	}
	if len(candidate.stages) != 0 {
		t.Fatal("invalid instance was started")
	}
}

func TestComponentScopePreservesCallerMetadata(t *testing.T) {
	set, scope := lifecycleTestSet(t)
	metadata := &adapter.InboundContext{Outbound: "dynamic"}
	ctx := adapter.WithContext(context.Background(), metadata)
	var captured context.Context
	if err := set.create(ctx, func(ctx context.Context) (*lifecycleTestComponent, error) {
		captured = ctx
		if adapter.ContextFrom(ctx) != metadata {
			t.Fatal("dynamic constructor lost caller metadata")
		}
		return &lifecycleTestComponent{tag: "dynamic"}, nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if captured.Err() != context.Canceled {
		t.Fatal("dynamic constructor context outlived box scope")
	}
}

func TestComponentScopeCloseAggregatesErrors(t *testing.T) {
	set, scope := lifecycleTestSet(t)
	firstErr, secondErr := errors.New("first close"), errors.New("second close")
	first := &lifecycleTestComponent{tag: "first", closeErr: firstErr}
	second := &lifecycleTestComponent{tag: "second", closeErr: secondErr}
	if err := addLifecycleTestComponent(set, first); err != nil {
		t.Fatal(err)
	}
	if err := addLifecycleTestComponent(set, second); err != nil {
		t.Fatal(err)
	}
	err := scope.Close()
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("cleanup errors lost: %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if first.closes.Load() != 1 || second.closes.Load() != 1 {
		t.Fatal("close not idempotent")
	}
}

func newLifecycleTestManagers() (*outboundManager, *endpointManager, *adapter.Scope) {
	logger := log.NewNOPFactory().Logger()
	e := &endpointManager{componentSet: newComponentSet[adapter.Endpoint]("endpoint", logger)}
	o := &outboundManager{componentSet: newComponentSet[adapter.Outbound]("outbound", logger), endpoint: e}
	e.outbound = o
	o.Initialize(func(ctx context.Context) (adapter.Outbound, error) { return &lifecycleTestComponent{ctx: ctx}, nil })
	return o, e, adapter.NewScope(context.Background(), logger)
}

func TestOutboundScopeRegistrationOwnershipAndDependencies(t *testing.T) {
	outbounds, endpoints, scope := newLifecycleTestManagers()
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Error(err)
		}
	})
	var trace []string
	regular := &lifecycleTestComponent{tag: "regular", trace: &trace}
	endpoint := &lifecycleTestComponent{tag: "endpoint", deps: []string{"regular"}, trace: &trace}
	if _, structurallyEndpoint := any(regular).(adapter.Endpoint); !structurallyEndpoint {
		t.Fatal("fixture must reproduce alpha12 structural interface overlap")
	}
	if err := outbounds.create(scope.Context(), func(context.Context) (adapter.Outbound, error) { return regular, nil }, nil); err != nil {
		t.Fatal(err)
	}
	if err := endpoints.create(scope.Context(), func(context.Context) (adapter.Endpoint, error) { return endpoint, nil }, nil); err != nil {
		t.Fatal(err)
	}
	for _, stage := range adapter.ListStartStages {
		if err := outbounds.Start(stage, scope); err != nil {
			t.Fatal(err)
		}
		if err := endpoints.Start(stage, scope); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(regular.stages, adapter.ListStartStages) || !reflect.DeepEqual(endpoint.stages, adapter.ListStartStages) {
		t.Fatalf("lifecycle stages regular=%v endpoint=%v", regular.stages, endpoint.stages)
	}
	if slices.Index(trace, "start:regular") >= slices.Index(trace, "start:endpoint") {
		t.Fatalf("dependency order: %v", trace)
	}
	if err := outbounds.Remove("regular"); err == nil {
		t.Fatal("removed dependency while endpoint still uses it")
	}
	if current, _ := outbounds.Outbound("regular"); current != regular || regular.closes.Load() != 0 {
		t.Fatal("failed removal changed live dependency")
	}
	if err := endpoints.Remove("endpoint"); err != nil {
		t.Fatal(err)
	}
	if err := outbounds.Remove("regular"); err != nil {
		t.Fatal(err)
	}
	if outbounds.Default() == nil {
		t.Fatal("removing last outbound left nil default")
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if regular.closes.Load() != 1 || endpoint.closes.Load() != 1 {
		t.Fatal("endpoint/outbound cleanup repeated")
	}
}

type lifecycleOutboundRegistry struct {
	adapter.OutboundRegistry
	create func(context.Context, string) (adapter.Outbound, error)
}

func (r lifecycleOutboundRegistry) CreateOutbound(ctx context.Context, _ adapter.Router, _ log.ContextLogger, tag, _ string, _ any) (adapter.Outbound, error) {
	return r.create(ctx, tag)
}

func TestOutboundScopeFailedDefaultReplacementAndInvalidRemoval(t *testing.T) {
	outbounds, endpoints, scope := newLifecycleTestManagers()
	t.Cleanup(func() { _ = scope.Close() })
	old := &lifecycleTestComponent{tag: "default"}
	if err := outbounds.create(scope.Context(), func(context.Context) (adapter.Outbound, error) { return old, nil }, nil); err != nil {
		t.Fatal(err)
	}
	outbounds.defaultTag = "default"
	for _, stage := range adapter.ListStartStages {
		if err := outbounds.Start(stage, scope); err != nil {
			t.Fatal(err)
		}
		if err := endpoints.Start(stage, scope); err != nil {
			t.Fatal(err)
		}
	}
	if err := endpoints.Remove("default"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("wrong-manager removal: %v", err)
	}
	if len(outbounds.Outbounds()) != 1 || outbounds.Default() != old {
		t.Fatal("invalid removal mutated the default or created a fallback")
	}
	failedStage := adapter.StartStatePostStart
	failed := &lifecycleTestComponent{tag: "default", failStage: &failedStage}
	outbounds.registry = lifecycleOutboundRegistry{create: func(ctx context.Context, _ string) (adapter.Outbound, error) {
		failed.ctx = ctx
		return failed, nil
	}}
	if err := outbounds.Create(context.Background(), nil, outbounds.logger, "default", "test", nil); !errors.Is(err, errManagedStart) {
		t.Fatalf("replacement error: %v", err)
	}
	if old.closes.Load() != 1 || failed.closes.Load() != 1 || failed.ctx.Err() != context.Canceled {
		t.Fatal("failed default replacement leaked an instance")
	}
	if outbounds.Default() == nil || outbounds.Default().Tag() != "" {
		t.Fatal("failed sole-default replacement left no implicit fallback")
	}
	if err := outbounds.Remove(""); err == nil || outbounds.Default() == nil {
		t.Fatal("last implicit fallback can be removed")
	}
	recovered := &lifecycleTestComponent{tag: "default"}
	outbounds.registry = lifecycleOutboundRegistry{create: func(context.Context, string) (adapter.Outbound, error) { return recovered, nil }}
	if err := outbounds.Create(context.Background(), nil, outbounds.logger, "default", "test", nil); err != nil {
		t.Fatal(err)
	}
	if outbounds.Default() != recovered {
		t.Fatal("recreated configured default was not selected")
	}
}

func TestBoxConstructorFailureCancelsCreatedComponents(t *testing.T) {
	panelLog.InitLogger(logging.ERROR)
	registry := inbound.NewRegistry()
	var contexts []context.Context
	var released []chan struct{}
	var addresses []string
	inbound.Register[option.DirectInboundOptions](registry, "test", func(ctx context.Context, _ adapter.Router, _ log.ContextLogger, tag string, _ option.DirectInboundOptions) (adapter.Inbound, error) {
		contexts = append(contexts, ctx)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = listener.Close() })
		addresses = append(addresses, listener.Addr().String())
		done := make(chan struct{})
		released = append(released, done)
		go func() {
			<-ctx.Done()
			_ = listener.Close()
			close(done)
		}()
		if tag == "fail" {
			return nil, errManagedStart
		}
		return &lifecycleTestComponent{tag: tag, ctx: ctx}, nil
	})
	ctx := Context(context.Background(), registry, OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
	_, err := NewBox(Options{Context: ctx, Options: option.Options{Inbounds: []option.Inbound{{Type: "test", Tag: "first", Options: &option.DirectInboundOptions{}}, {Type: "test", Tag: "fail", Options: &option.DirectInboundOptions{}}}}})
	// NovaPanel's common.NewError formats its arguments without wrapping them.
	if err == nil || !strings.Contains(err.Error(), "initialize inbound[") || !strings.Contains(err.Error(), errManagedStart.Error()) {
		t.Fatalf("constructor failure: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("constructor contexts=%d", len(contexts))
	}
	for _, ctx := range contexts {
		if ctx.Err() != context.Canceled {
			t.Fatal("failed NewBox retained a component context")
		}
	}
	for i, done := range released {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("constructor resource did not stop after cancellation")
		}
		requireLifecyclePortReleased(t, addresses[i])
	}
}

func TestBoxStartFailureClosesStartedScopes(t *testing.T) {
	panelLog.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "failed-start.db")); err != nil {
		t.Fatal(err)
	}
	registry := inbound.NewRegistry()
	failedStage := adapter.StartStateStart
	components := []*lifecycleTestComponent{
		{tag: "first", address: lifecycleTestAddress(t)},
		{tag: "fail", address: lifecycleTestAddress(t), failStage: &failedStage},
	}
	inbound.Register[option.DirectInboundOptions](registry, "test", func(ctx context.Context, _ adapter.Router, _ log.ContextLogger, tag string, _ option.DirectInboundOptions) (adapter.Inbound, error) {
		for _, component := range components {
			if component.tag == tag {
				component.ctx = ctx
				return component, nil
			}
		}
		return nil, os.ErrInvalid
	})
	ctx := Context(context.Background(), registry, OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
	box, err := NewBox(Options{Context: ctx, Options: option.Options{Log: &option.LogOptions{Disabled: true}, Inbounds: []option.Inbound{
		{Type: "test", Tag: "first", Options: &option.DirectInboundOptions{}},
		{Type: "test", Tag: "fail", Options: &option.DirectInboundOptions{}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	if err := box.Start(); !errors.Is(err, errManagedStart) {
		t.Fatalf("start failure: %v", err)
	}
	if err := box.Close(); err != nil {
		t.Fatal(err)
	}
	if err := box.Start(); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed box restarted: %v", err)
	}
	for _, component := range components {
		if component.closes.Load() != 1 || component.ctx.Err() != context.Canceled {
			t.Fatal("failed Box.Start retained or re-closed a component")
		}
		requireLifecyclePortReleased(t, component.address)
	}
}

type lifecycleCountingFactory struct {
	log.Factory
	starts int
}

func (f *lifecycleCountingFactory) Start() error { f.starts++; return nil }

func TestBoxCloseBeforeStartDoesNotReopenLogger(t *testing.T) {
	factory := &lifecycleCountingFactory{Factory: log.NewNOPFactory()}
	scope := adapter.NewScope(context.Background(), factory.Logger())
	box := &Box{scope: scope, logFactory: factory}
	if err := box.Close(); err != nil {
		t.Fatal(err)
	}
	if err := box.Start(); !errors.Is(err, context.Canceled) {
		t.Fatalf("start after close: %v", err)
	}
	if err := box.PreStart(); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-start after close: %v", err)
	}
	if factory.starts != 0 {
		t.Fatal("closed box reopened its logger")
	}
}

func TestCoreImplicitDirectTagCompatibility(t *testing.T) {
	panelLog.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "implicit-direct.db")); err != nil {
		t.Fatal(err)
	}
	runtime := NewCore()
	t.Cleanup(func() { _ = runtime.Stop() })
	if err := runtime.Start([]byte(`{"log":{"disabled":true},"route":{"rules":[{"network":"tcp","outbound":"direct"}]}}`)); err != nil {
		t.Fatalf("legacy implicit direct reference: %v", err)
	}
	manager := runtime.GetInstance().Outbound()
	if manager.Default() == nil || manager.Default().Tag() != "direct" {
		t.Fatal("initial implicit default lost its legacy tag")
	}
	if err := runtime.RemoveOutbound("direct"); err != nil {
		t.Fatal(err)
	}
	if manager.Default() == nil || manager.Default().Tag() != "" {
		t.Fatal("last removal did not reserve a collision-free runtime fallback")
	}
	if err := runtime.AddOutbound([]byte(`{"type":"direct","tag":"direct"}`)); err != nil {
		t.Fatalf("cannot reuse a removed user tag: %v", err)
	}
}

func TestCoreDynamicDefaultAndPortCleanup(t *testing.T) {
	panelLog.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "lifecycle.db")); err != nil {
		t.Fatal(err)
	}
	runtime := NewCore()
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	if err := runtime.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"first"},{"type":"direct","tag":"second"}],"route":{"final":"first"}}`)); err != nil {
		t.Fatal(err)
	}
	box := runtime.GetInstance()
	if box.StatsTracker() == nil || box.ConnTracker() == nil || box.RateLimitTracker() == nil {
		t.Fatal("panel trackers were lost")
	}
	if err := runtime.AddOutbound([]byte(`{"type":"direct","tag":"first"}`)); err != nil {
		t.Fatal(err)
	}
	if box.Outbound().Default().Tag() != "first" {
		t.Fatal("replacement changed selected default")
	}
	if err := runtime.RemoveOutbound("first"); err != nil {
		t.Fatal(err)
	}
	if box.Outbound().Default() == nil || box.Outbound().Default().Tag() != "second" {
		t.Fatal("default removal did not select remaining outbound")
	}
	if err := runtime.RemoveOutbound("second"); err != nil {
		t.Fatal(err)
	}
	if box.Outbound().Default() == nil || box.Outbound().Default().Type() != "direct" {
		t.Fatal("last removal did not install direct fallback")
	}
	address := lifecycleTestAddress(t)
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	config := []byte(fmt.Sprintf(`{"type":"mixed","tag":"loopback","listen":"127.0.0.1","listen_port":%s}`, portText))
	for range 16 {
		if err := runtime.AddInbound(config); err != nil {
			t.Fatal(err)
		}
		if err := runtime.AddInbound(config); err != nil {
			t.Fatalf("same-port replacement: %v", err)
		}
		if err := runtime.RemoveInbound("loopback"); err != nil {
			t.Fatal(err)
		}
		requireLifecyclePortReleased(t, address)
	}
	if err := runtime.AddInbound(config); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if err := box.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	requireLifecyclePortReleased(t, address)
}
