package util

import (
	"encoding/json"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestFillOutJsonPreservesSnellV6Mode(t *testing.T) {
	for _, mode := range []string{"default", "unshaped", "unsafe-raw"} {
		t.Run(mode, func(t *testing.T) {
			inbound := &model.Inbound{
				Type:    "snell",
				Tag:     "snell-" + mode,
				Options: json.RawMessage(`{"listen_port":24443,"version":6,"psk":"valid-shared-psk","mode":"` + mode + `"}`),
				OutJson: json.RawMessage(`{}`),
			}
			if err := FillOutJson(inbound, "snell.example"); err != nil {
				t.Fatalf("FillOutJson: %v", err)
			}
			var outbound map[string]any
			if err := json.Unmarshal(inbound.OutJson, &outbound); err != nil {
				t.Fatal(err)
			}
			if outbound["mode"] != mode {
				t.Fatalf("generated outbound mode = %v, want %q", outbound["mode"], mode)
			}
		})
	}
}
