package core

import (
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
)

var mieruBridgeInboundTag atomic.Pointer[string]

// SetMieruBridgeInboundTag identifies the SOCKS bridge used for Mieru traffic.
func SetMieruBridgeInboundTag(tag string) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		mieruBridgeInboundTag.Store(nil)
		return
	}
	mieruBridgeInboundTag.Store(&tag)
}

func normalizeTrackedSource(inboundTag, source string) string {
	tag := mieruBridgeInboundTag.Load()
	if tag == nil || inboundTag != *tag || source == "" {
		return source
	}

	host := source
	if parsedHost, _, err := net.SplitHostPort(source); err == nil {
		host = parsedHost
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil || !address.Unmap().IsLoopback() {
		return source
	}
	return ""
}
