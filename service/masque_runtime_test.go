package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	mtls "github.com/metacubex/tls"
)

func TestMasqueRuntimeRoutesFlowToOriginatingSession(t *testing.T) {
	runtime := &masqueRuntime{
		sessions: map[uint64]*masqueSession{}, userSessions: map[string]map[uint64]*masqueSession{},
		flows:     map[masqueFlowKey]masqueFlowEntry{},
		usersByIP: map[netip.Addr]string{}, traffic: map[string]*masqueUserTraffic{"alice": {}},
	}
	prefix := netip.MustParsePrefix("172.16.1.2/32")
	runtime.usersByIP[prefix.Addr()] = "alice"
	newSession := func() (*masqueSession, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		return &masqueSession{identity: masqueClientIdentity{Name: "alice", Prefix: prefix}, ctx: ctx, cancel: cancel, outgoing: make(chan []byte, 1)}, cancel
	}
	first, cancelFirst := newSession()
	defer cancelFirst()
	second, cancelSecond := newSession()
	defer cancelSecond()
	runtime.addSession(first)
	runtime.addSession(second)

	request := testIPv4UDPPacket([4]byte{172, 16, 1, 2}, [4]byte{1, 1, 1, 1}, 41000, 53)
	runtime.recordClientFlow(first, request)
	response := testIPv4UDPPacket([4]byte{1, 1, 1, 1}, [4]byte{172, 16, 1, 2}, 53, 41000)
	if !runtime.routeTunPacket(response) {
		t.Fatal("expected response to be routed")
	}
	select {
	case <-first.outgoing:
	default:
		t.Fatal("originating session did not receive response")
	}
	select {
	case <-second.outgoing:
		t.Fatal("latest session incorrectly stole an existing flow")
	default:
	}
}

func TestMasqueRuntimeDoesNotGuessAmongConcurrentSessions(t *testing.T) {
	runtime := &masqueRuntime{
		sessions: map[uint64]*masqueSession{}, userSessions: map[string]map[uint64]*masqueSession{},
		flows:     map[masqueFlowKey]masqueFlowEntry{},
		usersByIP: map[netip.Addr]string{}, traffic: map[string]*masqueUserTraffic{"alice": {}},
	}
	prefix := netip.MustParsePrefix("172.16.1.2/32")
	runtime.usersByIP[prefix.Addr()] = "alice"
	newSession := func() *masqueSession {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		return &masqueSession{identity: masqueClientIdentity{Name: "alice", Prefix: prefix}, startedAt: time.Now(), ctx: ctx, cancel: cancel, outgoing: make(chan []byte, 1)}
	}
	first := newSession()
	second := newSession()
	runtime.addSession(first)
	runtime.addSession(second)

	unknown := testIPv4UDPPacket([4]byte{1, 1, 1, 1}, [4]byte{172, 16, 1, 2}, 53, 42000)
	if runtime.routeTunPacket(unknown) {
		t.Fatal("ambiguous packet was routed to an arbitrary session")
	}
}

func TestMasqueRuntimeReapsIdleAndOldSessions(t *testing.T) {
	runtime := &masqueRuntime{
		tag: "masque-main", sessions: map[uint64]*masqueSession{},
		userSessions: map[string]map[uint64]*masqueSession{},
		flows:        map[masqueFlowKey]masqueFlowEntry{},
	}
	now := time.Now()
	newSession := func(startedAt, activeAt time.Time) *masqueSession {
		ctx, cancel := context.WithCancel(context.Background())
		session := &masqueSession{identity: masqueClientIdentity{Name: "alice"}, startedAt: startedAt, ctx: ctx, cancel: cancel, outgoing: make(chan []byte, 1)}
		session.touch(activeAt)
		return session
	}
	idle := newSession(now.Add(-2*time.Minute), now.Add(-masqueSessionIdleTimeout-time.Second))
	old := newSession(now.Add(-masqueSessionMaxLifetime-time.Second), now.Add(-masqueAgedSessionQuiet-time.Second))
	active := newSession(now.Add(-time.Minute), now)
	longActive := newSession(now.Add(-masqueSessionMaxLifetime-time.Minute), now)
	runtime.addSession(idle)
	idle.touch(now.Add(-masqueSessionIdleTimeout - time.Second))
	runtime.addSession(old)
	old.touch(now.Add(-masqueAgedSessionQuiet - time.Second))
	runtime.addSession(active)
	runtime.addSession(longActive)
	longActive.touch(now)

	if got := runtime.closeExpiredSessions(now); got != 2 {
		t.Fatalf("unexpected expired session count: %d", got)
	}
	select {
	case <-idle.ctx.Done():
	default:
		t.Fatal("idle session was not closed")
	}
	select {
	case <-old.ctx.Done():
	default:
		t.Fatal("old session was not closed")
	}
	select {
	case <-active.ctx.Done():
		t.Fatal("active session was closed")
	default:
	}
	select {
	case <-longActive.ctx.Done():
		t.Fatal("long-running active session was closed")
	default:
	}
}

func testIPv4UDPPacket(src, dst [4]byte, srcPort, dstPort uint16) []byte {
	packet := make([]byte, 28)
	packet[0] = 0x45
	packet[9] = 17
	copy(packet[12:16], src[:])
	copy(packet[16:20], dst[:])
	packet[20], packet[21] = byte(srcPort>>8), byte(srcPort)
	packet[22], packet[23] = byte(dstPort>>8), byte(dstPort)
	return packet
}

func TestMasqueRuntimeStatusSnapshot(t *testing.T) {
	runtime := &masqueRuntime{
		sessions: map[uint64]*masqueSession{}, userSessions: map[string]map[uint64]*masqueSession{},
		flows:   map[masqueFlowKey]masqueFlowEntry{},
		clients: map[string]masqueClientIdentity{"key": {Name: "alice"}},
	}
	runtime.running.Store(true)
	runtime.rxBytes.Store(4096)
	runtime.txBytes.Store(8192)
	runtime.rxPackets.Store(4)
	runtime.txPackets.Store(8)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &masqueSession{
		identity:  masqueClientIdentity{Name: "alice"},
		startedAt: time.Now().Add(-5 * time.Second),
		remote:    "203.0.113.10:44321",
		ctx:       ctx,
		cancel:    cancel,
		outgoing:  make(chan []byte, 1),
	}
	session.rxBytes.Store(1024)
	session.txBytes.Store(2048)
	runtime.addSession(session)

	status := runtime.statusSnapshot()
	if status["session_active"] != true || status["active_sessions"] != 1 || status["active_users"] != 1 {
		t.Fatalf("unexpected session status: %#v", status)
	}
	if status["rx_bytes"] != uint64(4096) {
		t.Fatalf("unexpected traffic counters: %#v", status)
	}
}

func TestMasqueCertificateReloaderReloadsChangedFiles(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "fullchain.cer")
	keyFile := filepath.Join(dir, "server.key")
	writeTestMasqueCertificate(t, certFile, keyFile, time.Now().Add(24*time.Hour), 1)

	initial, err := mtls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load initial certificate: %v", err)
	}
	reloader := newMasqueCertificateReloader(initial, certFile, keyFile, "file")
	before := reloader.snapshot()

	writeTestMasqueCertificate(t, certFile, keyFile, time.Now().Add(48*time.Hour), 2)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(certFile, future, future); err != nil {
		t.Fatalf("update certificate mtime: %v", err)
	}
	if err := os.Chtimes(keyFile, future, future); err != nil {
		t.Fatalf("update key mtime: %v", err)
	}
	reloader.reloadIfChanged(true)
	after := reloader.snapshot()

	if after.ReloadCount != 1 {
		t.Fatalf("expected one reload, got %#v", after)
	}
	if after.Fingerprint == "" || after.Fingerprint == before.Fingerprint {
		t.Fatalf("certificate fingerprint was not updated: before=%s after=%s", before.Fingerprint, after.Fingerprint)
	}
	if after.LastError != "" {
		t.Fatalf("unexpected reload error: %s", after.LastError)
	}
}

func TestMasqueDiagnosticsRejectInvalidCertificateWindow(t *testing.T) {
	config := &masqueInboundConfig{Network: "quic"}
	tests := []struct {
		name string
		cert masqueCertificateSnapshot
		want string
	}{
		{
			name: "not yet valid",
			cert: masqueCertificateSnapshot{NotBefore: time.Now().Add(time.Hour), NotAfter: time.Now().Add(25 * time.Hour)},
			want: "证书尚未生效",
		},
		{
			name: "expired",
			cert: masqueCertificateSnapshot{NotBefore: time.Now().Add(-25 * time.Hour), NotAfter: time.Now().Add(-time.Hour)},
			want: "证书已过期",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := buildMasqueDiagnostics(config, nil, tt.cert, nil, "")
			for _, check := range checks {
				if check.ID == "certificate" {
					if check.Status != "error" || check.Detail != tt.want {
						t.Fatalf("unexpected certificate diagnostic: %#v", check)
					}
					return
				}
			}
			t.Fatal("certificate diagnostic is missing")
		})
	}
}

func writeTestMasqueCertificate(t *testing.T, certFile, keyFile string, notAfter time.Time, serial int64) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"masque.example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
}
