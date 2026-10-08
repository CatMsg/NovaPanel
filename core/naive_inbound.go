package core

import (
	"context"
	"net"
	"net/http"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/naive"
)

func registerNaiveInbound(registry *inbound.Registry) {
	inbound.Register[option.NaiveInboundOptions](registry, C.TypeNaive, newNaiveInbound)
}

func newNaiveInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.NaiveInboundOptions) (adapter.Inbound, error) {
	created, err := naive.NewInbound(ctx, router, logger, tag, options)
	if err != nil {
		return nil, err
	}
	return &naiveInbound{created.(*naive.Inbound)}, nil
}

// Embed the concrete implementation to retain its lifecycle and HTTP methods.
type naiveInbound struct{ *naive.Inbound }

var (
	_ adapter.Inbound = (*naiveInbound)(nil)
	_ http.Handler    = (*naiveInbound)(nil)
)

func (n *naiveInbound) Close() error {
	// Upstream closes both the TCP listener and the HTTP server that owns it.
	// Normalize only that idempotent result, across remove, replacement and Stop.
	return normalizeNaiveCloseError(n.Inbound.Close())
}

func normalizeNaiveCloseError(err error) error {
	if onlyClosedNetworkErrors(err) {
		return nil
	}
	return err
}

func onlyClosedNetworkErrors(err error) bool {
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		found := false
		for _, cause := range wrapped.Unwrap() {
			if cause == nil {
				continue
			}
			found = true
			if !onlyClosedNetworkErrors(cause) {
				return false
			}
		}
		return found
	case interface{ Unwrap() error }:
		return onlyClosedNetworkErrors(wrapped.Unwrap())
	default:
		// errors.Is on the whole tree would also match mixed close failures.
		return err == net.ErrClosed
	}
}
