package core

import (
	"testing"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
)

func TestValidateConfigRejectsInvalidOutboundOptions(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	core := NewCore()
	err := core.ValidateConfig([]byte(`{"outbounds":[{"type":"direct","tag":"direct","bind_interface":123}]}`))
	if err == nil {
		t.Fatal("expected invalid outbound option type to be rejected")
	}
}

func TestValidateConfigAcceptsSingBox115Options(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	runtime := NewCore()
	err := runtime.ValidateConfig([]byte(`{
		"dns":{"servers":[{"type":"local","tag":"local"}],"optimistic":true,"timeout":"5s"},
		"http_clients":[{"tag":"rules"}],
		"inbounds":[{"type":"tun","tag":"tun-in","address":["172.19.0.1/30"],"multi_queue":true}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"default_http_client":"rules","rule_set":[{"type":"remote","tag":"test","url":"https://example.com/test.srs","http_client":"rules"}]}
	}`))
	if err != nil {
		t.Fatalf("expected sing-box 1.15 options to be accepted: %v", err)
	}
}

func TestValidateConfigAcceptsInlineACMECertificateProvider(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	runtime := NewCore()
	err := runtime.ValidateConfig([]byte(`{
		"inbounds":[{
			"type":"vless",
			"tag":"vless-in",
			"listen":"::",
			"listen_port":443,
			"users":[],
			"tls":{
				"enabled":true,
				"certificate_provider":{
					"type":"acme",
					"domain":["edge.example"],
					"provider":"letsencrypt",
					"profile":"shortlived",
					"account_key":"account-key",
					"key_type":"p256",
					"external_account":{"key_id":"account-id","mac_key":"account-mac"},
					"dns01_challenge":{"provider":"cloudflare","api_token":"dns-token"}
				}
			}
		}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"final":"direct"}
	}`))
	if err != nil {
		t.Fatalf("expected inline ACME certificate provider options to parse: %v", err)
	}
}
