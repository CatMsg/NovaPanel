package sub

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
)

func TestNativeMASQUEUserJSONVisibilityAndUnsupportedFormats(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "native-subscription.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	inbounds := []model.Inbound{
		{Type: "masque", Tag: "native-visible", Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{"type":"masque-client","tag":"native-visible","server":"127.0.0.1","server_port":14443,"version":3,"system":false,"on_demand":true,"disable_version_fallback":true,"tls":{"enabled":true,"alpn":["h3"]}}`)},
		{Type: "masque", Tag: "native-hidden", ExcludeFromSub: true, Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{"type":"masque-client","tag":"native-hidden","server":"127.0.0.1","server_port":24443,"version":3,"tls":{"enabled":true}}`)},
		{Type: "http", Tag: "plain-http", Addrs: json.RawMessage(`[]`), Options: json.RawMessage(`{"version":1}`), OutJson: json.RawMessage(`{"type":"http","tag":"plain-http","server":"127.0.0.1","server_port":18880,"version":1}`)},
	}
	if err := db.Create(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Enable: true, Name: "native-sub-user", Config: json.RawMessage(`{"masque":{"username":"native-sub-user","password":"synthetic-native-password"},"http":{"username":"http-user","password":"http-password"}}`), Inbounds: json.RawMessage(fmt.Sprintf(`[%d,%d,%d]`, inbounds[0].Id, inbounds[1].Id, inbounds[2].Id)), Links: json.RawMessage(`[]`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	service := JsonService{}
	result, _, err := service.GetJson(client.Name, "json")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatal(err)
	}
	endpoints, ok := config["endpoints"].([]any)
	if !ok || len(endpoints) != 1 {
		t.Fatalf("native JSON endpoint visibility failed: %#v", config["endpoints"])
	}
	endpoint := endpoints[0].(map[string]any)
	if endpoint["tag"] != "native-visible" || endpoint["username"] != client.Name || endpoint["password"] != "synthetic-native-password" {
		t.Fatalf("native JSON auth mismatch: %#v", endpoint)
	}
	for _, raw := range config["outbounds"].([]any) {
		if raw.(map[string]any)["type"] == "masque-client" {
			t.Fatal("native MASQUE leaked into outbounds")
		}
	}
	clash, _, err := (&ClashService{}).GetClash(client.Name)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(*clash, "masque") || strings.Contains(*clash, "synthetic-native-password") {
		t.Fatal("native MASQUE exposed as incompatible Clash key auth")
	}
	if got := util.LinkGenerator(client.Config, &inbounds[0], "localhost"); len(got) != 0 {
		t.Fatalf("native MASQUE emitted unsupported URI: %v", got)
	}
	if err := db.Model(&model.Inbound{}).Where("id = ?", inbounds[0].Id).Update("exclude_from_sub", true).Error; err != nil {
		t.Fatal(err)
	}
	result, _, err = service.GetJson(client.Name, "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(*result, "native-visible") || strings.Contains(*result, "native-hidden") || strings.Contains(*result, "synthetic-native-password") {
		t.Fatal("native visibility toggle served a stale JSON endpoint")
	}
}

func TestNativeMASQUESubscriptionDoesNotCollideWithGeneratedDirect(t *testing.T) {
	inbound := &model.Inbound{Type: "masque", Tag: "direct", Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{"type":"masque-client","tag":"direct","server":"127.0.0.1","server_port":443,"version":3,"system":false,"tls":{"enabled":true}}`)}
	nodes, tags, err := (&JsonService{}).getOutbounds(json.RawMessage(`{"masque":{"username":"alice","password":"test-only"}}`), []*model.Inbound{inbound})
	if err != nil || len(*nodes) != 1 || len(*tags) != 1 || (*nodes)[0]["tag"] != "masque.direct" || (*tags)[0] != "masque.direct" {
		t.Fatalf("native default tag collision: %#v %#v %v", nodes, tags, err)
	}
}
