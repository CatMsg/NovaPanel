//go:build !linux

package service

import (
	"context"
	"fmt"
	"net/netip"
)

type masqueTun struct{}

func newMasqueTun(tag string, peerPrefix netip.Prefix, mtu int) (*masqueTun, error) {
	return nil, fmt.Errorf("masque server requires Linux TUN support")
}

func (t *masqueTun) ReadPacket(ctx context.Context, buf []byte) (int, error) {
	return 0, context.Canceled
}

func (t *masqueTun) WritePacket(packet []byte) error {
	return context.Canceled
}

func (t *masqueTun) configureKernelForwarding() error {
	return context.Canceled
}

func (t *masqueTun) Close() error {
	return nil
}

func masqueTunDiagnostics(runtime *masqueRuntime) []MasqueDiagnostic {
	return []MasqueDiagnostic{
		{ID: "tun", Status: "info", Title: "TUN 接口", Detail: "仅 Linux 服务器支持"},
		{ID: "forwarding", Status: "info", Title: "IPv4 转发", Detail: "仅 Linux 服务器检查"},
	}
}
