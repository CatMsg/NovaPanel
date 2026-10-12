package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

type taggedComponent interface {
	Type() string
	Tag() string
}

type managedComponent[T taggedComponent] struct {
	value T
	scope *adapter.Scope
	stage int
}

// Each live instance owns a separate scope. Removed instances are not retained
// in the upstream Scope.children map until the entire panel shuts down.
type componentSet[T taggedComponent] struct {
	op      sync.Mutex
	access  sync.RWMutex
	kind    string
	logger  log.ContextLogger
	entries []*managedComponent[T]
	byTag   map[string]*managedComponent[T]
	scope   *adapter.Scope
	stage   int
	closed  bool
}

func newComponentSet[T taggedComponent](kind string, logger log.ContextLogger) *componentSet[T] {
	return &componentSet[T]{kind: kind, logger: logger, byTag: make(map[string]*managedComponent[T]), stage: -1}
}

func (m *componentSet[T]) values() []T {
	m.access.RLock()
	defer m.access.RUnlock()
	values := make([]T, 0, len(m.entries))
	for _, entry := range m.entries {
		values = append(values, entry.value)
	}
	return values
}

func (m *componentSet[T]) get(tag string) (value T, found bool) {
	m.access.RLock()
	defer m.access.RUnlock()
	if entry := m.byTag[tag]; entry != nil {
		return entry.value, true
	}
	return value, false
}

func (m *componentSet[T]) startEntry(entry *managedComponent[T], stage adapter.StartStage) error {
	if entry.stage >= int(stage) {
		return nil
	}
	if entry.scope == nil {
		entry.scope = adapter.NewScope(m.scope.Context(), m.logger)
	}
	if lifecycle, ok := any(entry.value).(adapter.Lifecycle); ok {
		name := m.kind + "/" + entry.value.Type() + "[" + entry.value.Tag() + "]"
		if err := entry.scope.Start(name, lifecycle, stage); err != nil {
			return err
		}
	}
	entry.stage = int(stage)
	return nil
}

func closeComponent[T taggedComponent](entry *managedComponent[T]) error {
	if entry.scope == nil {
		return nil
	}
	err := entry.scope.Close()
	entry.scope = nil
	return err
}

func (m *componentSet[T]) prepare(stage adapter.StartStage, scope *adapter.Scope) error {
	if m.closed {
		return os.ErrClosed
	}
	if m.scope == nil {
		m.scope = scope
		scope.Add(m.close)
	}
	if err := scope.Context().Err(); err != nil {
		return err
	}
	m.stage = int(stage)
	return nil
}

func (m *componentSet[T]) start(stage adapter.StartStage, scope *adapter.Scope, skip bool) error {
	m.op.Lock()
	defer m.op.Unlock()
	if err := m.prepare(stage, scope); err != nil {
		return err
	}
	if skip {
		return nil
	}
	for _, entry := range m.entries {
		if err := m.startEntry(entry, stage); err != nil {
			return err
		}
	}
	return nil
}

func (m *componentSet[T]) startValue(value T, stage adapter.StartStage) error {
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed || m.scope == nil {
		return os.ErrClosed
	}
	m.access.RLock()
	entry := m.byTag[value.Tag()]
	m.access.RUnlock()
	if entry == nil || any(entry.value) != any(value) {
		return os.ErrInvalid
	}
	return m.startEntry(entry, stage)
}

func (m *componentSet[T]) create(ctx context.Context, factory func(context.Context) (T, error), validate func(T) error) error {
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed {
		return os.ErrClosed
	}
	var cancel context.CancelFunc
	var stop func() bool
	if m.scope != nil {
		if err := m.scope.Context().Err(); err != nil {
			return err
		}
		// Keep caller metadata (including the outbound tag), but also cancel
		// dynamic instances when the box's lifecycle scope is canceled.
		ctx, cancel = context.WithCancel(ctx)
		stop = context.AfterFunc(m.scope.Context(), cancel)
	}
	candidate := adapter.NewScope(ctx, m.logger)
	if cancel != nil {
		candidate.Add(func() error { stop(); cancel(); return nil })
	}
	value, err := factory(candidate.Context())
	if err != nil {
		return errors.Join(err, candidate.Close())
	}
	if validate != nil {
		if err := validate(value); err != nil {
			return errors.Join(err, candidate.Close())
		}
	}
	m.access.Lock()
	old := m.byTag[value.Tag()]
	if old != nil {
		delete(m.byTag, value.Tag())
		m.entries = slices.DeleteFunc(m.entries, func(entry *managedComponent[T]) bool { return entry == old })
	}
	m.access.Unlock()
	// Release the previous listener before starting its replacement on the same
	// address. A failed replacement is closed and never published to readers.
	if old != nil {
		if err := closeComponent(old); err != nil {
			return errors.Join(fmt.Errorf("close replaced %s[%s]: %w", m.kind, value.Tag(), err), candidate.Close())
		}
	}
	entry := &managedComponent[T]{value: value, scope: candidate, stage: -1}
	if m.scope != nil {
		for _, stage := range adapter.ListStartStages {
			if int(stage) > m.stage {
				break
			}
			if err := m.startEntry(entry, stage); err != nil {
				return errors.Join(err, closeComponent(entry))
			}
		}
	}
	m.access.Lock()
	m.entries = append(m.entries, entry)
	m.byTag[value.Tag()] = entry
	m.access.Unlock()
	return nil
}

func (m *componentSet[T]) remove(tag string, validate func(T) error) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.access.RLock()
	entry := m.byTag[tag]
	m.access.RUnlock()
	if entry == nil {
		return os.ErrInvalid
	}
	if validate != nil {
		if err := validate(entry.value); err != nil {
			return err
		}
	}
	m.access.Lock()
	delete(m.byTag, tag)
	m.entries = slices.DeleteFunc(m.entries, func(item *managedComponent[T]) bool { return item == entry })
	m.access.Unlock()
	return closeComponent(entry)
}

func (m *componentSet[T]) close() error {
	m.op.Lock()
	defer m.op.Unlock()
	m.closed = true
	m.access.Lock()
	entries := m.entries
	m.entries = nil
	clear(m.byTag)
	m.access.Unlock()
	var err error
	for _, entry := range slices.Backward(entries) {
		err = errors.Join(err, closeComponent(entry))
	}
	return err
}

type inboundManager struct {
	*componentSet[adapter.Inbound]
	registry adapter.InboundRegistry
	endpoint adapter.EndpointManager
}

func (m *inboundManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return m.start(stage, scope, false)
}
func (m *inboundManager) Inbounds() []adapter.Inbound { return m.values() }
func (m *inboundManager) Get(tag string) (adapter.Inbound, bool) {
	if value, found := m.get(tag); found {
		return value, true
	}
	return m.endpoint.Get(tag)
}
func (m *inboundManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag, kind string, options any) error {
	return m.create(ctx, func(ctx context.Context) (adapter.Inbound, error) {
		return m.registry.Create(ctx, router, logger, tag, kind, options)
	}, nil)
}
func (m *inboundManager) Remove(tag string) error { return m.remove(tag, nil) }

type endpointManager struct {
	*componentSet[adapter.Endpoint]
	registry adapter.EndpointRegistry
	outbound *outboundManager
}

func (m *endpointManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	m.outbound.graph.Lock()
	defer m.outbound.graph.Unlock()
	return m.start(stage, scope, stage == adapter.StartStateStart)
}
func (m *endpointManager) StartEndpoint(value adapter.Endpoint) error {
	return m.startValue(value, adapter.StartStateStart)
}
func (m *endpointManager) Endpoints() []adapter.Endpoint           { return m.values() }
func (m *endpointManager) Get(tag string) (adapter.Endpoint, bool) { return m.get(tag) }
func (m *endpointManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag, kind string, options any) error {
	m.outbound.graph.Lock()
	defer m.outbound.graph.Unlock()
	if _, exists := m.outbound.get(tag); exists {
		return fmt.Errorf("duplicate outbound/endpoint tag: %s", tag)
	}
	err := m.create(ctx, func(ctx context.Context) (adapter.Endpoint, error) {
		return m.registry.Create(ctx, router, logger, tag, kind, options)
	}, func(value adapter.Endpoint) error {
		if m.scope != nil {
			return m.outbound.validateDependencies(value)
		}
		return nil
	})
	if err != nil {
		err = errors.Join(err, m.outbound.recoverDefault())
	}
	m.outbound.refreshDefault()
	return err
}
func (m *endpointManager) Remove(tag string) error {
	m.outbound.graph.Lock()
	defer m.outbound.graph.Unlock()
	if _, found := m.get(tag); !found {
		return os.ErrInvalid
	}
	if err := m.outbound.ensureFallback(tag); err != nil {
		return err
	}
	err := m.remove(tag, func(value adapter.Endpoint) error { return m.outbound.validateRemoval(value.Tag()) })
	m.outbound.refreshDefault()
	return err
}

type serviceManager struct {
	*componentSet[adapter.Service]
	registry adapter.ServiceRegistry
}

func (m *serviceManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return m.start(stage, scope, false)
}
func (m *serviceManager) Services() []adapter.Service            { return m.values() }
func (m *serviceManager) Get(tag string) (adapter.Service, bool) { return m.get(tag) }
func (m *serviceManager) Create(ctx context.Context, logger log.ContextLogger, tag, kind string, options any) error {
	return m.create(ctx, func(ctx context.Context) (adapter.Service, error) {
		return m.registry.Create(ctx, logger, tag, kind, options)
	}, nil)
}
func (m *serviceManager) Remove(tag string) error { return m.remove(tag, nil) }

type outboundManager struct {
	*componentSet[adapter.Outbound]
	registry    adapter.OutboundRegistry
	endpoint    adapter.EndpointManager
	defaultTag  string
	selectedTag string
	graph       sync.Mutex
	fallback    func(context.Context) (adapter.Outbound, error)
}

func (m *outboundManager) Initialize(fallback func(context.Context) (adapter.Outbound, error)) {
	m.fallback = fallback
}
func (m *outboundManager) Outbounds() []adapter.Outbound { return m.values() }
func (m *outboundManager) Outbound(tag string) (adapter.Outbound, bool) {
	if value, found := m.get(tag); found {
		return value, true
	}
	return m.endpoint.Get(tag)
}
func (m *outboundManager) Default() adapter.Outbound {
	m.access.RLock()
	tag := m.selectedTag
	m.access.RUnlock()
	if value, found := m.Outbound(tag); found {
		return value
	}
	if values := m.values(); len(values) > 0 {
		return values[0]
	}
	return nil
}
func (m *outboundManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	m.graph.Lock()
	defer m.graph.Unlock()
	if stage == adapter.StartStateInitialize {
		if m.defaultTag != "" {
			if _, found := m.Outbound(m.defaultTag); !found {
				return fmt.Errorf("default outbound not found: %s", m.defaultTag)
			}
			m.access.Lock()
			m.selectedTag = m.defaultTag
			m.access.Unlock()
		} else if m.Default() == nil {
			if err := m.create(scope.Context(), m.fallback, nil); err != nil {
				return err
			}
		}
	}
	if err := m.start(stage, scope, stage == adapter.StartStateStart); err != nil {
		return err
	}
	if stage != adapter.StartStateStart {
		return nil
	}
	values := m.values()
	for _, endpoint := range m.endpoint.Endpoints() {
		values = append(values, endpoint)
	}
	started := make(map[string]bool)
	for len(started) < len(values) {
		progress := false
		for _, value := range values {
			if started[value.Tag()] {
				continue
			}
			ready := true
			for _, dependency := range value.Dependencies() {
				if !started[dependency] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			var err error
			// Lifecycle outbounds also structurally satisfy adapter.Endpoint.
			// Use actual manager membership, not a structural type assertion.
			if endpoint, ok := m.endpoint.Get(value.Tag()); ok && any(endpoint) == any(value) {
				err = m.endpoint.StartEndpoint(endpoint)
			} else {
				err = m.startValue(value, stage)
			}
			if err != nil {
				return err
			}
			started[value.Tag()] = true
			progress = true
		}
		if !progress {
			return fmt.Errorf("missing or circular outbound/endpoint dependencies")
		}
	}
	return nil
}
func (m *outboundManager) validateDependencies(value adapter.Outbound) error {
	visited := map[string]bool{value.Tag(): true}
	var visit func(adapter.Outbound) error
	visit = func(current adapter.Outbound) error {
		for _, tag := range current.Dependencies() {
			if visited[tag] {
				return fmt.Errorf("circular outbound dependency: %s", tag)
			}
			dependency, found := m.Outbound(tag)
			if !found {
				return fmt.Errorf("dependency[%s] not found for outbound[%s]", tag, current.Tag())
			}
			visited[tag] = true
			if err := visit(dependency); err != nil {
				return err
			}
			delete(visited, tag)
		}
		return nil
	}
	return visit(value)
}
func (m *outboundManager) validateRemoval(tag string) error {
	values := m.values()
	for _, endpoint := range m.endpoint.Endpoints() {
		values = append(values, endpoint)
	}
	for _, value := range values {
		if value.Tag() != tag && slices.Contains(value.Dependencies(), tag) {
			return fmt.Errorf("outbound[%s] is depended by %s", tag, value.Tag())
		}
	}
	return nil
}
func (m *outboundManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag, kind string, options any) error {
	m.graph.Lock()
	defer m.graph.Unlock()
	if tag == "" {
		return os.ErrInvalid
	}
	if _, exists := m.endpoint.Get(tag); exists {
		return fmt.Errorf("duplicate outbound/endpoint tag: %s", tag)
	}
	current := m.Default()
	err := m.create(ctx, func(ctx context.Context) (adapter.Outbound, error) {
		return m.registry.CreateOutbound(ctx, router, logger, tag, kind, options)
	}, func(value adapter.Outbound) error {
		if m.scope != nil {
			return m.validateDependencies(value)
		}
		return nil
	})
	if err == nil && (tag == m.defaultTag || current == nil || current.Tag() == tag) {
		m.access.Lock()
		m.selectedTag = tag
		m.access.Unlock()
	}
	if err != nil {
		err = errors.Join(err, m.recoverDefault())
	}
	m.refreshDefault()
	return err
}
func (m *outboundManager) Remove(tag string) error {
	m.graph.Lock()
	defer m.graph.Unlock()
	if _, found := m.get(tag); !found {
		return os.ErrInvalid
	}
	if err := m.ensureFallback(tag); err != nil {
		return err
	}
	err := m.remove(tag, func(value adapter.Outbound) error { return m.validateRemoval(value.Tag()) })
	m.refreshDefault()
	return err
}

func (m *outboundManager) refreshDefault() {
	if current := m.Default(); current != nil {
		m.access.Lock()
		m.selectedTag = current.Tag()
		m.access.Unlock()
	}
}

func (m *outboundManager) recoverDefault() error {
	if m.scope == nil || m.Default() != nil {
		return nil
	}
	return m.create(m.scope.Context(), m.fallback, nil)
}

func (m *outboundManager) ensureFallback(removingTag string) error {
	if m.scope == nil {
		return nil
	}
	current := m.Default()
	if current == nil || current.Tag() != removingTag {
		return nil
	}
	for _, value := range m.values() {
		if value.Tag() != removingTag {
			return nil
		}
	}
	if err := m.validateRemoval(removingTag); err != nil {
		return err
	}
	// Keep a usable direct default when deleting the last selectable outbound.
	// The legacy manager could leave Default nil, which is unsafe for routing.
	if removingTag == "" {
		return fmt.Errorf("cannot remove the last implicit fallback outbound")
	}
	return m.create(m.scope.Context(), m.fallback, nil)
}

var (
	_ adapter.InboundManager  = (*inboundManager)(nil)
	_ adapter.EndpointManager = (*endpointManager)(nil)
	_ adapter.OutboundManager = (*outboundManager)(nil)
	_ adapter.ServiceManager  = (*serviceManager)(nil)
)
