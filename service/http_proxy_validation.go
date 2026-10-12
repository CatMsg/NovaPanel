package service

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
)

func validateHTTPInboundWrite(inbound *model.Inbound, action string) error {
	if inbound == nil || inbound.Type != "http" {
		return nil
	}

	var options map[string]json.RawMessage
	if len(bytes.TrimSpace(inbound.Options)) != 0 && !bytes.Equal(bytes.TrimSpace(inbound.Options), []byte("null")) {
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			return fmt.Errorf("HTTP inbound options: %w", err)
		}
	}
	if options == nil {
		options = make(map[string]json.RawMessage)
	}

	rawVersion, exists := options["version"]
	if !exists {
		if action != "new" {
			return fmt.Errorf("HTTP inbound edit requires exactly one version choice (1, 2, or 3)")
		}
		rawVersion = json.RawMessage("1")
	}

	version := 0
	trimmedVersion := bytes.TrimSpace(rawVersion)
	if len(trimmedVersion) > 0 && trimmedVersion[0] == '[' {
		var versions []int
		if err := json.Unmarshal(trimmedVersion, &versions); err != nil || len(versions) != 1 {
			return fmt.Errorf("HTTP inbound requires exactly one version choice (1, 2, or 3)")
		}
		version = versions[0]
	} else if err := json.Unmarshal(trimmedVersion, &version); err != nil {
		return fmt.Errorf("HTTP inbound requires exactly one version choice (1, 2, or 3)")
	}
	if version < 1 || version > 3 {
		return fmt.Errorf("HTTP inbound version must be 1, 2, or 3")
	}

	if version >= 2 && !util.HTTPInboundTLSEnabled(inbound) {
		return fmt.Errorf("HTTP inbound version %d requires an enabled TLS server configuration", version)
	}

	canonical, err := json.Marshal(version)
	if err != nil {
		return err
	}
	options["version"] = canonical
	inbound.Options, err = json.MarshalIndent(options, "", "  ")
	if err != nil {
		return err
	}
	return nil
}

func validateHTTPProxyConfig(config map[string]interface{}) error {
	for index, inbound := range objectList(config["inbounds"]) {
		if stringValue(inbound["type"]) != "http" {
			continue
		}
		options, err := json.Marshal(inbound)
		if err != nil {
			return fmt.Errorf("inbounds[%d]: %w", index, err)
		}
		versions, err := util.HTTPInboundVersions(options)
		if err != nil {
			return fmt.Errorf("inbounds[%d]: %w", index, err)
		}
		if includesHTTPVersion(versions, 3) {
			tls := objectValue(inbound["tls"])
			if enabled, _ := tls["enabled"].(bool); !enabled {
				return fmt.Errorf("inbounds[%d]: HTTP inbound version 3 requires an enabled TLS server configuration", index)
			}
		}
	}
	for index, outbound := range objectList(config["outbounds"]) {
		if stringValue(outbound["type"]) != "http" {
			continue
		}
		if _, err := util.ResolveHTTPOutboundVersion(outbound, nil); err != nil {
			return fmt.Errorf("outbounds[%d]: %w", index, err)
		}
	}
	return nil
}

func validateHTTPOutboundModel(outbound *model.Outbound) error {
	if outbound == nil || outbound.Type != "http" {
		return nil
	}
	var options map[string]interface{}
	if err := json.Unmarshal(outbound.Options, &options); err != nil {
		return fmt.Errorf("HTTP outbound options: %w", err)
	}
	if options == nil {
		options = make(map[string]interface{})
	}
	if _, err := util.ResolveHTTPOutboundVersion(options, nil); err != nil {
		return err
	}
	return nil
}

func includesHTTPVersion(versions []int, wanted int) bool {
	for _, version := range versions {
		if version == wanted {
			return true
		}
	}
	return false
}
