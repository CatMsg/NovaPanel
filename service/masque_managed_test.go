package service

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	transportHTTP "github.com/sagernet/sing-box/transport/http"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func masqueManagedConfigFixture(t *testing.T) (*gorm.DB, model.Inbound) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "masque-config.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&model.Tls{}, &model.Inbound{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}
	tls := masqueManagedTestTLS(t)
	if err := db.Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	return db, model.Inbound{Type: "masque", Tag: "native-config", TlsId: tls.Id, Tls: &tls, Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`), Options: json.RawMessage(`{"listen_port":443}`)}
}

func TestMASQUEManagedPrepareDefaultsAndRejectLegacy(t *testing.T) {
	db, inbound := masqueManagedConfigFixture(t)
	if err := prepareMasqueInbound(db, &inbound, nil, "localhost"); err != nil {
		t.Fatal(err)
	}
	var fields struct {
		Server  string   `json:"server"`
		Listen  string   `json:"listen"`
		Version []int    `json:"version"`
		System  *bool    `json:"system"`
		Address []string `json:"address"`
		MTU     int      `json:"mtu"`
	}
	if err := json.Unmarshal(inbound.Options, &fields); err != nil {
		t.Fatal(err)
	}
	if fields.Server != "localhost" || fields.Listen != "::" || len(fields.Version) != 1 || fields.Version[0] != 3 || fields.System == nil || *fields.System || fields.MTU != 1280 || len(fields.Address) != 1 || fields.Address[0] != "172.16.0.1/24" {
		t.Fatal("native server defaults changed")
	}
	if inbound.TlsId != 0 || inbound.Tls != nil || inbound.MasqueTLSCertificate == "" || inbound.MasqueTLSKey == "" {
		t.Fatal("MASQUE did not generate its own TLS identity or still depends on a template")
	}
	if _, err := validateMasqueTLSMaterial(&inbound, "localhost"); err != nil {
		t.Fatalf("generated MASQUE identity is invalid: %v", err)
	}
	stableKey, stableCertificate := inbound.MasqueTLSKey, inbound.MasqueTLSCertificate
	updated := inbound
	updated.Options = json.RawMessage(`{"listen_port":443}`)
	if err := prepareMasqueInbound(db, &updated, &inbound, "localhost"); err != nil {
		t.Fatal(err)
	}
	if updated.MasqueTLSKey != stableKey || updated.MasqueTLSCertificate != stableCertificate {
		t.Fatal("ordinary MASQUE edit rotated the inbound ECDSA identity")
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	second := inbound
	second.Id, second.Tag, second.Options = 0, "native-second", json.RawMessage(`{"listen_port":444}`)
	second.MasqueTLSCertificate, second.MasqueTLSKey = "", ""
	if err := prepareMasqueInbound(db, &second, nil, "localhost"); err != nil {
		t.Fatal(err)
	}
	if second.MasqueTLSKey == stableKey || second.MasqueTLSCertificate == stableCertificate {
		t.Fatal("new MASQUE inbounds shared a TLS identity")
	}
	firstConfig, _ := parseMasqueInbound(&inbound)
	secondConfig, err := parseMasqueInbound(&second)
	if err != nil || firstConfig.Address.Overlaps(secondConfig.Address) {
		t.Fatal("automatically assigned native pools overlap")
	}
	for _, key := range []string{"private_key", "public_key", "client_subnet", "keepalive", "remote_dns_resolve", "network", "udp", "sni", "system"} {
		t.Run(key, func(t *testing.T) {
			copy := inbound
			copy.Options, _ = json.Marshal(map[string]any{"listen_port": 443, key: true})
			if err := prepareMasqueInbound(db, &copy, nil, "localhost"); err == nil {
				t.Fatal("legacy option or system:true accepted")
			}
		})
	}
	for _, mode := range []string{"missing-id", "missing-template", "disabled-template"} {
		t.Run(mode, func(t *testing.T) {
			copy := inbound
			switch mode {
			case "missing-id":
				copy.TlsId = 0
			case "missing-template":
				copy.Tls = nil
			case "disabled-template":
				copy.Tls = &model.Tls{Server: json.RawMessage(`{"enabled":false}`)}
			}
			if err := prepareMasqueInbound(db, &copy, nil, "localhost"); err != nil {
				t.Fatalf("MASQUE should not require a shared TLS template: %v", err)
			}
			if copy.MasqueTLSKey == "" || copy.MasqueTLSCertificate == "" || copy.TlsId != 0 {
				t.Fatal("MASQUE did not replace the shared TLS dependency with its own identity")
			}
		})
	}
}

func TestMASQUETLSMigrationRefreshesSubscriptionAndIsIdempotent(t *testing.T) {
	db, template := masqueManagedConfigFixture(t)
	legacy := model.Inbound{
		Type: "masque", Tag: "legacy-masque", TlsId: template.TlsId, Tls: template.Tls,
		Options: json.RawMessage(`{"server":"legacy.example","listen_port":443,"version":[3],"system":false,"mtu":1280,"address":["172.20.0.1/24"],"mihomo_compatible":true}`),
		OutJson: json.RawMessage(`{"type":"masque-client","tls":{"enabled":true}}`),
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateMasqueTLSInbounds(db); err != nil {
		t.Fatal(err)
	}
	var migrated model.Inbound
	if err := db.First(&migrated, legacy.Id).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.TlsId != 0 || migrated.MasqueTLSCertificate == "" || migrated.MasqueTLSKey == "" {
		t.Fatal("legacy MASQUE TLS identity was not migrated away from the shared template")
	}
	if _, err := validateMasqueTLSMaterial(&migrated, "legacy.example"); err != nil {
		t.Fatalf("migrated TLS identity is invalid: %v", err)
	}
	var outbound map[string]any
	if err := json.Unmarshal(migrated.OutJson, &outbound); err != nil {
		t.Fatal(err)
	}
	tls, ok := outbound["tls"].(map[string]any)
	certificates, certOK := tls["certificate"].([]any)
	if !ok || !certOK || len(certificates) != 1 || certificates[0] != migrated.MasqueTLSCertificate {
		t.Fatal("sing-box subscription does not trust the exact MASQUE server certificate")
	}
	if _, exists := tls["key"]; exists {
		t.Fatal("server private key leaked into the client subscription")
	}
	novaMihomo, ok := outbound["nova_mihomo"].(map[string]any)
	if !ok || novaMihomo["public-key"] == "" {
		t.Fatal("Mihomo pin was not generated from the dedicated certificate")
	}
	for _, value := range []any{migrated, func() any { raw, _ := migrated.MarshalFull(); return raw }()} {
		encoded, err := json.Marshal(value)
		if err != nil || bytes.Contains(encoded, []byte(migrated.MasqueTLSKey)) || bytes.Contains(encoded, []byte("masque_tls_key")) {
			t.Fatal("MASQUE private key escaped through an API JSON representation")
		}
	}
	keyBefore, certBefore, outBefore := migrated.MasqueTLSKey, migrated.MasqueTLSCertificate, bytes.Clone(migrated.OutJson)
	if err := MigrateMasqueTLSInbounds(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&migrated, legacy.Id).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.MasqueTLSKey != keyBefore || migrated.MasqueTLSCertificate != certBefore || !bytes.Equal(migrated.OutJson, outBefore) {
		t.Fatal("repeated startup migration rotated the identity or rewrote subscriptions")
	}
}

func TestMASQUEManagedNormalizeCredentialsAcrossSaves(t *testing.T) {
	db, inbound := masqueManagedConfigFixture(t)
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Enable: true, Name: "alice", Config: json.RawMessage(`{"socks":{"username":"other-user","password":"test-only"},"vmess":{"uuid":"other-protocol-id"}}`), Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)), Links: json.RawMessage(`[]`), History: json.RawMessage(`[]`)}
	_, original, err := decodeMasqueCredential(client.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := normalizeMasqueClientConfigs(db, []*model.Client{&client}); err != nil {
		t.Fatal(err)
	}
	credential, _, err := decodeMasqueCredential(client.Config)
	if err != nil || credential.Username != client.Name || len(credential.Password) < 40 {
		t.Fatal("native credentials were not initialized")
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	stableConfig := append([]byte(nil), client.Config...)
	for round := 0; round < 3; round++ {
		if err := db.First(&client, client.Id).Error; err != nil {
			t.Fatal(err)
		}
		if err := normalizeMasqueClientConfigs(db, []*model.Client{&client}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stableConfig, client.Config) {
			t.Fatal("repeated normalization rotated native credentials")
		}
		if err := db.Save(&client).Error; err != nil {
			t.Fatal(err)
		}
	}
	client.Name = "renamed-alice"
	if err := normalizeMasqueClientConfigs(db, []*model.Client{&client}); err != nil {
		t.Fatal(err)
	}
	renamed, root, err := decodeMasqueCredential(client.Config)
	if err != nil || renamed.Username != client.Name || renamed.Password != credential.Password {
		t.Fatal("renaming did not update username while preserving the password")
	}
	for _, key := range []string{"socks", "vmess"} {
		var before, after bytes.Buffer
		if json.Compact(&before, original[key]) != nil || json.Compact(&after, root[key]) != nil || !bytes.Equal(before.Bytes(), after.Bytes()) {
			t.Fatal("native normalization changed another protocol credential")
		}
	}
	unbound := client
	unbound.Inbounds = json.RawMessage(`[]`)
	unbound.Config = json.RawMessage(`{"socks":{"username":"untouched"}}`)
	before := append([]byte(nil), unbound.Config...)
	if err := normalizeMasqueClientConfigs(db, []*model.Client{&unbound}); err != nil || !bytes.Equal(unbound.Config, before) {
		t.Fatal("unbound client was changed")
	}
}

func TestMASQUEManagedEndpointFiltersUsersFailClosed(t *testing.T) {
	db, inbound := masqueManagedConfigFixture(t)
	if err := prepareMasqueInbound(db, &inbound, nil, "localhost"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"active", "disabled", "expired", "exhausted", "unbound", "other-inbound"} {
		client := model.Client{Enable: true, Name: name, Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)), Config: json.RawMessage(`{}`), Links: json.RawMessage(`[]`), History: json.RawMessage(`[]`)}
		switch name {
		case "disabled":
			client.Enable = false
		case "expired":
			client.Expiry = time.Now().Add(-time.Hour).Unix()
		case "exhausted":
			client.Volume, client.Up, client.Down = 100, 40, 60
		case "unbound":
			client.Inbounds = json.RawMessage(`[]`)
		case "other-inbound":
			client.Inbounds = json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id+1))
		}
		if err := normalizeMasqueClientConfigs(db, []*model.Client{&client}); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&client).Error; err != nil {
			t.Fatal(err)
		}
	}
	raw, err := buildMasqueEndpointConfig(db, &inbound)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Type  string                   `json:"type"`
		Users []masqueClientCredential `json:"users"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.Type != "masque-server" || len(config.Users) != 1 || config.Users[0].Username != "active" || config.Users[0].Password == "" {
		t.Fatal("endpoint included an ineligible or unbound user")
	}
	if err := db.Model(&model.Client{}).Where("name = ?", "active").Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if raw, err := buildMasqueEndpointConfig(db, &inbound); err != nil || len(raw) != 0 {
		t.Fatal("removing the last eligible user emitted an open endpoint")
	}
	invalid := model.Client{Enable: true, Name: "invalid-credentials", Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)), Config: json.RawMessage(`{}`)}
	if err := db.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}
	if raw, err := buildMasqueEndpointConfig(db, &inbound); err == nil || len(raw) != 0 {
		t.Fatal("invalid bound credentials emitted a server config")
	}
}

func TestMASQUEManagedClientTemplateNativeFields(t *testing.T) {
	db, inbound := masqueManagedConfigFixture(t)
	inbound.Options = json.RawMessage(`{"listen_port":443,"path":"/.well-known/masque/ip/{target}/{ipproto}/"}`)
	if err := prepareMasqueInbound(db, &inbound, nil, "localhost"); err != nil {
		t.Fatal(err)
	}
	raw, err := buildMasqueClientTemplate(&inbound)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config["type"] != "masque-client" || config["tag"] != inbound.Tag || config["server"] != "localhost" || config["server_port"] != float64(443) || config["version"] != float64(3) || config["system"] != false || config["disable_version_fallback"] != true || config["on_demand"] != true || config["mtu"] != float64(1280) || config["path"] != "/.well-known/masque/ip/{target}/{ipproto}/" {
		t.Fatal("client template does not contain native HTTP/3 fields")
	}
	tls, ok := config["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["server_name"] != "localhost" {
		t.Fatal("client template lost TLS settings")
	}
	certificates, ok := tls["certificate"].([]any)
	if !ok || len(certificates) != 1 || certificates[0] != inbound.MasqueTLSCertificate {
		t.Fatal("sing-box client template does not pin the inbound-owned certificate")
	}
	alpn, ok := tls["alpn"].([]any)
	if !ok || len(alpn) != 1 || alpn[0] != "h3" {
		t.Fatal("client template is not restricted to HTTP/3 ALPN")
	}
	for _, key := range []string{"private_key", "public_key", "client_subnet", "keepalive", "remote_dns_resolve", "network", "udp", "sni", "address", "listen", "listen_port", "users", "username", "password"} {
		if _, present := config[key]; present {
			t.Fatalf("client template leaked server, legacy or per-user field %s", key)
		}
	}
	if _, present := tls["key"]; present {
		t.Fatal("server TLS private key leaked into client template")
	}
	if _, present := tls["key_path"]; present {
		t.Fatal("server TLS private key path leaked into client template")
	}
	var pool struct {
		Address []netip.Prefix `json:"address"`
	}
	if err := json.Unmarshal(inbound.Options, &pool); err != nil || len(pool.Address) != 1 {
		t.Fatal("client-template generation changed server options")
	}
}

func TestMASQUEManagedHTTP3GeneratedConfigAndUserRevocation(t *testing.T) {
	if transportHTTP.ConfigureHTTP3ListenerFunc == nil {
		t.Skip("requires with_quic")
	}
	db, _ := setupInboundRuntimeTest(t)
	tls := masqueManagedTestTLS(t)
	if err := db.Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{Type: "masque", Tag: "managed-native", TlsId: tls.Id, Tls: &tls, Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`), Options: json.RawMessage(fmt.Sprintf(`{"server":"127.0.0.1","listen":"127.0.0.1","listen_port":%d}`, masqueNativeFreePort(t)))}
	if err := prepareMasqueInbound(db, &inbound, nil, "localhost"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Enable: true, Name: "alice", Config: json.RawMessage(`{"socks":{"username":"keep","password":"unchanged"}}`), Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)), Links: json.RawMessage(`[]`), Up: 12, Down: 34, TotalUp: 56, TotalDown: 78, History: json.RawMessage(`[]`)}
	if err := normalizeMasqueClientConfigs(db, []*model.Client{&client}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	credential, root, err := decodeMasqueCredential(client.Config)
	if err != nil || credential.Username != "alice" || len(credential.Password) < 40 || !bytes.Contains(root["socks"], []byte("unchanged")) {
		t.Fatal("native normalization lost unrelated credentials or did not create a secure password")
	}
	beforeConfig := append([]byte(nil), client.Config...)
	if err := normalizeMasqueClientConfigs(db, []*model.Client{&client}); err != nil || !bytes.Equal(beforeConfig, client.Config) {
		t.Fatal("native password changed on normalization")
	}
	service := NewMasqueService()
	if err := service.SyncFromDB(); err != nil {
		t.Fatal(err)
	}
	endpoint, exists := corePtr.GetInstance().Endpoint().Get(inbound.Tag)
	if !exists || endpoint.Type() != "masque-server" {
		t.Fatal("managed native server was not created")
	}
	if err := service.SyncFromDB(); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := corePtr.GetInstance().Endpoint().Get(inbound.Tag)
	if endpoint != unchanged {
		t.Fatal("unchanged native configuration restarted its listener")
	}
	template, err := buildMasqueClientTemplate(&inbound)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(template, &config); err != nil {
		t.Fatal(err)
	}
	config["tag"] = "native-client"
	config["username"], config["password"] = credential.Username, credential.Password
	// The generated server address is assigned automatically by the panel.
	var fields struct {
		Address []string `json:"address"`
	}
	if err := json.Unmarshal(inbound.Options, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields.Address) != 1 || fields.Address[0] != "172.16.0.1/24" {
		t.Fatalf("unexpected generated pool: %v", fields.Address)
	}
	// The shared round-trip helper uses 172.29.0.1, so explicitly use that
	// server prefix here before reconnecting the generated native endpoint.
	var options map[string]any
	json.Unmarshal(inbound.Options, &options)
	options["address"] = []string{"172.29.0.1/29"}
	inbound.Options, _ = json.Marshal(options)
	if err := db.Save(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.SyncFromDB(); err != nil {
		t.Fatal(err)
	}
	clientBox := masqueNativeBox(t, []map[string]any{config}, nil)
	proxy, _ := clientBox.Endpoint().Get("native-client")
	masqueNativeRoundTrip(t, proxy, "tcp")
	masqueNativeRoundTrip(t, proxy, "udp")
	stats := corePtr.GetInstance().StatsTracker().GetStats()
	var upload, download int64
	for _, stat := range *stats {
		if stat.Resource == "user" && stat.Tag == client.Name {
			if stat.Direction {
				upload += stat.Traffic
			} else {
				download += stat.Traffic
			}
		}
	}
	if upload < 40 || download < 40 {
		t.Fatalf("native TCP/UDP traffic bypassed user counters: up=%d down=%d", upload, download)
	}
	var stored model.Client
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(stored.History, &history); err != nil {
		t.Fatal(err)
	}
	networks := map[string]bool{}
	for _, entry := range history {
		if entry.Inbound != inbound.Tag || entry.SourceIP != "127.0.0.1" {
			t.Fatalf("untrusted native history identity/source: %+v", entry)
		}
		networks[entry.Network] = true
	}
	if !networks["tcp"] || !networks["udp"] {
		t.Fatalf("missing native TCP/UDP history: %+v", history)
	}
	if stored.Up != 12 || stored.Down != 34 || stored.TotalUp != 56 || stored.TotalDown != 78 || !bytes.Equal(stored.Config, beforeConfig) {
		t.Fatal("native reconciliation mutated persisted counters or credentials")
	}
	// A fresh reconciler must revoke a cold-started endpoint when the last
	// user becomes ineligible, rather than reusing an unauthenticated listener.
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewMasqueService().SyncFromDB(); err != nil {
		t.Fatal(err)
	}
	if _, exists := corePtr.GetInstance().Endpoint().Get(inbound.Tag); exists {
		t.Fatal("last disabled user left a native listener running")
	}
	configs, err := (&EndpointService{}).GetAllConfig(db)
	if err != nil || len(configs) != 0 {
		t.Fatalf("empty users emitted native server config: %s, %v", configs, err)
	}
}

func TestMASQUEManagedEligibilityAndNoSharedTLSDependency(t *testing.T) {
	db, _ := setupInboundRuntimeTest(t)
	tls := masqueManagedTestTLS(t)
	db.Create(&tls)
	inbound := model.Inbound{Type: "masque", Tag: "eligibility", TlsId: tls.Id, Tls: &tls, Options: json.RawMessage(`{"server":"localhost","listen_port":443,"version":[3],"system":false,"mtu":1280,"address":["172.16.0.1/24"]}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name               string
		disabled           bool
		volume, up, expiry int64
	}{{"active", false, 0, 0, 0}, {"disabled", true, 0, 0, 0}, {"quota", false, 100, 100, 0}, {"expired", false, 0, 0, time.Now().Unix() - 1}} {
		client := model.Client{Enable: !test.disabled, Name: test.name, Volume: test.volume, Up: test.up, Expiry: test.expiry, Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)), Config: json.RawMessage(`{}`)}
		if err := normalizeMasqueClientConfigs(db, []*model.Client{&client}); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&client).Error; err != nil {
			t.Fatal(err)
		}
	}
	users, err := loadMasqueUsers(db, &inbound)
	if err != nil || len(users) != 1 || users[0].Username != "active" {
		t.Fatalf("native eligible users: %+v, %v", users, err)
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`{"enabled":false}`), json.RawMessage(`{}`), json.RawMessage(`null`), json.RawMessage(`{"enabled":true,"max_version":"1.2"}`), json.RawMessage(`{"enabled":true,"reality":{"enabled":true}}`)} {
		copy := inbound
		copy.Tls = &model.Tls{Server: raw}
		copy.TlsId = 0
		if _, err := parseMasqueInbound(&copy); err != nil {
			t.Fatalf("MASQUE unexpectedly depends on an unrelated TLS template %s: %v", raw, err)
		}
	}
	copy := inbound
	copy.TlsId = 0
	copy.Tls = nil
	if _, err := parseMasqueInbound(&copy); err != nil {
		t.Fatalf("MASQUE rejected missing shared TLS template: %v", err)
	}
}

func masqueManagedTestTLS(t *testing.T) model.Tls {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	server, _ := json.Marshal(map[string]any{"enabled": true, "certificate": []string{certPEM}, "key": []string{keyPEM}})
	client, _ := json.Marshal(map[string]any{"server_name": "localhost", "certificate": []string{certPEM}})
	return model.Tls{Name: "native-local-test", Server: server, Client: client}
}
