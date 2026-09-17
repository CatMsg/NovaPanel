package core

import (
	"context"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

type ConnectionInfo struct {
	ID          string
	Conn        net.Conn
	PacketConn  network.PacketConn
	Inbound     string
	Outbound    string
	User        string
	Type        string // "tcp" or "udp"
	Source      string
	Destination string
	Domain      string
	Protocol    string
	Rule        string
	StartedAt   time.Time
	Upload      atomic.Uint64
	Download    atomic.Uint64
}

// SessionSnapshot is a read-only view of an active routed connection.
type SessionSnapshot struct {
	ID          string    `json:"id"`
	Inbound     string    `json:"inbound,omitempty"`
	Outbound    string    `json:"outbound,omitempty"`
	User        string    `json:"user,omitempty"`
	Network     string    `json:"network"`
	Source      string    `json:"source,omitempty"`
	Destination string    `json:"destination,omitempty"`
	Domain      string    `json:"domain,omitempty"`
	Protocol    string    `json:"protocol,omitempty"`
	Rule        string    `json:"rule,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	Upload      uint64    `json:"upload"`
	Download    uint64    `json:"download"`
}

// ResourceSnapshot describes the resources used by currently tracked
// connections. It is intentionally reduced to names for status reporting.
type ResourceSnapshot struct {
	Inbound  []string
	Outbound []string
	User     []string
}

type ConnTracker struct {
	access      sync.Mutex
	connections map[string]*ConnectionInfo
}

func NewConnTracker() *ConnTracker {
	return &ConnTracker{
		connections: make(map[string]*ConnectionInfo),
	}
}

func (c *ConnTracker) Reset() {
	c.access.Lock()
	connections := make([]*ConnectionInfo, 0, len(c.connections))
	for _, connInfo := range c.connections {
		connections = append(connections, connInfo)
	}
	c.connections = make(map[string]*ConnectionInfo)
	c.access.Unlock()
	closeTrackedConnections(connections)
}

func (c *ConnTracker) generateConnectionID() string {
	return uuid.Must(uuid.NewV4()).String()
}

func (c *ConnTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	connID := c.generateConnectionID()
	connInfo := &ConnectionInfo{
		ID:          connID,
		Conn:        conn,
		Inbound:     metadata.Inbound,
		Outbound:    matchOutbound.Tag(),
		User:        metadata.User,
		Type:        "tcp",
		Source:      socksaddrString(metadata.Source),
		Destination: socksaddrString(metadata.Destination),
		Domain:      metadata.Domain,
		Protocol:    metadata.Protocol,
		Rule:        routeRuleString(metadata, matchedRule),
		StartedAt:   time.Now(),
	}

	c.trackConnection(connID, connInfo)

	return c.createWrappedConn(conn, connID)
}

func (c *ConnTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	connID := c.generateConnectionID()
	connInfo := &ConnectionInfo{
		ID:          connID,
		PacketConn:  conn,
		Inbound:     metadata.Inbound,
		Outbound:    matchOutbound.Tag(),
		User:        metadata.User,
		Type:        "udp",
		Source:      socksaddrString(metadata.Source),
		Destination: socksaddrString(metadata.Destination),
		Domain:      metadata.Domain,
		Protocol:    metadata.Protocol,
		Rule:        routeRuleString(metadata, matchedRule),
		StartedAt:   time.Now(),
	}

	c.trackConnection(connID, connInfo)

	return c.createWrappedPacketConn(conn, connID)
}

func (c *ConnTracker) RoutedFlow(context.Context, adapter.InboundContext, adapter.Rule, adapter.Outbound) tun.FlowTracker {
	return nil
}

func (c *ConnTracker) CloseConnByInbound(inbound string) int {
	c.access.Lock()
	connections := make([]*ConnectionInfo, 0)
	for connID, connInfo := range c.connections {
		if connInfo.Inbound == inbound {
			delete(c.connections, connID)
			connections = append(connections, connInfo)
		}
	}
	c.access.Unlock()
	closeTrackedConnections(connections)
	return len(connections)
}

// CloseSession closes one active routed connection without holding the tracker
// mutex while the wrapped connection calls back into the tracker.
func (c *ConnTracker) CloseSession(id string) bool {
	c.access.Lock()
	connInfo, ok := c.connections[id]
	if ok {
		delete(c.connections, id)
	}
	c.access.Unlock()
	if !ok {
		return false
	}
	closeTrackedConnections([]*ConnectionInfo{connInfo})
	return true
}

func (c *ConnTracker) trackConnection(connID string, connInfo *ConnectionInfo) {
	c.access.Lock()
	defer c.access.Unlock()
	c.connections[connID] = connInfo
}

func (c *ConnTracker) untrackConnection(connID string) {
	c.access.Lock()
	defer c.access.Unlock()
	delete(c.connections, connID)
}

// Snapshot returns the currently active inbound, outbound, and user names.
// The tracker owns the connection lifecycle, so this is more accurate than
// inferring online state from destructive traffic counters.
func (c *ConnTracker) Snapshot() ResourceSnapshot {
	c.access.Lock()
	defer c.access.Unlock()

	inbounds := make(map[string]struct{})
	outbounds := make(map[string]struct{})
	users := make(map[string]struct{})
	for _, connInfo := range c.connections {
		if connInfo.Inbound != "" {
			inbounds[connInfo.Inbound] = struct{}{}
		}
		if connInfo.Outbound != "" {
			outbounds[connInfo.Outbound] = struct{}{}
		}
		if connInfo.User != "" {
			users[connInfo.User] = struct{}{}
		}
	}

	return ResourceSnapshot{
		Inbound:  sortedResourceNames(inbounds),
		Outbound: sortedResourceNames(outbounds),
		User:     sortedResourceNames(users),
	}
}

// Sessions returns stable copies of active sessions, newest first.
func (c *ConnTracker) Sessions() []SessionSnapshot {
	c.access.Lock()
	result := make([]SessionSnapshot, 0, len(c.connections))
	for _, connInfo := range c.connections {
		result = append(result, SessionSnapshot{
			ID: connInfo.ID, Inbound: connInfo.Inbound, Outbound: connInfo.Outbound,
			User: connInfo.User, Network: connInfo.Type, Source: connInfo.Source,
			Destination: connInfo.Destination, Domain: connInfo.Domain,
			Protocol: connInfo.Protocol, Rule: connInfo.Rule, StartedAt: connInfo.StartedAt,
			Upload: connInfo.Upload.Load(), Download: connInfo.Download.Load(),
		})
	}
	c.access.Unlock()
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAt.After(result[j].StartedAt) })
	return result
}

func sortedResourceNames(names map[string]struct{}) []string {
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (c *ConnTracker) createWrappedConn(conn net.Conn, connID string) *wrappedConn {
	return &wrappedConn{
		Conn:    conn,
		tracker: c,
		connID:  connID,
	}
}

func (c *ConnTracker) createWrappedPacketConn(conn network.PacketConn, connID string) *wrappedPacketConn {
	return &wrappedPacketConn{
		PacketConn: conn,
		tracker:    c,
		connID:     connID,
	}
}

type wrappedConn struct {
	net.Conn
	tracker     *ConnTracker
	connID      string
	untrackOnce sync.Once
}

func (w *wrappedConn) doUntrack() {
	w.untrackOnce.Do(func() {
		w.tracker.untrackConnection(w.connID)
	})
}

func (w *wrappedConn) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if n > 0 {
		w.tracker.addUpload(w.connID, uint64(n))
	}
	if shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return n, err
}

func (w *wrappedConn) Write(b []byte) (int, error) {
	n, err := w.Conn.Write(b)
	if n > 0 {
		w.tracker.addDownload(w.connID, uint64(n))
	}
	if err != nil && shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return n, err
}

func (w *wrappedConn) Close() error {
	w.doUntrack()
	return w.Conn.Close()
}

func (w *wrappedConn) Upstream() any {
	return w.Conn
}

type wrappedPacketConn struct {
	network.PacketConn
	tracker     *ConnTracker
	connID      string
	untrackOnce sync.Once
}

func (w *wrappedPacketConn) doUntrack() {
	w.untrackOnce.Do(func() {
		w.tracker.untrackConnection(w.connID)
	})
}

func (w *wrappedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	dest, err := w.PacketConn.ReadPacket(buffer)
	if err == nil && buffer != nil && buffer.Len() > 0 {
		w.tracker.addUpload(w.connID, uint64(buffer.Len()))
	}
	if shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return dest, err
}

func (w *wrappedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	err := w.PacketConn.WritePacket(buffer, destination)
	if err == nil && buffer != nil && buffer.Len() > 0 {
		w.tracker.addDownload(w.connID, uint64(buffer.Len()))
	}
	if err != nil && shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return err
}

func (w *wrappedPacketConn) Close() error {
	w.doUntrack()
	return w.PacketConn.Close()
}

func (w *wrappedPacketConn) Upstream() any {
	return w.PacketConn
}

func (c *ConnTracker) addUpload(id string, size uint64) {
	c.access.Lock()
	connInfo := c.connections[id]
	c.access.Unlock()
	if connInfo != nil {
		connInfo.Upload.Add(size)
	}
}

func (c *ConnTracker) addDownload(id string, size uint64) {
	c.access.Lock()
	connInfo := c.connections[id]
	c.access.Unlock()
	if connInfo != nil {
		connInfo.Download.Add(size)
	}
}

func closeTrackedConnections(connections []*ConnectionInfo) {
	for _, connInfo := range connections {
		if connInfo.Conn != nil {
			_ = connInfo.Conn.Close()
		}
		if connInfo.PacketConn != nil {
			_ = connInfo.PacketConn.Close()
		}
	}
}

func socksaddrString(address M.Socksaddr) string {
	if !address.IsValid() {
		return ""
	}
	return address.String()
}

func routeRuleString(metadata adapter.InboundContext, matchedRule adapter.Rule) string {
	if metadata.RouteRule != "" {
		return metadata.RouteRule
	}
	if matchedRule != nil {
		return matchedRule.String()
	}
	return ""
}
