package service

import (
	"encoding/json"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
)

func TestMarshalEndpointConfigForCoreSkipsWireGuardPeersWithoutAddress(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	endpoint := &model.Endpoint{
		Type: "wireguard",
		Tag:  "wg-test",
		Options: json.RawMessage(`{
			"listen_port": 500,
			"peers": [
				{
					"public_key": "peer-without-address",
					"allowed_ips": ["10.0.0.2/32"]
				},
				{
					"address": "203.0.113.10",
					"port": 51820,
					"public_key": "peer-with-address",
					"allowed_ips": ["10.0.0.3/32"]
				}
			]
		}`),
	}

	raw, err := marshalEndpointConfigForCore(endpoint)
	if err != nil {
		t.Fatalf("marshal endpoint config: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	peers, ok := payload["peers"].([]interface{})
	if !ok {
		t.Fatalf("missing peers in payload: %#v", payload)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer after filtering, got %d: %#v", len(peers), peers)
	}

	peer, ok := peers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected peer payload: %#v", peers[0])
	}
	if peer["address"] != "203.0.113.10" {
		t.Fatalf("unexpected peer address: %#v", peer["address"])
	}
}
