package sub

import (
	"encoding/json"
	"strings"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util"
)

type Link struct {
	Type   string `json:"type"`
	Remark string `json:"remark"`
	Uri    string `json:"uri"`
}

type LinkService struct {
}

func (s *LinkService) GetLinks(linkJson *json.RawMessage, types string, clientInfo string) []string {
	links := []Link{}
	var result []string
	err := json.Unmarshal(*linkJson, &links)
	if err != nil {
		return nil
	}
	for _, link := range links {
		switch link.Type {
		case "external":
			result = append(result, link.Uri)
		case "sub":
			subLinks := util.GetExternalLink(link.Uri)
			result = append(result, strings.Split(subLinks, "\n")...)
		case "local":
			if types == "all" {
				result = append(result, s.addClientInfo(link.Uri, clientInfo))
			}
		}
	}
	return result
}

func (s *LinkService) GetRemoteLinks(linkJson *json.RawMessage, includeSub bool) []string {
	links := []Link{}
	var result []string
	err := json.Unmarshal(*linkJson, &links)
	if err != nil {
		return nil
	}
	for _, link := range links {
		switch link.Type {
		case "external":
			result = append(result, strings.TrimSpace(link.Uri))
		case "sub":
			if !includeSub {
				continue
			}
			subLinks := util.GetExternalLink(link.Uri)
			for _, line := range strings.Split(subLinks, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					result = append(result, line)
				}
			}
		}
	}
	return result
}

func (s *LinkService) GetAssignedMasqueTags(linkJson *json.RawMessage) []string {
	links := []Link{}
	result := make([]string, 0)
	if err := json.Unmarshal(*linkJson, &links); err != nil {
		return result
	}

	seen := make(map[string]struct{})
	for _, link := range links {
		if link.Type != "masque" {
			continue
		}
		tag := strings.TrimSpace(link.Uri)
		if tag == "" {
			tag = strings.TrimSpace(link.Remark)
		}
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}

func (s *LinkService) addClientInfo(uri string, clientInfo string) string {
	if len(clientInfo) == 0 {
		return uri
	}
	protocol := strings.Split(uri, "://")
	if len(protocol) < 2 {
		return uri
	}
	switch protocol[0] {
	case "vmess":
		var vmessJson map[string]interface{}
		config, err := util.B64StrToByte(protocol[1])
		if err != nil {
			logger.Warning("sub: Error decoding vmess content:", err)
			return uri
		}
		err = json.Unmarshal(config, &vmessJson)
		if err != nil {
			logger.Warning("sub: Error decoding vmess content:", err)
			return uri
		}
		vmessJson["ps"] = vmessJson["ps"].(string) + clientInfo
		result, err := json.MarshalIndent(vmessJson, "", "  ")
		if err != nil {
			logger.Warning("sub: Error decoding vmess + clientInfo content:", err)
			return uri
		}
		return "vmess://" + util.ByteToB64Str(result)
	default:
		return uri + clientInfo
	}
}
