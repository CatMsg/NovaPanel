package util

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
)

const maxExternalSubscriptionSize = 16 << 20

var externalSubscriptionTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Upstream subscription aggregation intentionally tolerates private and
	// mismatched certificates configured by the panel owner.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return transport
}()

func GetExternalLink(url string) string {
	data, _ := GetExternalLinkWithHeadersTimeout(url, 15*time.Second)
	return data
}

func GetExternalLinkWithHeaders(url string) (string, http.Header) {
	return GetExternalLinkWithHeadersTimeout(url, 15*time.Second)
}

func GetExternalLinkWithHeadersTimeout(url string, timeout time.Duration) (string, http.Header) {
	client := &http.Client{Transport: externalSubscriptionTransport, Timeout: timeout}

	response, err := client.Get(url)
	if err != nil {
		logger.Warning("sub: Error making HTTP request:", err)
		return "", nil
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		logger.Warning("sub: Upstream returned HTTP status:", response.StatusCode)
		return "", response.Header.Clone()
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxExternalSubscriptionSize+1))
	if err != nil {
		logger.Warning("sub: Error reading response body:", err)
		return "", nil
	}
	if len(body) > maxExternalSubscriptionSize {
		logger.Warning("sub: Upstream response exceeds 16 MiB limit")
		return "", response.Header.Clone()
	}

	data := StrOrBase64Encoded(string(body))
	return data, response.Header.Clone()
}

func GetExternalSub(url string) ([]map[string]interface{}, error) {
	var err error
	var result []map[string]interface{}

	if len(url) == 0 {
		return nil, common.NewError("no url")
	}

	data := GetExternalLink(url)
	if len(data) == 0 {
		return nil, common.NewError("no result")
	}

	// if the data is a JSON object
	if strings.HasPrefix(data, "{") && strings.HasSuffix(data, "}") {
		var jsonData map[string]interface{}
		err = json.Unmarshal([]byte(data), &jsonData)
		if err != nil {
			logger.Warning("sub: Error unmarshalling JSON:", err)
			return nil, err
		}
		outbounds, ok := jsonData["outbounds"].([]any)
		if !ok {
			logger.Warning("sub: Error getting outbounds:", err)
			return nil, err
		}
		for _, outbound := range outbounds {
			outboundMap, ok := outbound.(map[string]interface{})
			if ok && len(outboundMap) > 0 {
				oType, _ := outboundMap["type"].(string)
				switch oType {
				case "urltest":
				case "direct":
				case "selector":
				case "block":
					continue
				default:
					result = append(result, outboundMap)
				}
			}
		}
		if len(result) == 0 {
			return nil, common.NewError("no result")
		}
		return result, nil
	} else {
		// if data is a text
		links := strings.Split(data, "\n")
		for _, link := range links {
			linkToJson, _, err := GetOutbound(link, 0)
			if err == nil {
				result = append(result, *linkToJson)
			}
		}
	}
	if len(result) == 0 {
		return nil, common.NewError("no result")
	}
	return result, nil
}
