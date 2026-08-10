package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
)

const maxMieruPortRangeSize = 512

var validMieruMultiplexing = map[string]struct{}{
	"MULTIPLEXING_OFF":    {},
	"MULTIPLEXING_LOW":    {},
	"MULTIPLEXING_MIDDLE": {},
	"MULTIPLEXING_HIGH":   {},
}

var validMieruHandshakeModes = map[string]struct{}{
	"HANDSHAKE_STANDARD": {},
}

var validMieruTrafficPatterns = map[string]int{
	"DEFAULT":  0,
	"BALANCED": 1,
	"ENHANCED": 2,
}

type mieruInboundConfig struct {
	ID             uint
	Tag            string
	ListenPort     int
	PortRange      string
	Ports          []int
	Transport      string
	Multiplexing   string
	HandshakeMode  string
	TrafficPattern string
	MTU            int
}

type mieruClientCredential struct {
	Name     string
	Password string
}

type mitaServerConfig struct {
	PortBindings   []mitaPortBinding   `json:"portBindings"`
	Users          []mitaUser          `json:"users"`
	LoggingLevel   string              `json:"loggingLevel"`
	TrafficPattern *mitaTrafficPattern `json:"trafficPattern,omitempty"`
	MTU            int                 `json:"mtu,omitempty"`
	DNS            mitaDNS             `json:"dns"`
	Egress         mitaEgress          `json:"egress"`
}

type mitaPortBinding struct {
	Port      int    `json:"port,omitempty"`
	PortRange string `json:"portRange,omitempty"`
	Protocol  string `json:"protocol"`
}

type mitaUser struct {
	Name           string `json:"name"`
	HashedPassword string `json:"hashedPassword"`
}

type mitaTrafficPattern struct {
	Seed        int                `json:"seed"`
	UnlockAll   bool               `json:"unlockAll"`
	TCPFragment mitaTCPFragment    `json:"tcpFragment"`
	Nonce       mitaNoncePattern   `json:"nonce"`
	Padding     mitaPaddingPattern `json:"padding"`
}

type mitaTCPFragment struct {
	Enable     bool `json:"enable"`
	MaxSleepMs int  `json:"maxSleepMs"`
}

type mitaNoncePattern struct {
	Type                string `json:"type"`
	ApplyToAllUDPPacket bool   `json:"applyToAllUDPPacket"`
	MinLen              int    `json:"minLen"`
	MaxLen              int    `json:"maxLen"`
}

type mitaPaddingPattern struct {
	MaxMiddlePaddingLen int `json:"maxMiddlePaddingLen"`
	MaxEndPaddingLen    int `json:"maxEndPaddingLen"`
}

type mitaDNS struct {
	DualStack string `json:"dualStack"`
}

type mitaEgress struct {
	Proxies []mitaEgressProxy `json:"proxies"`
	Rules   []mitaEgressRule  `json:"rules"`
}

type mitaEgressProxy struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type mitaEgressRule struct {
	IPRanges    []string `json:"ipRanges"`
	DomainNames []string `json:"domainNames"`
	Action      string   `json:"action"`
	ProxyNames  []string `json:"proxyNames"`
}

func parseMieruInbound(inbound *model.Inbound) (*mieruInboundConfig, error) {
	if inbound == nil {
		return nil, common.NewError("missing mieru inbound")
	}
	full, err := inbound.MarshalFull()
	if err != nil {
		return nil, err
	}

	config := &mieruInboundConfig{
		ID:             inbound.Id,
		Tag:            strings.TrimSpace(inbound.Tag),
		ListenPort:     mieruInt((*full)["listen_port"]),
		PortRange:      mieruString((*full)["port_range"]),
		Transport:      strings.ToUpper(mieruString((*full)["transport"])),
		Multiplexing:   strings.ToUpper(mieruString((*full)["multiplexing"])),
		HandshakeMode:  strings.ToUpper(mieruString((*full)["handshake_mode"])),
		TrafficPattern: strings.ToUpper(mieruString((*full)["traffic_pattern"])),
		MTU:            mieruInt((*full)["mtu"]),
	}
	if config.Transport == "" {
		config.Transport = "TCP"
	}
	if config.Multiplexing == "" {
		config.Multiplexing = "MULTIPLEXING_LOW"
	}
	if config.HandshakeMode == "" || config.HandshakeMode == "HANDSHAKE_NO_WAIT" {
		config.HandshakeMode = "HANDSHAKE_STANDARD"
	}
	if config.TrafficPattern == "" {
		config.TrafficPattern = "DEFAULT"
	}
	if config.MTU == 0 {
		config.MTU = 1400
	}

	config.Ports, err = parseMieruInboundPorts(config.ListenPort, config.PortRange)
	if err != nil {
		return nil, err
	}
	if err := validateMieruInboundConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

func normalizeMieruInboundOptions(inbound *model.Inbound) error {
	if inbound == nil {
		return common.NewError("missing mieru inbound")
	}
	options := make(map[string]interface{})
	if len(inbound.Options) > 0 {
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			return fmt.Errorf("parse Mieru options: %w", err)
		}
	}
	options["handshake_mode"] = "HANDSHAKE_STANDARD"
	payload, err := json.MarshalIndent(options, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Mieru options: %w", err)
	}
	inbound.Options = payload
	return nil
}

func parseMieruClientCredential(client *model.Client) (mieruClientCredential, error) {
	if client == nil {
		return mieruClientCredential{}, common.NewError("missing Mieru client")
	}
	var configs map[string]json.RawMessage
	if err := json.Unmarshal(client.Config, &configs); err != nil {
		return mieruClientCredential{}, fmt.Errorf("parse client %s config: %w", client.Name, err)
	}
	raw, ok := configs["mieru"]
	if !ok || len(raw) == 0 {
		return mieruClientCredential{}, fmt.Errorf("client %s has no Mieru credentials", client.Name)
	}
	var credential mieruClientCredential
	if err := json.Unmarshal(raw, &credential); err != nil {
		return mieruClientCredential{}, fmt.Errorf("parse client %s Mieru credentials: %w", client.Name, err)
	}
	credential.Name = strings.TrimSpace(client.Name)
	credential.Password = strings.TrimSpace(credential.Password)
	if credential.Name == "" {
		return mieruClientCredential{}, common.NewError("Mieru username is empty")
	}
	if credential.Password == "" {
		return mieruClientCredential{}, fmt.Errorf("client %s has an empty Mieru password", client.Name)
	}
	return credential, nil
}

func mieruString(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mieruInt(value interface{}) int {
	if value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return parsed
	}
}

func validateMieruInboundConfig(config *mieruInboundConfig) error {
	if config == nil {
		return common.NewError("missing mieru config")
	}
	if config.Tag == "" {
		return common.NewError("mieru tag is required")
	}
	if config.Transport != "TCP" && config.Transport != "UDP" {
		return fmt.Errorf("unsupported mieru transport %q", config.Transport)
	}
	if _, ok := validMieruMultiplexing[config.Multiplexing]; !ok {
		return fmt.Errorf("unsupported mieru multiplexing %q", config.Multiplexing)
	}
	if _, ok := validMieruHandshakeModes[config.HandshakeMode]; !ok {
		return fmt.Errorf("unsupported mieru handshake mode %q", config.HandshakeMode)
	}
	if _, ok := validMieruTrafficPatterns[config.TrafficPattern]; !ok {
		return fmt.Errorf("unsupported mieru traffic pattern %q", config.TrafficPattern)
	}
	if config.MTU < 1280 || config.MTU > 1500 {
		return fmt.Errorf("invalid mieru MTU %d: expected 1280-1500", config.MTU)
	}
	return nil
}

func parseMieruInboundPorts(listenPort int, portRange string) ([]int, error) {
	if listenPort < 1025 || listenPort > 65535 {
		return nil, fmt.Errorf("invalid mieru listen port %d: expected 1025-65535", listenPort)
	}
	portRange = strings.TrimSpace(portRange)
	if portRange == "" {
		return []int{listenPort}, nil
	}
	parts := strings.Split(portRange, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid mieru port range %q: expected start-end", portRange)
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid mieru port range %q", portRange)
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid mieru port range %q", portRange)
	}
	if start < 1025 || end > 65535 || start > end {
		return nil, fmt.Errorf("invalid mieru port range %q", portRange)
	}
	if listenPort != start {
		return nil, fmt.Errorf("mieru listen port must match the first port in range %q", portRange)
	}
	if end-start+1 > maxMieruPortRangeSize {
		return nil, fmt.Errorf("mieru port range is too large: maximum %d ports", maxMieruPortRangeSize)
	}
	ports := make([]int, 0, end-start+1)
	for current := start; current <= end; current++ {
		ports = append(ports, current)
	}
	return ports, nil
}

func buildMitaServerConfig(config *mieruInboundConfig, credentials []mieruClientCredential) (*mitaServerConfig, error) {
	if err := validateMieruInboundConfig(config); err != nil {
		return nil, err
	}
	bridgePort, err := getMieruBridgePort()
	if err != nil {
		return nil, err
	}
	result := &mitaServerConfig{
		PortBindings: make([]mitaPortBinding, 0, 1),
		Users:        make([]mitaUser, 0, len(credentials)),
		LoggingLevel: "INFO",
		MTU:          config.MTU,
		DNS:          mitaDNS{DualStack: "PREFER_IPv4"},
		Egress: mitaEgress{
			Proxies: []mitaEgressProxy{{
				Name:     mieruBridgeProxyName,
				Protocol: "SOCKS5_PROXY_PROTOCOL",
				Host:     mieruBridgeHost,
				Port:     bridgePort,
			}},
			Rules: []mitaEgressRule{{
				IPRanges:    []string{"*"},
				DomainNames: []string{"*"},
				Action:      "PROXY",
				ProxyNames:  []string{mieruBridgeProxyName},
			}},
		},
	}
	binding := mitaPortBinding{Protocol: config.Transport}
	if config.PortRange != "" {
		binding.PortRange = config.PortRange
	} else {
		binding.Port = config.ListenPort
	}
	result.PortBindings = append(result.PortBindings, binding)

	seenUsers := make(map[string]struct{}, len(credentials))
	for _, credential := range credentials {
		credential.Name = strings.TrimSpace(credential.Name)
		credential.Password = strings.TrimSpace(credential.Password)
		if credential.Name == "" || credential.Password == "" {
			return nil, common.NewError("Mieru username and password are required")
		}
		if _, exists := seenUsers[credential.Name]; exists {
			return nil, fmt.Errorf("duplicate Mieru username %q", credential.Name)
		}
		seenUsers[credential.Name] = struct{}{}
		result.Users = append(result.Users, mitaUser{
			Name:           credential.Name,
			HashedPassword: hashMieruPassword(credential.Name, credential.Password),
		})
	}
	result.TrafficPattern = buildMitaTrafficPattern(validMieruTrafficPatterns[config.TrafficPattern])
	return result, nil
}

func hashMieruPassword(username, password string) string {
	payload := make([]byte, 0, len(password)+len(username)+1)
	payload = append(payload, password...)
	payload = append(payload, 0)
	payload = append(payload, username...)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func buildMitaTrafficPattern(level int) *mitaTrafficPattern {
	switch level {
	case 1:
		return &mitaTrafficPattern{
			Seed:      1031,
			UnlockAll: false,
			TCPFragment: mitaTCPFragment{
				Enable: false, MaxSleepMs: 0,
			},
			Nonce: mitaNoncePattern{
				Type: "NONCE_TYPE_PRINTABLE_SUBSET", ApplyToAllUDPPacket: false, MinLen: 4, MaxLen: 6,
			},
			Padding: mitaPaddingPattern{MaxMiddlePaddingLen: 32, MaxEndPaddingLen: 64},
		}
	case 2:
		return &mitaTrafficPattern{
			Seed:      2053,
			UnlockAll: true,
			TCPFragment: mitaTCPFragment{
				Enable: true, MaxSleepMs: 5,
			},
			Nonce: mitaNoncePattern{
				Type: "NONCE_TYPE_PRINTABLE", ApplyToAllUDPPacket: true, MinLen: 6, MaxLen: 8,
			},
			Padding: mitaPaddingPattern{MaxMiddlePaddingLen: 64, MaxEndPaddingLen: 128},
		}
	default:
		return nil
	}
}

func MieruTrafficPatternBase64(preset string) string {
	switch strings.ToUpper(strings.TrimSpace(preset)) {
	case "BALANCED":
		return "CIcIEAAaBAgAEAAiCAgCEAAYBCAGKgQIIBBA"
	case "ENHANCED":
		return "CIUQEAEaBAgBEAUiCAgBEAEYBiAIKgUIQBCAAQ=="
	default:
		return ""
	}
}
