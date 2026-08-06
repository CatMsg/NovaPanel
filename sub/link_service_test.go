package sub

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/util"
)

func TestAddClientInfoHandlesVMessWithoutRemark(t *testing.T) {
	payload, err := json.Marshal(map[string]interface{}{"add": "example.com", "port": "443"})
	if err != nil {
		t.Fatal(err)
	}
	result := (&LinkService{}).addClientInfo("vmess://"+util.ByteToB64Str(payload), " | quota")
	encoded := strings.TrimPrefix(result, "vmess://")
	decoded, err := util.B64StrToByte(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(decoded, &config); err != nil {
		t.Fatal(err)
	}
	if config["ps"] != " | quota" {
		t.Fatalf("unexpected remark: %#v", config["ps"])
	}
}
