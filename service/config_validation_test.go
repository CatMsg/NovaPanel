package service

import (
	"strings"
	"testing"
)

func TestValidateConfigReferencesAcceptsValidRoute(t *testing.T) {
	config := []byte(`{
		"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":1080}],
		"outbounds":[
			{"type":"direct","tag":"proxy-a"},
			{"type":"selector","tag":"select","outbounds":["proxy-a"]}
		],
		"route":{
			"final":"select",
			"rule_set":[{"type":"remote","tag":"remote","format":"binary","url":"https://example.com/rules.srs","download_detour":"proxy-a"}],
			"rules":[{"inbound":["mixed-in"],"rule_set":["remote"],"action":"route","outbound":"proxy-a"}]
		}
	}`)
	if err := validateConfigReferences(config); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateConfigReferencesRejectsMissingOutbound(t *testing.T) {
	config := []byte(`{"outbounds":[{"type":"direct","tag":"proxy-a"}],"route":{"final":"missing"}}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "route.final") {
		t.Fatalf("expected missing final outbound error, got %v", err)
	}
}

func TestValidateConfigReferencesRejectsMissingNestedRuleSet(t *testing.T) {
	config := []byte(`{"route":{"rules":[{"type":"logical","rules":[{"rule_set":["missing"]}]}]}}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "不存在的规则集") {
		t.Fatalf("expected missing rule-set error, got %v", err)
	}
}

func TestValidateConfigReferencesRejectsOutboundCycle(t *testing.T) {
	config := []byte(`{
		"outbounds":[
			{"type":"selector","tag":"a","outbounds":["b"]},
			{"type":"selector","tag":"b","outbounds":["a"]}
		]
	}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "形成循环") {
		t.Fatalf("expected outbound cycle error, got %v", err)
	}
}

func TestValidateConfigReferencesRejectsEmptySelector(t *testing.T) {
	config := []byte(`{"outbounds":[{"type":"selector","tag":"select","outbounds":[]}]}`)
	err := validateConfigReferences(config)
	if err == nil || !strings.Contains(err.Error(), "至少需要一个成员出站") {
		t.Fatalf("expected empty selector error, got %v", err)
	}
}
