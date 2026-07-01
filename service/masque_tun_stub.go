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

func (t *masqueTun) Close() error {
	return nil
}
