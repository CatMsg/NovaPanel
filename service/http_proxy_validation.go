package service

import (
	"encoding/json"
	"fmt"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
)

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
