package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	urltest "github.com/sagernet/sing-box/common/urltest"
	M "github.com/sagernet/sing/common/metadata"
)

const checkTimeout = 15 * time.Second

const (
	outboundIdentityURL       = "https://www.cloudflare.com/cdn-cgi/trace"
	outboundIdentityTimeout   = 8 * time.Second
	outboundIdentityBodyLimit = 8 * 1024
)

type CheckOutboundResult struct {
	OK    bool
	Delay uint16
	Error string
}

func CheckOutbound(ctx context.Context, tag string, link string) (result CheckOutboundResult) {
	outboundManagerMu.RLock()
	defer outboundManagerMu.RUnlock()
	if outbound_manager == nil {
		result.Error = "core not running"
		return result
	}
	ob, ok := outbound_manager.Outbound(tag)
	if !ok {
		result.Error = "outbound not found"
		return result
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	delay, err := urltest.URLTest(ctx, link, ob)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.Delay = delay
	return result
}

type OutboundIdentity struct {
	PublicIP    string
	CountryCode string
	Colo        string
}

func FetchOutboundIdentity(ctx context.Context, tag string) (OutboundIdentity, error) {
	outboundManagerMu.RLock()
	defer outboundManagerMu.RUnlock()
	if outbound_manager == nil {
		return OutboundIdentity{}, errors.New("core not running")
	}
	outbound, ok := outbound_manager.Outbound(tag)
	if !ok {
		return OutboundIdentity{}, errors.New("outbound not found")
	}

	identityURL, err := url.Parse(outboundIdentityURL)
	if err != nil {
		return OutboundIdentity{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, outboundIdentityTimeout)
	defer cancel()

	transport := &http.Transport{
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return outbound.DialContext(ctx, network, M.ParseSocksaddr(address))
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   outboundIdentityTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return errors.New("too many identity endpoint redirects")
			}
			if !strings.EqualFold(request.URL.Hostname(), identityURL.Hostname()) {
				return errors.New("identity endpoint redirected to another host")
			}
			return nil
		},
	}
	defer transport.CloseIdleConnections()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, identityURL.String(), nil)
	if err != nil {
		return OutboundIdentity{}, err
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "NovaPanel outbound health")
	response, err := client.Do(request)
	if err != nil {
		return OutboundIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return OutboundIdentity{}, fmt.Errorf("identity endpoint returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, outboundIdentityBodyLimit+1))
	if err != nil {
		return OutboundIdentity{}, err
	}
	if len(body) > outboundIdentityBodyLimit {
		return OutboundIdentity{}, errors.New("identity response is too large")
	}
	return parseCloudflareTrace(body)
}

func parseCloudflareTrace(body []byte) (OutboundIdentity, error) {
	values := make(map[string]string, 3)
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "ip" || key == "loc" || key == "colo" {
			values[key] = strings.TrimSpace(value)
		}
	}
	if net.ParseIP(values["ip"]) == nil {
		return OutboundIdentity{}, errors.New("identity response has no valid public IP")
	}
	countryCode := strings.ToUpper(values["loc"])
	if len(countryCode) != 2 {
		countryCode = ""
	}
	colo := strings.ToUpper(values["colo"])
	if len(colo) > 8 {
		colo = ""
	}
	return OutboundIdentity{
		PublicIP:    values["ip"],
		CountryCode: countryCode,
		Colo:        colo,
	}, nil
}
