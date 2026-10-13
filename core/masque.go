package core

import (
	"context"
	stdtls "crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	native "github.com/sagernet/sing-box/protocol/masque"
	transport "github.com/sagernet/sing-box/transport/masque"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

var errMASQUEIdentity = errors.New("MASQUE source has no active authenticated session")

type masqueLease struct {
	user        string
	peer        M.Socksaddr
	ctx         context.Context
	cancel      context.CancelFunc
	connections map[io.Closer]struct{}
}

type masqueSessionContextKey struct{}

type masqueFlowKey struct {
	network     string
	source      netip.AddrPort
	destination netip.AddrPort
}

type masqueFlowEntry struct {
	lease    *masqueLease
	lastSeen time.Time
}

const masqueFlowTTL = 5 * time.Minute

type masqueRouter struct {
	adapter.Router
	mu            sync.Mutex
	users         map[string]struct{}
	leases        map[netip.Addr][]*masqueLease
	flows         map[masqueFlowKey]masqueFlowEntry
	compatibility *masqueCompatibility
}

type managedMASQUEEndpoint struct {
	*native.ServerEndpoint
	options  managedMASQUEServerOptions
	identity *masqueRouter
}

func (e *managedMASQUEEndpoint) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if err := e.ServerEndpoint.Start(stage, scope); err != nil {
		return err
	}
	if stage == adapter.StartStateInitialize {
		scope.Add(func() error { e.identity.closeAll(); return nil })
	}
	return nil
}

func (c *Core) MASQUEEndpointMatches(tag string, config []byte) bool {
	if c == nil || !c.IsRunning() {
		return false
	}
	current, exists := c.instance.Endpoint().Get(tag)
	managed, ok := current.(*managedMASQUEEndpoint)
	if !exists || !ok {
		return false
	}
	var desired option.Endpoint
	if desired.UnmarshalJSONContext(c.GetCtx(), config) != nil {
		return false
	}
	options, ok := desired.Options.(*managedMASQUEServerOptions)
	return ok && reflect.DeepEqual(managed.options, *options)
}

func registerMASQUEEndpoints(registry *endpoint.Registry) {
	native.RegisterEndpoint(registry)
	endpoint.Register[managedMASQUEServerOptions](registry, "masque-server", newMASQUEServerEndpoint)
}

func newMASQUEServerEndpoint(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options managedMASQUEServerOptions) (adapter.Endpoint, error) {
	if options.System || len(options.Users) == 0 {
		return nil, errors.New("managed MASQUE requires system:false and at least one authenticated user")
	}
	wrapped := &masqueRouter{Router: router, users: make(map[string]struct{}), leases: make(map[netip.Addr][]*masqueLease), flows: make(map[masqueFlowKey]masqueFlowEntry)}
	for _, user := range options.Users {
		if user.Username == "" || user.Password == "" {
			return nil, errors.New("MASQUE username and password are required")
		}
		if _, exists := wrapped.users[user.Username]; exists {
			return nil, errors.New("duplicate MASQUE username")
		}
		wrapped.users[user.Username] = struct{}{}
	}
	compatibility, err := newMASQUECompatibility(options)
	if err != nil {
		return nil, err
	}
	wrapped.compatibility = compatibility
	ctx = service.ContextWith[transport.ServerSessionObserver](ctx, wrapped)
	nativeOptions := options.MASQUEServerEndpointOptions
	if compatibility != nil {
		ctx = service.ContextWith[native.ServerHTTPOptionsCustomizer](ctx, compatibility)
		tlsOptions := *nativeOptions.TLS
		tlsOptions.ClientAuthentication = option.ClientAuthType(stdtls.RequestClientCert)
		nativeOptions.TLS = &tlsOptions
	}
	value, err := native.NewServerEndpoint(ctx, wrapped, logger, tag, nativeOptions)
	if err != nil {
		return nil, err
	}
	return &managedMASQUEEndpoint{ServerEndpoint: value.(*native.ServerEndpoint), options: options, identity: wrapped}, nil
}

func (r *masqueRouter) closeAll() {
	r.mu.Lock()
	leases := make(map[*masqueLease]struct{})
	for _, addressLeases := range r.leases {
		for _, lease := range addressLeases {
			leases[lease] = struct{}{}
		}
	}
	r.leases = make(map[netip.Addr][]*masqueLease)
	r.flows = make(map[masqueFlowKey]masqueFlowEntry)
	var connections []io.Closer
	for lease := range leases {
		for conn := range lease.connections {
			connections = append(connections, conn)
		}
		lease.connections = nil
	}
	r.mu.Unlock()
	for lease := range leases {
		lease.cancel()
	}
	for _, conn := range connections {
		conn.Close()
	}
}

func (r *masqueRouter) OnSessionOpened(ctx context.Context, user string, peer M.Socksaddr, assigned []netip.Addr) (context.Context, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, configured := r.users[user]; !configured || !peer.Addr.IsValid() || len(assigned) == 0 {
		return nil, nil, errMASQUEIdentity
	}
	for _, address := range assigned {
		if !address.IsValid() {
			return nil, nil, errMASQUEIdentity
		}
		for _, current := range r.leases[address] {
			if current.user != user {
				return nil, nil, errMASQUEIdentity
			}
		}
	}
	baseContext, cancel := context.WithCancel(ctx)
	lease := &masqueLease{user: user, peer: peer, cancel: cancel, connections: make(map[io.Closer]struct{})}
	sessionCtx := context.WithValue(baseContext, masqueSessionContextKey{}, lease)
	lease.ctx = sessionCtx
	for _, address := range assigned {
		r.leases[address] = append(r.leases[address], lease)
	}
	var once sync.Once
	return sessionCtx, func() {
		once.Do(func() {
			r.mu.Lock()
			for _, address := range assigned {
				current := slices.DeleteFunc(r.leases[address], func(it *masqueLease) bool { return it == lease })
				if len(current) == 0 {
					delete(r.leases, address)
				} else {
					r.leases[address] = current
				}
			}
			for key, entry := range r.flows {
				if entry.lease == lease {
					delete(r.flows, key)
				}
			}
			connections := make([]io.Closer, 0, len(lease.connections))
			for connection := range lease.connections {
				connections = append(connections, connection)
			}
			lease.connections = nil
			r.mu.Unlock()
			cancel()
			for _, connection := range connections {
				connection.Close()
			}
		})
	}, nil
}

func (r *masqueRouter) ShareSessionAddress(ctx context.Context, user string, address netip.Addr) bool {
	return r.SessionAddress(ctx, user) == address
}

func (r *masqueRouter) OnSessionPacket(ctx context.Context, user string, _ M.Socksaddr, protocol uint8, source, destination M.Socksaddr) {
	lease, ok := ctx.Value(masqueSessionContextKey{}).(*masqueLease)
	if !ok || lease == nil || lease.user != user {
		return
	}
	network := N.NetworkTCP
	if protocol == 17 {
		network = N.NetworkUDP
	} else if protocol != 6 {
		return
	}
	key := masqueFlowKey{network: network, source: source.AddrPort(), destination: destination.AddrPort()}
	if !key.source.IsValid() || !key.destination.IsValid() {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if lease.ctx.Err() != nil || lease.connections == nil {
		return
	}
	for flowKey, entry := range r.flows {
		if now.Sub(entry.lastSeen) > masqueFlowTTL {
			delete(r.flows, flowKey)
		}
	}
	r.flows[key] = masqueFlowEntry{lease: lease, lastSeen: now}
}

func (*masqueRouter) AllowPeerRoutes(_ context.Context, _ string, routes []transport.AddressRange) error {
	if len(routes) != 0 {
		return errors.New("client route advertisements are disabled for managed MASQUE")
	}
	return nil
}

func (*masqueRouter) AllowPeerForwarding(context.Context, string, netip.Addr) error {
	return errors.New("tunnel-to-tunnel forwarding bypasses managed user controls")
}

func (r *masqueRouter) bind(metadata adapter.InboundContext, connection io.Closer) (context.Context, adapter.InboundContext, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var lease *masqueLease
	destination := metadata.Destination
	if metadata.OriginDestination.IsValid() {
		destination = metadata.OriginDestination
	}
	key := masqueFlowKey{network: metadata.Network, source: metadata.Source.AddrPort(), destination: destination.AddrPort()}
	entry, hasFlow := r.flows[key]
	if hasFlow && time.Since(entry.lastSeen) <= masqueFlowTTL && entry.lease.ctx.Err() == nil {
		lease = entry.lease
	} else {
		delete(r.flows, key)
		current := r.leases[metadata.Source.Addr]
		if len(current) == 1 {
			lease = current[0]
		}
	}
	if lease == nil || lease.ctx.Err() != nil || lease.connections == nil {
		return nil, metadata, nil, errMASQUEIdentity
	}
	metadata.User, metadata.Source = lease.user, lease.peer
	if connection != nil {
		lease.connections[connection] = struct{}{}
	}
	return lease.ctx, metadata, func() {
		r.mu.Lock()
		delete(lease.connections, connection)
		r.mu.Unlock()
	}, nil
}

func (r *masqueRouter) RouteConnectionEx(_ context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	metadata.Network = N.NetworkTCP
	ctx, metadata, unbind, err := r.bind(metadata, conn)
	if err != nil {
		N.CloseOnHandshakeFailure(conn, onClose, err)
		return
	}
	r.Router.RouteConnectionEx(ctx, conn, metadata, func(err error) {
		unbind()
		if onClose != nil {
			onClose(err)
		}
	})
}

func (r *masqueRouter) RoutePacketConnectionEx(_ context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	metadata.Network = N.NetworkUDP
	ctx, metadata, unbind, err := r.bind(metadata, conn)
	if err != nil {
		N.CloseOnHandshakeFailure(conn, onClose, err)
		return
	}
	r.Router.RoutePacketConnectionEx(ctx, conn, metadata, func(err error) {
		unbind()
		if onClose != nil {
			onClose(err)
		}
	})
}

func (r *masqueRouter) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	r.RouteConnectionEx(ctx, conn, metadata, nil)
	return nil
}

func (r *masqueRouter) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	r.RoutePacketConnectionEx(ctx, conn, metadata, nil)
	return nil
}

func (r *masqueRouter) PreMatch(metadata adapter.InboundContext, _ []byte) adapter.PreMatchResult {
	if _, _, _, err := r.bind(metadata, nil); err != nil || (metadata.Network != N.NetworkTCP && metadata.Network != N.NetworkUDP) {
		return adapter.PreMatchResult{Action: adapter.PreMatchReject}
	}
	// Panel flow trackers cannot enforce user accounting/limits. Route TCP/UDP
	// through the normal connection path instead of direct flow forwarding.
	return adapter.PreMatchResult{Action: adapter.PreMatchContinue}
}

func (r *masqueRouter) HijackDNSPacket(_ context.Context, payload []byte, writer N.PacketWriter, metadata adapter.InboundContext) {
	metadata.Network = N.NetworkUDP
	ctx, metadata, _, err := r.bind(metadata, nil)
	if err != nil {
		return
	}
	r.Router.HijackDNSPacket(ctx, payload, writer, metadata)
}
