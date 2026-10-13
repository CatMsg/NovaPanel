package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/sagernet/sing-box/option"
	transportHTTP "github.com/sagernet/sing-box/transport/http"
	transport "github.com/sagernet/sing-box/transport/masque"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

type masqueAuthTestRequest struct {
	request  *http.Request
	rejected int
}

func (r *masqueAuthTestRequest) Request() *http.Request              { return r.request }
func (r *masqueAuthTestRequest) Source() M.Socksaddr                 { return M.ParseSocksaddr("203.0.113.99:1") }
func (r *masqueAuthTestRequest) Accept() (io.ReadWriteCloser, error) { panic("not used") }
func (r *masqueAuthTestRequest) Reject(status int, _ http.Header)    { r.rejected = status }

type masqueAuthTestHandler struct{ ctx context.Context }

func (h *masqueAuthTestHandler) NewTunnelRequest(ctx context.Context, _ transportHTTP.TunnelRequest) {
	h.ctx = ctx
}

func TestManagedMASQUEMihomoAuthenticationAndRoutes(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	address := netip.MustParseAddr("172.29.0.2")
	options := managedMASQUEServerOptions{MihomoUsers: []MihomoMASQUEUser{{Username: "alice", PublicKey: base64.StdEncoding.EncodeToString(der), Address: address}}}
	options.Users = []auth.User{{Username: "alice", Password: "test-only"}}
	options.Address = []netip.Prefix{netip.MustParsePrefix("172.29.0.1/29")}
	options.TLS = &option.InboundTLSOptions{Enabled: true}
	compatible, err := newMASQUECompatibility(options)
	if err != nil {
		t.Fatal(err)
	}
	router := &masqueRouter{compatibility: compatible}
	for _, tc := range []struct {
		name        string
		certificate *x509.Certificate
		password    string
		accepted    bool
		mihomo      bool
	}{
		{"native-basic", nil, "test-only", true, false},
		{"key", &x509.Certificate{PublicKey: &key.PublicKey}, "", true, true},
		{"missing", nil, "", false, false},
		{"bad-basic", nil, "wrong", false, false},
		{"unknown-certificate-with-valid-basic", &x509.Certificate{}, "test-only", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodConnect, "https://localhost/", nil)
			if tc.password != "" {
				r.SetBasicAuth("alice", tc.password)
			}
			if tc.certificate != nil {
				r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{tc.certificate}}
			}
			request := &masqueAuthTestRequest{request: r}
			next := &masqueAuthTestHandler{}
			handler := &masqueCompatibleTunnel{compatibility: compatible, next: next, logger: logger.NOP()}
			handler.NewTunnelRequest(context.Background(), request)
			if (next.ctx != nil) != tc.accepted {
				t.Fatal("authentication result mismatch")
			}
			if !tc.accepted {
				if request.rejected != http.StatusUnauthorized {
					t.Fatal("unauthorized request not rejected")
				}
				return
			}
			user, ok := auth.UserFromContext[string](next.ctx)
			if !ok || user != "alice" || router.SessionAddress(next.ctx, user).IsValid() != tc.mihomo {
				t.Fatal("trusted identity or session address policy mismatch")
			}
			if tc.mihomo {
				if router.SessionAddress(next.ctx, user) != address || !router.ShareSessionAddress(next.ctx, user, address) {
					t.Fatal("Mihomo user lost its shared session address")
				}
				defaults := []transport.AddressRange{{Start: netip.IPv4Unspecified(), End: netip.MustParseAddr("255.255.255.255")}, {Start: netip.IPv6Unspecified(), End: netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")}}
				if routes, err := router.SessionRoutes(next.ctx, user, defaults); err != nil || len(routes) != 0 {
					t.Fatal("default routes were installed or rejected instead of discarded")
				}
				for _, route := range []transport.AddressRange{{Start: netip.MustParseAddr("172.29.0.3"), End: netip.MustParseAddr("172.29.0.3")}, {Start: netip.IPv4Unspecified(), End: netip.MustParseAddr("255.255.255.255"), Protocol: 17}} {
					if _, err := router.SessionRoutes(next.ctx, user, []transport.AddressRange{route}); err == nil {
						t.Fatal("non-default route advertisement accepted")
					}
				}
			} else if router.SessionAddress(next.ctx, user).IsValid() {
				t.Fatal("native username/password identity was marked as Mihomo")
			}
		})
	}
	if !router.ReservedSessionAddress(address) || router.ReservedSessionAddress(netip.MustParseAddr("172.29.0.3")) {
		t.Fatal("Mihomo address reservation mismatch")
	}
	legacy := []byte(`{"address":["172.29.0.1/29"],"mihomo_users":[{"username":"alice","public_key":"` + base64.StdEncoding.EncodeToString(der) + `","address":"172.29.0.2"}]}`)
	var legacyOptions managedMASQUEServerOptions
	if err := legacyOptions.UnmarshalJSONContext(context.Background(), legacy); err != nil {
		t.Fatalf("legacy stored fixed address was not loaded: %v", err)
	}
	legacyOptions.Users, legacyOptions.TLS = options.Users, options.TLS
	legacyCompatibility, err := newMASQUECompatibility(legacyOptions)
	if err != nil || len(legacyCompatibility.keys) != 1 {
		t.Fatalf("legacy stored fixed address prevented compatibility: %v", err)
	}
}
