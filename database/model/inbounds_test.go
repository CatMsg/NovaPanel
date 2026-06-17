package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInboundMarshalJSONStripsNaiveHttpVersion(t *testing.T) {
	inb := Inbound{
		Type: "naive",
		Tag:  "naive-demo",
		Options: json.RawMessage(`{
			"listen_port": 1234,
			"http_version": "http3",
			"quic_congestion_control": "bbr2"
		}`),
	}

	raw, err := inb.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON returned error: %v", err)
	}

	got := string(raw)
	if strings.Contains(got, "http_version") {
		t.Fatalf("MarshalJSON unexpectedly kept http_version: %s", got)
	}
	if !strings.Contains(got, `"quic_congestion_control":"bbr2"`) {
		t.Fatalf("MarshalJSON lost valid naive option: %s", got)
	}
}

func TestInboundMarshalFullKeepsNaiveHttpVersion(t *testing.T) {
	inb := Inbound{
		Id:   1,
		Type: "naive",
		Tag:  "naive-demo",
		Options: json.RawMessage(`{
			"listen_port": 1234,
			"http_version": "http3"
		}`),
	}

	got, err := inb.MarshalFull()
	if err != nil {
		t.Fatalf("MarshalFull returned error: %v", err)
	}

	if _, ok := (*got)["http_version"]; !ok {
		t.Fatalf("MarshalFull unexpectedly removed http_version: %#v", *got)
	}
}
