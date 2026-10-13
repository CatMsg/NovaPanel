package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"

	"github.com/sagernet/sing-box/option"
	native "github.com/sagernet/sing-box/protocol/masque"
	transportHTTP "github.com/sagernet/sing-box/transport/http"
	transport "github.com/sagernet/sing-box/transport/masque"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/logger"
)

type MihomoMASQUEUser struct {
	Username  string     `json:"username"`
	PublicKey string     `json:"public_key"`
	Address   netip.Addr `json:"address"`
}

type managedMASQUEServerOptions struct {
	option.MASQUEServerEndpointOptions
	MihomoUsers []MihomoMASQUEUser `json:"mihomo_users,omitempty"`
}

func (o *managedMASQUEServerOptions) UnmarshalJSONContext(ctx context.Context, content []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return err
	}
	if raw := fields["mihomo_users"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &o.MihomoUsers); err != nil {
			return err
		}
	}
	delete(fields, "mihomo_users")
	content, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return o.MASQUEServerEndpointOptions.UnmarshalJSONContext(ctx, content)
}

func (o managedMASQUEServerOptions) MarshalJSON() ([]byte, error) {
	content, err := o.MASQUEServerEndpointOptions.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return nil, err
	}
	if len(o.MihomoUsers) > 0 {
		fields["mihomo_users"], err = json.Marshal(o.MihomoUsers)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

type mihomoCertificateSession struct{}

type masqueCompatibility struct {
	keys          map[string]MihomoMASQUEUser
	addresses     map[netip.Addr]struct{}
	authenticator *auth.Authenticator
}

func newMASQUECompatibility(options managedMASQUEServerOptions) (*masqueCompatibility, error) {
	if len(options.MihomoUsers) == 0 {
		return nil, nil
	}
	if options.TLS == nil || options.TLS.ClientAuthentication != 0 {
		return nil, errors.New("compatibility with Mihomo requires TLS without a conflicting client-auth policy")
	}
	result := &masqueCompatibility{keys: make(map[string]MihomoMASQUEUser), addresses: make(map[netip.Addr]struct{}), authenticator: auth.NewAuthenticator(options.Users)}
	users := make(map[string]bool)
	for _, user := range options.Users {
		users[user.Username] = true
	}
	for _, user := range options.MihomoUsers {
		der, err := base64.StdEncoding.DecodeString(user.PublicKey)
		if err != nil {
			return nil, errors.New("invalid Mihomo client public key")
		}
		key, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, errors.New("invalid Mihomo client public key")
		}
		if _, ok := key.(*ecdsa.PublicKey); !ok {
			return nil, errors.New("compatibility with Mihomo requires ECDSA client keys")
		}
		validAddress := false
		for _, pool := range options.Address {
			if pool.Addr().Is4() && user.Address.Is4() && pool.Contains(user.Address) && pool.Contains(user.Address.Next()) && pool.Addr() != user.Address && pool.Masked().Addr() != user.Address {
				validAddress = true
			}
		}
		if !users[user.Username] || !validAddress {
			return nil, errors.New("client identity/address for Mihomo does not belong to an enabled native user")
		}
		if _, duplicate := result.keys[user.PublicKey]; duplicate {
			return nil, errors.New("duplicate Mihomo public key")
		}
		if _, duplicate := result.addresses[user.Address]; duplicate {
			return nil, errors.New("duplicate Mihomo client address")
		}
		result.keys[user.PublicKey] = user
		result.addresses[user.Address] = struct{}{}
	}
	return result, nil
}

func (c *masqueCompatibility) CustomizeMASQUEHTTP(options *transportHTTP.ServerOptions) {
	handler := &masqueCompatibleTunnel{compatibility: c, next: options.Tunnels["connect-ip"], logger: options.Logger}
	options.Authenticator = nil
	options.Tunnels = map[string]transportHTTP.TunnelHandler{"connect-ip": handler, "cf-connect-ip": handler}
}

type masqueCompatibleTunnel struct {
	compatibility *masqueCompatibility
	next          transportHTTP.TunnelHandler
	logger        logger.ContextLogger
}

func (h *masqueCompatibleTunnel) NewTunnelRequest(ctx context.Context, request transportHTTP.TunnelRequest) {
	r := request.Request()
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		der, err := x509.MarshalPKIXPublicKey(r.TLS.PeerCertificates[0].PublicKey)
		if err == nil {
			if user, valid := h.compatibility.keys[base64.StdEncoding.EncodeToString(der)]; valid {
				ctx = context.WithValue(auth.ContextWithUser(ctx, user.Username), mihomoCertificateSession{}, user.Address)
				h.next.NewTunnelRequest(ctx, request)
				return
			}
		}
	} else if username, password, ok := r.BasicAuth(); ok && h.compatibility.authenticator.Verify(username, password) {
		h.next.NewTunnelRequest(auth.ContextWithUser(ctx, username), request)
		return
	}
	h.logger.WarnContext(ctx, "MASQUE compatibility authentication rejected")
	request.Reject(http.StatusUnauthorized, nil)
}

func (r *masqueRouter) SessionAddress(ctx context.Context, _ string) netip.Addr {
	address, _ := ctx.Value(mihomoCertificateSession{}).(netip.Addr)
	return address
}

func (r *masqueRouter) ReservedSessionAddress(address netip.Addr) bool {
	if r.compatibility == nil {
		return false
	}
	_, reserved := r.compatibility.addresses[address]
	return reserved
}

func (r *masqueRouter) SessionRoutes(ctx context.Context, _ string, routes []transport.AddressRange) ([]transport.AddressRange, error) {
	if !r.SessionAddress(ctx, "").IsValid() {
		return routes, nil
	}
	// Mihomo announces default routes unconditionally. They are not permission
	// to route another client's source: discard only these exact announcements.
	for _, route := range routes {
		if route.Protocol != 0 || !route.Start.IsUnspecified() || route.Start.BitLen() != route.End.BitLen() {
			return nil, errors.New("client route advertisement for Mihomo is not a default route")
		}
		for _, value := range route.End.AsSlice() {
			if value != 255 {
				return nil, errors.New("client route advertisement for Mihomo is not a default route")
			}
		}
	}
	return nil, nil
}

var _ native.ServerHTTPOptionsCustomizer = (*masqueCompatibility)(nil)
