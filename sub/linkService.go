package sub

import (
	"encoding/json"
	"net/url"
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

func (s *LinkService) addClientInfo(uri string, clientInfo string) string {
	protocol := strings.Split(uri, "://")
	if len(protocol) < 2 {
		return uri
	}
	if len(clientInfo) == 0 && protocol[0] != "mierus" {
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
		remark, _ := vmessJson["ps"].(string)
		vmessJson["ps"] = remark + clientInfo
		result, err := json.MarshalIndent(vmessJson, "", "  ")
		if err != nil {
			logger.Warning("sub: Error decoding vmess + clientInfo content:", err)
			return uri
		}
		return "vmess://" + util.ByteToB64Str(result)
	case "mierus":
		parsed, err := url.Parse(uri)
		if err != nil {
			logger.Warning("sub: Error parsing Mieru link:", err)
			return uri
		}
		query := parsed.Query()
		query.Set("handshake-mode", "HANDSHAKE_STANDARD")
		profile := query.Get("profile")
		if profile != "" {
			query.Set("profile", profile+clientInfo)
			parsed.RawQuery = util.EncodeMieruQuery(query)
		} else {
			parsed.Fragment += clientInfo
		}
		return parsed.String()
	default:
		return uri + clientInfo
	}
}
