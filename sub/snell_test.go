package sub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gopkg.in/yaml.v3"
)

func TestSnellV6SharedPSKSubscriptions(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snell-subscription.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	inbound := model.Inbound{
		Type:    "snell",
		Tag:     "la-snell",
		Options: json.RawMessage(`{"listen_port":24443,"version":6,"psk":"shared-server-psk-123456","mode":"unshaped"}`),
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"snell","tag":"la-snell","server":"la.example","server_port":24443,"version":6,"psk":"shared-server-psk-123456","mode":"unshaped"}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	clientInboundIDs, err := json.Marshal([]uint{inbound.Id})
	if err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "snell-v6-subscription-alice",
		Config:   json.RawMessage(`{"vless":{"uuid":"existing-client-config"}}`),
		Inbounds: clientInboundIDs,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	native, _, err := (&JsonService{}).GetJson(client.Name, "json")
	if err != nil {
		t.Fatal(err)
	}
	var singbox struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(*native), &singbox); err != nil {
		t.Fatal(err)
	}
	singboxSnell := findSnellOutbound(singbox.Outbounds)
	if singboxSnell == nil {
		t.Fatalf("Snell outbound missing from sing-box subscription: %#v", singbox.Outbounds)
	}
	assertSnellSubscriptionFields(t, singboxSnell)
	if _, exists := singboxSnell["userkey"]; exists || strings.Contains(*native, "legacy-userkey-must-not-leak") {
		t.Fatalf("legacy per-user key leaked into sing-box subscription: %#v", singboxSnell)
	}
	if strings.Contains(*native, "nova_mihomo") {
		t.Fatal("sing-box JSON unexpectedly contains panel-only metadata")
	}

	clashService := &ClashService{}
	clash, _, err := clashService.GetClash(client.Name)
	if err != nil {
		t.Fatalf("Snell-only Mihomo-compatible subscription failed: %v", err)
	}
	if !strings.Contains(*clash, "type: snell") || !strings.Contains(*clash, "psk: shared-server-psk-123456") || !strings.Contains(*clash, "udp: true") || strings.Contains(*clash, "userkey") {
		t.Fatalf("Clash subscription did not export shared-PSK Snell cleanly: %s", *clash)
	}
	clashOutbounds := []map[string]interface{}{
		{
			"type": "snell", "tag": "la-snell", "server": "la.example", "server_port": 24443,
			"version": 6, "psk": "shared-server-psk-123456", "mode": "unshaped",
		},
		{
			"type": "socks", "tag": "la-socks", "server": "socks.example", "server_port": 1080,
			"username": "alice", "password": "socks-password",
		},
	}
	clashText, err := clashService.ConvertToClashMeta(&clashOutbounds, basicClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	if outputPath := os.Getenv("NOVAPANEL_MIHOMO_SNELL_CONFIG"); outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(clashText), 0600); err != nil {
			t.Fatalf("write generated Snell Clash profile: %v", err)
		}
	}
	var profile struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(clashText), &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.Proxies) != 2 || profile.Proxies[0]["name"] != "la-snell" || profile.Proxies[1]["name"] != "la-socks" {
		t.Fatalf("mixed Clash profile should include Snell and SOCKS: %#v", profile.Proxies)
	}
	for _, proxy := range profile.Proxies {
		if proxy["type"] == "snell" && (proxy["psk"] != "shared-server-psk-123456" || fmt.Sprint(proxy["version"]) != "6" || proxy["mode"] != "unshaped" || proxy["udp"] != true || proxy["userkey"] != nil) {
			t.Fatalf("invalid shared-PSK Snell fields in Mihomo subscription: %#v", proxy)
		}
	}
	legacyOutbounds, _, err := (&JsonService{}).getOutbounds(json.RawMessage(`{"snell":{"name":"alice","userkey":"legacy-userkey-must-not-leak"}}`), []*model.Inbound{&inbound})
	if err != nil {
		t.Fatalf("legacy Snell client config rejected: %v", err)
	}
	if legacySnell := findSnellOutbound(*legacyOutbounds); legacySnell == nil || legacySnell["userkey"] != nil {
		t.Fatalf("legacy Snell userkey was not stripped: %#v", legacyOutbounds)
	}
	unsafeRawInbound := inbound
	unsafeRawInbound.OutJson = json.RawMessage(`{"type":"snell","tag":"la-snell","server":"la.example","server_port":24443,"version":6,"psk":"shared-server-psk-123456","mode":"unsafe-raw"}`)
	unsafeRawOutbounds, _, err := (&JsonService{}).getOutbounds(client.Config, []*model.Inbound{&unsafeRawInbound})
	if err != nil {
		t.Fatalf("unsafe-raw sing-box subscription failed: %v", err)
	}
	unsafeRaw := findSnellOutbound(*unsafeRawOutbounds)
	if unsafeRaw == nil || unsafeRaw["mode"] != "unshaped" {
		t.Fatalf("Snell subscription did not normalize the legacy mode: %#v", unsafeRawOutbounds)
	}

	if err := db.Model(&inbound).Update("exclude_from_sub", true).Error; err != nil {
		t.Fatal(err)
	}
	clash, _, err = (&ClashService{}).GetClash(client.Name)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(*clash, "legacy-userkey-must-not-leak") || strings.Contains(*clash, "shared-server-psk-123456") {
		t.Fatal("hidden Snell inbound or its warning leaked into the Clash subscription")
	}
}

func assertSnellSubscriptionFields(t *testing.T, outbound map[string]any) {
	t.Helper()
	want := map[string]any{
		"type": "snell",
		"psk":  "shared-server-psk-123456",
		"mode": "unshaped",
	}
	for key, value := range want {
		if outbound[key] != value {
			t.Fatalf("Snell subscription field %q = %v, want %v; outbound: %#v", key, outbound[key], value, outbound)
		}
	}
	if fmt.Sprint(outbound["version"]) != "6" {
		t.Fatalf("Snell subscription version = %v, want 6; outbound: %#v", outbound["version"], outbound)
	}
}

func findSnellOutbound(outbounds []map[string]any) map[string]any {
	for _, outbound := range outbounds {
		if outbound["type"] == "snell" {
			return outbound
		}
	}
	return nil
}
