package util

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
)

const maxExternalSubscriptionSize = 16 << 20

var externalSubscriptionTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Subscription URLs are administrator-configured but fetched through public
	// endpoints. Do not let an ambient proxy bypass the destination-IP policy.
	transport.Proxy = nil
	transport.DialContext = dialExternalSubscription
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return transport
}()

var externalSubscriptionDeniedPrefixes = mustParsePrefixes(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"240.0.0.0/4",
	"::/96",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"fec0::/10",
	"3fff::/20",
)

func mustParsePrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func dialExternalSubscription(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return dialExternalSubscriptionWith(ctx, network, address, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

func dialExternalSubscriptionWith(
	ctx context.Context,
	network string,
	address string,
	lookupNetIP func(context.Context, string, string) ([]netip.Addr, error),
	dialContext func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.Contains(host, "%") {
		return nil, errors.New("invalid external subscription address")
	}
	var ips []netip.Addr
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		ips = []netip.Addr{ip.Unmap()}
	} else {
		ips, err = lookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve external subscription host: %w", err)
		}
	}
	if err := validateExternalSubscriptionIPs(ips); err != nil {
		return nil, err
	}

	var dialErrors []error
	for _, ip := range ips {
		if network == "tcp4" && !ip.Is4() || network == "tcp6" && !ip.Is6() {
			continue
		}
		conn, dialErr := dialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		dialErrors = append(dialErrors, dialErr)
	}
	if len(dialErrors) == 0 {
		return nil, errors.New("no external subscription address matches the network")
	}
	return nil, errors.Join(dialErrors...)
}

func validateExternalSubscriptionIPs(ips []netip.Addr) error {
	if len(ips) == 0 {
		return errors.New("external subscription host resolved to no addresses")
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
			return errors.New("external subscription host resolved to a non-public address")
		}
		for _, prefix := range externalSubscriptionDeniedPrefixes {
			if prefix.Contains(ip) {
				return errors.New("external subscription host resolved to a reserved address")
			}
		}
	}
	return nil
}

func GetExternalLink(url string) string {
	data, _ := GetExternalLinkWithHeadersTimeout(url, 15*time.Second)
	return data
}

func GetExternalLinkWithHeaders(url string) (string, http.Header) {
	return GetExternalLinkWithHeadersTimeout(url, 15*time.Second)
}

func GetExternalLinkWithHeadersTimeout(rawURL string, timeout time.Duration) (string, http.Header) {
	return GetExternalLinkWithHeadersContext(context.Background(), rawURL, timeout)
}

func GetExternalLinkWithHeadersContext(ctx context.Context, rawURL string, timeout time.Duration) (string, http.Header) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestURL, transport, pinnedRedirectOrigin, closeTransport, err := prepareExternalSubscriptionURL(rawURL)
	if err != nil {
		logger.Warning("sub: Invalid external subscription URL")
		return "", nil
	}
	if closeTransport {
		defer transport.CloseIdleConnections()
	}
	requestContext := ctx
	cancel := func() {}
	if timeout > 0 {
		requestContext, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	client := &http.Client{Transport: transport, Timeout: timeout}
	if pinnedRedirectOrigin != "" {
		client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if externalSubscriptionOrigin(request.URL) != pinnedRedirectOrigin {
				return errors.New("explicit subscription redirect must remain on the configured origin")
			}
			return nil
		}
	} else if initialURL, parseErr := url.Parse(requestURL); parseErr == nil && strings.EqualFold(initialURL.Scheme, "https") {
		client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if !strings.EqualFold(request.URL.Scheme, "https") {
				return errors.New("HTTPS subscription redirects must remain encrypted")
			}
			return nil
		}
	}

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, requestURL, nil)
	if err != nil {
		logger.Warning("sub: Invalid external subscription request")
		return "", nil
	}
	response, err := client.Do(request)
	if err != nil {
		logger.Warning("sub: Error making external subscription HTTP request")
		return "", nil
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		logger.Warning("sub: Upstream returned HTTP status:", response.StatusCode)
		return "", response.Header.Clone()
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxExternalSubscriptionSize+1))
	if err != nil {
		logger.Warning("sub: Error reading external subscription response")
		return "", nil
	}
	if len(body) > maxExternalSubscriptionSize {
		logger.Warning("sub: Upstream response exceeds 16 MiB limit")
		return "", response.Header.Clone()
	}

	data := StrOrBase64Encoded(string(body))
	return data, response.Header.Clone()
}

func externalSubscriptionOrigin(parsed *url.URL) string {
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func prepareExternalSubscriptionURL(rawURL string) (string, *http.Transport, string, bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", nil, "", false, errors.New("subscription URL must be absolute")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		return parsed.String(), externalSubscriptionTransport, "", false, nil
	case "insecure+https":
		parsed.Scheme = "https"
		transport := externalSubscriptionTransport.Clone()
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if transport.TLSClientConfig != nil {
			tlsConfig = transport.TLSClientConfig.Clone()
		}
		// #nosec G402 -- explicitly enabled per source; redirects are pinned to this HTTPS authority.
		tlsConfig.InsecureSkipVerify = true
		transport.TLSClientConfig = tlsConfig
		return parsed.String(), transport, externalSubscriptionOrigin(parsed), true, nil
	case "internal+http", "internal+https":
		parsed.Scheme = strings.TrimPrefix(strings.ToLower(parsed.Scheme), "internal+")
		transport := externalSubscriptionTransport.Clone()
		transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		return parsed.String(), transport, externalSubscriptionOrigin(parsed), true, nil
	default:
		return "", nil, "", false, fmt.Errorf("unsupported subscription URL scheme %q", parsed.Scheme)
	}
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
