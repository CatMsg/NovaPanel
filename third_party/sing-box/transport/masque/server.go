package masque

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	transportHTTP "github.com/sagernet/sing-box/transport/http"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"

	"go4.org/netipx"
)

type ServerHandler interface {
	WriteInboundBuffers(packetBuffers []*buf.Buffer) error
	WaitWriteInboundBuffers(packetBuffers []*buf.Buffer) error
	FrontHeadroom() int
	NewOutboundQueue(handler func(packetBuffers []*buf.Buffer)) *tun.OutboundQueue
}

// ServerSessionObserver is an optional embedding hook for authenticated proxy
// accounting. Opening runs before Accept; release runs before address reuse.
type ServerSessionObserver interface {
	OnSessionOpened(ctx context.Context, user string, peer M.Socksaddr, assigned []netip.Addr) (sessionContext context.Context, release func(), err error)
	AllowPeerRoutes(ctx context.Context, user string, routes []AddressRange) error
	AllowPeerForwarding(ctx context.Context, user string, destination netip.Addr) error
}

type ServerSessionSharingPolicy interface {
	ShareSessionAddress(ctx context.Context, user string, address netip.Addr) bool
}

type ServerSessionPacketObserver interface {
	OnSessionPacket(ctx context.Context, user string, peer M.Socksaddr, protocol uint8, source, destination M.Socksaddr)
}

type ServerSessionAddressPolicy interface {
	SessionAddress(context.Context, string) netip.Addr
	ReservedSessionAddress(netip.Addr) bool
}

type ServerSessionRoutePolicy interface {
	SessionRoutes(context.Context, string, []AddressRange) ([]AddressRange, error)
}

type ServerOptions struct {
	Context         context.Context
	Logger          logger.ContextLogger
	Path            string
	Address         []netip.Prefix
	AdvertiseRoutes []netip.Prefix
	Resolve         func(ctx context.Context, domain string) ([]netip.Addr, error)
	Handler         ServerHandler
	SessionObserver ServerSessionObserver
}

type Server struct {
	ctx               context.Context
	logger            logger.ContextLogger
	template          *Template
	pools             []*addressPool
	inet4Address      netip.Addr
	inet6Address      netip.Addr
	advertiseRoutes   *netipx.IPSet
	resolve           func(ctx context.Context, domain string) ([]netip.Addr, error)
	handler           ServerHandler
	sessionObserver   ServerSessionObserver
	access            sync.RWMutex
	addresses         map[netip.Addr][]*serverSession
	sharedAddressRefs map[netip.Addr]int
	flows             map[packetFlowKey]packetFlowEntry
	advertisements    []*serverSession
}

type serverSession struct {
	*session
	queue            *tun.OutboundQueue
	server           *Server
	ctx              context.Context
	user             string
	peer             M.Socksaddr
	addresses        []netip.Addr
	sharedAddresses  map[netip.Addr]bool
	advertisedRoutes []AddressRange
	peerRoutes       []AddressRange
}

const packetFlowTTL = 5 * time.Minute

type packetFlowKey struct {
	protocol uint8
	source   netip.AddrPort
	dest     netip.AddrPort
}

type packetFlowEntry struct {
	session  *serverSession
	lastSeen time.Time
}

func NewServer(options ServerOptions) (*Server, error) {
	template, err := ParseTemplate(options.Path)
	if err != nil {
		return nil, err
	}
	if len(options.Address) == 0 {
		return nil, E.New("missing address")
	}
	server := &Server{
		ctx:               options.Context,
		logger:            options.Logger,
		template:          template,
		resolve:           options.Resolve,
		handler:           options.Handler,
		sessionObserver:   options.SessionObserver,
		addresses:         make(map[netip.Addr][]*serverSession),
		sharedAddressRefs: make(map[netip.Addr]int),
		flows:             make(map[packetFlowKey]packetFlowEntry),
	}
	var builder netipx.IPSetBuilder
	for _, address := range options.Address {
		builder.AddPrefix(address.Masked())
		if address.Addr().Is4() {
			if server.inet4Address.IsValid() {
				return nil, E.New("multiple IPv4 addresses are not supported")
			}
			server.inet4Address = address.Addr()
		} else {
			if server.inet6Address.IsValid() {
				return nil, E.New("multiple IPv6 addresses are not supported")
			}
			server.inet6Address = address.Addr()
		}
		pool := newAddressPool(address)
		probe, allocated := pool.allocate()
		if !allocated {
			return nil, E.New("address ", address, " leaves no addresses to assign")
		}
		pool.release(probe)
		pool.next = pool.prefix.Addr()
		server.pools = append(server.pools, pool)
	}
	if len(options.AdvertiseRoutes) == 0 {
		builder.AddPrefix(netip.PrefixFrom(netip.IPv4Unspecified(), 0))
		builder.AddPrefix(netip.PrefixFrom(netip.IPv6Unspecified(), 0))
	}
	for _, route := range options.AdvertiseRoutes {
		builder.AddPrefix(route)
	}
	server.advertiseRoutes, err = builder.IPSet()
	if err != nil {
		return nil, E.Cause(err, "build advertised routes")
	}
	return server, nil
}

func (s *Server) NewTunnelRequest(ctx context.Context, request transportHTTP.TunnelRequest) {
	scope, matched, err := s.template.Match(request.Request().URL)
	if !matched {
		s.logger.ErrorContext(ctx, "process connection from ", request.Source(), ": unexpected path: ", request.Request().URL.Path)
		request.Reject(http.StatusNotFound, nil)
		return
	}
	if err != nil {
		s.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", request.Source()))
		request.Reject(http.StatusBadRequest, nil)
		return
	}
	var builder netipx.IPSetBuilder
	switch {
	case scope.Domain != "":
		addresses, resolveErr := s.resolve(ctx, scope.Domain)
		if resolveErr != nil {
			s.logger.ErrorContext(ctx, E.Cause(resolveErr, "process connection from ", request.Source(), ": resolve ", scope.Domain))
			request.Reject(http.StatusBadGateway, http.Header{"Proxy-Status": []string{"sing-box; error=dns_error"}})
			return
		}
		for _, address := range addresses {
			builder.Add(address)
		}
		builder.Intersect(s.advertiseRoutes)
	case scope.Prefix.IsValid():
		builder.AddPrefix(scope.Prefix)
		builder.Intersect(s.advertiseRoutes)
	default:
		builder.AddSet(s.advertiseRoutes)
	}
	current := &serverSession{
		server:          s,
		ctx:             ctx,
		sharedAddresses: make(map[netip.Addr]bool),
	}
	current.user, _ = auth.UserFromContext[string](ctx)
	addressPolicy, _ := s.sessionObserver.(ServerSessionAddressPolicy)
	sharingPolicy, _ := s.sessionObserver.(ServerSessionSharingPolicy)
	for _, pool := range s.pools {
		var address netip.Addr
		var allocated bool
		if addressPolicy != nil {
			address = addressPolicy.SessionAddress(ctx, current.user)
		}
		if address.IsValid() {
			shared := sharingPolicy != nil && sharingPolicy.ShareSessionAddress(ctx, current.user, address)
			s.access.Lock()
			pool.access.Lock()
			_, inUse := pool.allocated[address]
			if pool.prefix.Contains(address) && pool.usable(address) && (!inUse || shared) {
				if !inUse {
					pool.allocated[address] = struct{}{}
				}
				s.sharedAddressRefs[address]++
				current.sharedAddresses[address] = true
				allocated = true
			}
			pool.access.Unlock()
			s.access.Unlock()
		} else {
			var firstSkipped netip.Addr
			for attempts := 0; attempts < 65536; attempts++ {
				address, allocated = pool.allocate()
				if !allocated || addressPolicy == nil || !addressPolicy.ReservedSessionAddress(address) {
					break
				}
				pool.release(address)
				allocated = false
				// An exhausted pool may repeatedly return its only inactive
				// reserved address. Stop after one full cycle, not 65536 scans.
				if address == firstSkipped {
					break
				}
				if !firstSkipped.IsValid() {
					firstSkipped = address
				}
			}
		}
		if !allocated {
			continue
		}
		current.addresses = append(current.addresses, address)
	}
	defer s.releaseSession(current)
	if len(current.addresses) == 0 {
		s.logger.ErrorContext(ctx, "process connection from ", request.Source(), ": address pool exhausted")
		request.Reject(http.StatusServiceUnavailable, nil)
		return
	}
	var familyBuilder netipx.IPSetBuilder
	for _, address := range current.addresses {
		if address.Is4() {
			familyBuilder.AddPrefix(netip.PrefixFrom(netip.IPv4Unspecified(), 0))
		} else {
			familyBuilder.AddPrefix(netip.PrefixFrom(netip.IPv6Unspecified(), 0))
		}
	}
	families, _ := familyBuilder.IPSet()
	builder.Intersect(families)
	routeSet, err := builder.IPSet()
	if err != nil {
		s.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", request.Source(), ": build routes"))
		request.Reject(http.StatusInternalServerError, nil)
		return
	}
	current.advertisedRoutes = common.Map(routeSet.Ranges(), func(it netipx.IPRange) AddressRange {
		return AddressRange{Start: it.From(), End: it.To(), Protocol: scope.Protocol}
	})
	if len(current.advertisedRoutes) == 0 {
		s.logger.ErrorContext(ctx, "process connection from ", request.Source(), ": requested target is not routable")
		request.Reject(http.StatusForbidden, nil)
		return
	}
	if s.sessionObserver != nil {
		peer := request.Source()
		if actual := M.ParseSocksaddr(request.Request().RemoteAddr).Unwrap(); actual.IsValid() {
			peer = actual
		}
		current.peer = peer
		sessionContext, release, observeErr := s.sessionObserver.OnSessionOpened(ctx, current.user, peer, slices.Clone(current.addresses))
		if sessionContext != nil {
			ctx = sessionContext
			current.ctx = sessionContext
		}
		if release != nil {
			defer release()
		}
		if observeErr != nil {
			request.Reject(http.StatusForbidden, nil)
			return
		}
	}
	stream, err := request.Accept()
	if err != nil {
		s.logger.ErrorContext(ctx, E.Cause(err, "process connection from ", request.Source()))
		return
	}
	current.session = newSession(s.ctx, stream, current, s.receivedPacketHeadroom)
	current.queue = s.handler.NewOutboundQueue(func(packetBuffers []*buf.Buffer) {
		err := current.writePackets(packetBuffers)
		if err != nil {
			current.cancel(err)
		}
	})
	s.access.Lock()
	for _, address := range current.addresses {
		s.addresses[address] = append(s.addresses[address], current)
	}
	s.access.Unlock()
	assignedAddresses := strings.Join(common.Map(current.addresses, netip.Addr.String), " ")
	if current.user != "" {
		s.logger.InfoContext(ctx, "[", current.user, "] inbound tunnel from ", request.Source(), " assigned ", assignedAddresses)
	} else {
		s.logger.InfoContext(ctx, "inbound tunnel from ", request.Source(), " assigned ", assignedAddresses)
	}
	err = current.writeCapsule(newAddressCapsule(capsuleTypeAddressAssign, current.assignedAddresses(nil)))
	if err == nil {
		err = current.writeCapsule(newRouteCapsule(current.advertisedRoutes))
	}
	if err != nil {
		current.cancel(err)
	}
	err = current.run()
	if err != nil && !E.IsClosedOrCanceled(err) {
		s.logger.ErrorContext(ctx, E.Cause(err, "tunnel from ", request.Source(), " closed"))
	} else {
		s.logger.DebugContext(ctx, "tunnel from ", request.Source(), " closed")
	}
}

func (s *Server) releaseSession(current *serverSession) {
	s.access.Lock()
	for _, address := range current.addresses {
		currentSessions := slices.DeleteFunc(s.addresses[address], func(it *serverSession) bool { return it == current })
		if len(currentSessions) == 0 {
			delete(s.addresses, address)
		} else {
			s.addresses[address] = currentSessions
		}
	}
	for key, entry := range s.flows {
		if entry.session == current {
			delete(s.flows, key)
		}
	}
	var addressesToRelease []netip.Addr
	for _, address := range current.addresses {
		if !current.sharedAddresses[address] {
			addressesToRelease = append(addressesToRelease, address)
			continue
		}
		if refs := s.sharedAddressRefs[address]; refs > 1 {
			s.sharedAddressRefs[address] = refs - 1
		} else {
			delete(s.sharedAddressRefs, address)
			addressesToRelease = append(addressesToRelease, address)
		}
	}
	for _, address := range addressesToRelease {
		for _, pool := range s.pools {
			if pool.prefix.Contains(address) {
				pool.release(address)
			}
		}
	}
	s.advertisements = slices.DeleteFunc(s.advertisements, func(it *serverSession) bool {
		return it == current
	})
	s.access.Unlock()
	if current.queue != nil {
		current.queue.Close()
	}
}

func (s *Server) lookup(destination netip.Addr, protocol uint8) *serverSession {
	s.access.RLock()
	defer s.access.RUnlock()
	current := s.addresses[destination]
	if len(current) == 1 {
		return current[0]
	}
	if len(current) > 1 {
		return nil
	}
	if destination == s.inet4Address || destination == s.inet6Address {
		return nil
	}
	for _, advertisement := range slices.Backward(s.advertisements) {
		if RoutesContain(advertisement.peerRoutes, destination, protocol) {
			return advertisement
		}
	}
	return nil
}

func (s *Server) route(source netip.Addr, destination netip.Addr, protocol uint8) *serverSession {
	target := s.lookup(destination, protocol)
	if target == nil || !target.accepts(source, protocol) {
		return nil
	}
	return target
}

func (s *Server) RouteOutbound(packet []byte) *tun.OutboundQueue {
	source, destination, protocol, valid := packetAddresses(packet)
	if !valid {
		return nil
	}
	target := s.route(source, destination, protocol)
	if target == nil {
		return nil
	}
	return target.queue
}

func (s *Server) Contains(address netip.Addr) bool {
	s.access.RLock()
	defer s.access.RUnlock()
	_, loaded := s.addresses[address]
	return loaded || slices.ContainsFunc(s.advertisements, func(it *serverSession) bool {
		return rangesContain(it.peerRoutes, address)
	})
}

func (s *Server) WritePacketBuffers(packetBuffers []*buf.Buffer, forwarded bool) error {
	var (
		replies       []*buf.Buffer
		currentTarget *serverSession
	)
	currentBatch := make([]*buf.Buffer, 0, len(packetBuffers))
	for _, packetBuffer := range packetBuffers {
		source, destination, protocol, valid := packetAddresses(packetBuffer.Bytes())
		if !valid {
			packetBuffer.Release()
			continue
		}
		errorType := tun.ICMPErrorNoRoute
		target := s.routeFlow(packetBuffer.Bytes())
		if target == nil {
			target = s.route(source, destination, protocol)
		}
		if target != nil && forwarded && !decrementHopLimit(packetBuffer.Bytes()) {
			target = nil
			errorType = tun.ICMPErrorHopLimitExceeded
		}
		if target == nil {
			reply, built := buildICMPError(packetBuffer.Bytes(), errorType, s.inet4Address, s.inet6Address, 0, s.handler.FrontHeadroom())
			if built {
				replies = append(replies, reply)
			}
			packetBuffer.Release()
			continue
		}
		if len(currentBatch) > 0 && target != currentTarget {
			currentTarget.queue.WriteBuffers(currentBatch)
			currentBatch = currentBatch[:0]
		}
		currentTarget = target
		currentBatch = append(currentBatch, packetBuffer)
	}
	if len(currentBatch) > 0 {
		currentTarget.queue.WriteBuffers(currentBatch)
	}
	if len(replies) > 0 {
		return s.handler.WriteInboundBuffers(replies)
	}
	return nil
}

func (s *Server) receivedPacketHeadroom() int {
	return max(PacketHeadroom, s.handler.FrontHeadroom())
}

func (s *Server) Close() error {
	s.access.RLock()
	unique := make(map[*serverSession]struct{})
	for _, currentSessions := range s.addresses {
		for _, current := range currentSessions {
			unique[current] = struct{}{}
		}
	}
	s.access.RUnlock()
	for current := range unique {
		current.cancel(net.ErrClosed)
	}
	return nil
}

func (s *Server) routeFlow(packet []byte) *serverSession {
	key, ok := packetFlow(packet)
	if !ok {
		return nil
	}
	now := time.Now()
	s.access.Lock()
	defer s.access.Unlock()
	for flowKey, entry := range s.flows {
		if now.Sub(entry.lastSeen) > packetFlowTTL {
			delete(s.flows, flowKey)
		}
	}
	entry, loaded := s.flows[key]
	if !loaded || now.Sub(entry.lastSeen) > packetFlowTTL || entry.session.ctx.Err() != nil || !entry.session.accepts(key.source.Addr(), key.protocol) {
		delete(s.flows, key)
		return nil
	}
	entry.lastSeen = now
	s.flows[key] = entry
	return entry.session
}

func packetFlow(packet []byte) (packetFlowKey, bool) {
	source, destination, protocol, valid := packetAddresses(packet)
	if !valid || protocol != 6 && protocol != 17 {
		return packetFlowKey{}, false
	}
	offset, actualProtocol, valid := transportHeaderOffset(packet)
	if !valid || actualProtocol != protocol || len(packet) < offset+4 {
		return packetFlowKey{}, false
	}
	return packetFlowKey{
		protocol: protocol,
		source:   netip.AddrPortFrom(source, binary.BigEndian.Uint16(packet[offset:offset+2])),
		dest:     netip.AddrPortFrom(destination, binary.BigEndian.Uint16(packet[offset+2:offset+4])),
	}, true
}

func (key packetFlowKey) reverse() packetFlowKey {
	return packetFlowKey{protocol: key.protocol, source: key.dest, dest: key.source}
}

func transportHeaderOffset(packet []byte) (int, uint8, bool) {
	if len(packet) == 0 {
		return 0, 0, false
	}
	switch packet[0] >> 4 {
	case 4:
		if len(packet) < 20 {
			return 0, 0, false
		}
		offset := int(packet[0]&0x0f) * 4
		if offset < 20 || len(packet) < offset || binary.BigEndian.Uint16(packet[6:8])&0x1fff != 0 {
			return 0, 0, false
		}
		return offset, packet[9], true
	case 6:
		if len(packet) < 40 {
			return 0, 0, false
		}
		nextHeader, offset := packet[6], 40
		for extensions := 0; extensions < 8; extensions++ {
			switch nextHeader {
			case 0, 43, 60, 135, 139, 140:
				if len(packet) < offset+2 {
					return 0, 0, false
				}
				headerLength := (int(packet[offset+1]) + 1) * 8
				if len(packet) < offset+headerLength {
					return 0, 0, false
				}
				nextHeader, offset = packet[offset], offset+headerLength
			case 44:
				if len(packet) < offset+8 || binary.BigEndian.Uint16(packet[offset+2:offset+4])>>3 != 0 {
					return 0, 0, false
				}
				nextHeader, offset = packet[offset], offset+8
			case 51:
				if len(packet) < offset+2 {
					return 0, 0, false
				}
				headerLength := (int(packet[offset+1]) + 2) * 4
				if len(packet) < offset+headerLength {
					return 0, 0, false
				}
				nextHeader, offset = packet[offset], offset+headerLength
			default:
				return offset, nextHeader, true
			}
		}
	}
	return 0, 0, false
}

func (s *serverSession) accepts(source netip.Addr, protocol uint8) bool {
	return isControlProtocol(protocol) || RoutesContain(s.advertisedRoutes, source, protocol)
}

func (s *serverSession) assignedAddresses(requests []AssignedAddress) []AssignedAddress {
	assigned := make([]AssignedAddress, 0, len(s.addresses)+len(requests))
	answered := make(map[netip.Addr]bool)
	for _, request := range requests {
		index := slices.IndexFunc(s.addresses, func(address netip.Addr) bool {
			return address.Is4() == request.Prefix.Addr().Is4() && !answered[address]
		})
		if index == -1 {
			unspecified := netip.IPv4Unspecified()
			if request.Prefix.Addr().Is6() {
				unspecified = netip.IPv6Unspecified()
			}
			assigned = append(assigned, AssignedAddress{RequestID: request.RequestID, Prefix: netip.PrefixFrom(unspecified, unspecified.BitLen())})
			continue
		}
		address := s.addresses[index]
		answered[address] = true
		assigned = append(assigned, AssignedAddress{RequestID: request.RequestID, Prefix: netip.PrefixFrom(address, address.BitLen())})
	}
	for _, address := range s.addresses {
		if !answered[address] {
			assigned = append(assigned, AssignedAddress{Prefix: netip.PrefixFrom(address, address.BitLen())})
		}
	}
	return assigned
}

func (s *serverSession) handleAddressAssign(addresses []AssignedAddress) error {
	return nil
}

func (s *serverSession) handleAddressRequest(addresses []AssignedAddress) error {
	return s.writeCapsule(newAddressCapsule(capsuleTypeAddressAssign, s.assignedAddresses(addresses)))
}

func (s *serverSession) handleRouteAdvertisement(routes []AddressRange) error {
	if policy, ok := s.server.sessionObserver.(ServerSessionRoutePolicy); ok {
		var err error
		routes, err = policy.SessionRoutes(s.ctx, s.user, slices.Clone(routes))
		if err != nil {
			return err
		}
	}
	if s.server.sessionObserver != nil {
		if err := s.server.sessionObserver.AllowPeerRoutes(s.ctx, s.user, slices.Clone(routes)); err != nil {
			return err
		}
	}
	s.server.access.Lock()
	s.peerRoutes = routes
	s.server.advertisements = slices.DeleteFunc(s.server.advertisements, func(it *serverSession) bool {
		return it == s
	})
	if len(routes) > 0 {
		s.server.advertisements = append(s.server.advertisements, s)
	}
	s.server.access.Unlock()
	s.server.logger.InfoContext(s.ctx, "peer advertised ", len(routes), " routes")
	return nil
}

func (s *serverSession) handlePacket(buffer *buf.Buffer) {
	source, destination, protocol, valid := packetAddresses(buffer.Bytes())
	if !valid {
		buffer.Release()
		return
	}
	s.server.access.RLock()
	peerRoutes := s.peerRoutes
	s.server.access.RUnlock()
	errorType := tun.ICMPErrorNoRoute
	switch {
	case !slices.Contains(s.addresses, source) && !rangesContain(peerRoutes, source):
		errorType = tun.ICMPErrorSourcePolicy
	case destination.IsLinkLocalUnicast() || destination.IsLinkLocalMulticast() || destination.IsInterfaceLocalMulticast():
		buffer.Release()
		return
	case !RoutesContain(s.advertisedRoutes, destination, protocol):
	default:
		target := s.server.lookup(destination, protocol)
		if target == nil && destination != s.server.inet4Address && destination != s.server.inet6Address && slices.ContainsFunc(s.server.pools, func(pool *addressPool) bool {
			return pool.prefix.Contains(destination)
		}) {
			errorType = tun.ICMPErrorAddressUnreachable
			break
		}
		if target == nil {
			if flow, flowOK := packetFlow(buffer.Bytes()); flowOK {
				s.server.recordSessionFlow(flow.reverse(), s)
				if observer, ok := s.server.sessionObserver.(ServerSessionPacketObserver); ok {
					observer.OnSessionPacket(s.ctx, s.user, s.peer, flow.protocol, M.SocksaddrFrom(flow.source.Addr(), flow.source.Port()), M.SocksaddrFrom(flow.dest.Addr(), flow.dest.Port()))
				}
			}
			err := s.server.handler.WaitWriteInboundBuffers([]*buf.Buffer{buffer})
			if err != nil {
				s.server.logger.DebugContext(s.ctx, E.Cause(err, "write packet to device"))
			}
			return
		}
		if !target.accepts(source, protocol) {
			break
		}
		if s.server.sessionObserver != nil {
			if err := s.server.sessionObserver.AllowPeerForwarding(s.ctx, s.user, destination); err != nil {
				break
			}
		}
		if !decrementHopLimit(buffer.Bytes()) {
			errorType = tun.ICMPErrorHopLimitExceeded
			break
		}
		target.queue.WriteBuffers([]*buf.Buffer{buffer})
		return
	}
	reply, built := buildICMPError(buffer.Bytes(), errorType, s.server.inet4Address, s.server.inet6Address, 0, transportHTTP.CapsuleHeadroom)
	buffer.Release()
	if built {
		s.queue.WriteBuffers([]*buf.Buffer{reply})
	}
}

func (s *Server) recordSessionFlow(key packetFlowKey, current *serverSession) {
	now := time.Now()
	s.access.Lock()
	for flowKey, entry := range s.flows {
		if now.Sub(entry.lastSeen) > packetFlowTTL {
			delete(s.flows, flowKey)
		}
	}
	s.flows[key] = packetFlowEntry{session: current, lastSeen: now}
	s.access.Unlock()
}

func (s *serverSession) handlePacketTooBig(buffer *buf.Buffer, mtu int) {
	reply, built := buildICMPError(buffer.Bytes(), tun.ICMPErrorPacketTooBig, s.server.inet4Address, s.server.inet6Address, mtu, s.server.receivedPacketHeadroom())
	buffer.Release()
	if !built {
		return
	}
	_, destination, protocol, _ := packetAddresses(reply.Bytes())
	target := s.server.lookup(destination, protocol)
	if target != nil {
		target.queue.WriteBuffers([]*buf.Buffer{reply})
		return
	}
	err := s.server.handler.WriteInboundBuffers([]*buf.Buffer{reply})
	if err != nil {
		s.server.logger.DebugContext(s.ctx, E.Cause(err, "write packet to device"))
	}
}
