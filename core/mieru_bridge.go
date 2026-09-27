package core

import (
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
)

var mieruBridgeInboundTag atomic.Pointer[string]
var mieruSourceIPsResolver atomic.Pointer[func(inboundTag, username string) []string]

// SetMieruBridgeInboundTag identifies the SOCKS bridge used for Mieru traffic.
func SetMieruBridgeInboundTag(tag string) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		mieruBridgeInboundTag.Store(nil)
		return
	}
	mieruBridgeInboundTag.Store(&tag)
}

// SetMieruSourceIPsResolver provides the current remote client IPs for a Mieru
// user without coupling core tracking to the Mieru service implementation.
func SetMieruSourceIPsResolver(resolver func(inboundTag, username string) []string) {
	if resolver == nil {
		mieruSourceIPsResolver.Store(nil)
		return
	}
	mieruSourceIPsResolver.Store(&resolver)
}

func mieruSourceIPs(inboundTag, username string) []string {
	resolver := mieruSourceIPsResolver.Load()
	if resolver == nil || strings.TrimSpace(username) == "" {
		return nil
	}
	return (*resolver)(inboundTag, username)
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
