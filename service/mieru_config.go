package service

import (
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
	"HANDSHAKE_NO_WAIT":  {},
}

type mieruEndpointConfig struct {
	Tag           string
	Server        string
	Port          int
	PortRange     string
	Ports         []int
	Transport     string
	Username      string
	Password      string
	Multiplexing  string
	HandshakeMode string
	MTU           int
}

type mitaServerConfig struct {
	PortBindings []mitaPortBinding `json:"portBindings"`
	Users        []mitaUser        `json:"users"`
	LoggingLevel string            `json:"loggingLevel"`
	MTU          int               `json:"mtu,omitempty"`
	DNS          mitaDNS           `json:"dns"`
}

type mitaPortBinding struct {
	Port      int    `json:"port,omitempty"`
	PortRange string `json:"portRange,omitempty"`
	Protocol  string `json:"protocol"`
}

type mitaUser struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type mitaDNS struct {
	DualStack string `json:"dualStack"`
}

func parseMieruEndpoint(endpoint *model.Endpoint) (*mieruEndpointConfig, error) {
	if endpoint == nil {
		return nil, common.NewError("missing mieru endpoint")
	}
	full, err := endpoint.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(full, &payload); err != nil {
		return nil, err
	}

	config := &mieruEndpointConfig{
		Tag:           strings.TrimSpace(endpoint.Tag),
		Server:        mieruString(payload["server"]),
		PortRange:     mieruString(payload["port_range"]),
		Transport:     strings.ToUpper(mieruString(payload["transport"])),
		Username:      mieruString(payload["username"]),
		Password:      mieruString(payload["password"]),
		Multiplexing:  strings.ToUpper(mieruString(payload["multiplexing"])),
		HandshakeMode: strings.ToUpper(mieruString(payload["handshake_mode"])),
		MTU:           mieruInt(payload["mtu"]),
	}
	if rawPort, ok := payload["port"]; ok && rawPort != nil && strings.TrimSpace(fmt.Sprint(rawPort)) != "" {
		config.Port, err = normalizeManagedPort(rawPort)
		if err != nil {
			return nil, fmt.Errorf("invalid mieru port: %w", err)
		}
	}
	if config.Transport == "" {
		config.Transport = "TCP"
	}
	if config.Multiplexing == "" {
		config.Multiplexing = "MULTIPLEXING_LOW"
	}
	if config.HandshakeMode == "" {
		config.HandshakeMode = "HANDSHAKE_STANDARD"
	}
	if config.MTU == 0 {
		config.MTU = 1400
	}

	config.Ports, err = parseMieruPorts(config.Port, config.PortRange)
	if err != nil {
		return nil, err
	}
	if err := validateMieruEndpointConfig(config); err != nil {
		return nil, err
	}
	return config, nil
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

func validateMieruEndpointConfig(config *mieruEndpointConfig) error {
	if config == nil {
		return common.NewError("missing mieru config")
	}
	if config.Tag == "" {
		return common.NewError("mieru tag is required")
	}
	if config.Server == "" {
		return common.NewError("mieru server is required")
	}
	if config.Username == "" {
		return common.NewError("mieru username is required")
	}
	if config.Password == "" {
		return common.NewError("mieru password is required")
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
	if config.MTU < 1280 || config.MTU > 1500 {
		return fmt.Errorf("invalid mieru MTU %d: expected 1280-1500", config.MTU)
	}
	for _, port := range config.Ports {
		if port < 1025 || port > 65535 {
			return fmt.Errorf("invalid mieru port %d: expected 1025-65535", port)
		}
	}
	return nil
}

func parseMieruPorts(port int, portRange string) ([]int, error) {
	portRange = strings.TrimSpace(portRange)
	if port > 0 && portRange != "" {
		return nil, common.NewError("mieru port and port range cannot be used together")
	}
	if port > 0 {
		if port < 1025 || port > 65535 {
			return nil, fmt.Errorf("invalid mieru port %d: expected 1025-65535", port)
		}
		return []int{port}, nil
	}
	if portRange == "" {
		return nil, common.NewError("mieru port or port range is required")
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
	if end-start+1 > maxMieruPortRangeSize {
		return nil, fmt.Errorf("mieru port range is too large: maximum %d ports", maxMieruPortRangeSize)
	}
	ports := make([]int, 0, end-start+1)
	for current := start; current <= end; current++ {
		ports = append(ports, current)
	}
	return ports, nil
}

func buildMitaServerConfig(configs []*mieruEndpointConfig) (*mitaServerConfig, error) {
	result := &mitaServerConfig{
		PortBindings: make([]mitaPortBinding, 0, len(configs)),
		Users:        make([]mitaUser, 0, len(configs)),
		LoggingLevel: "INFO",
		MTU:          1400,
		DNS:          mitaDNS{DualStack: "PREFER_IPv4"},
	}
	seenUsers := make(map[string]string)
	for _, config := range configs {
		if err := validateMieruEndpointConfig(config); err != nil {
			return nil, err
		}
		if owner, exists := seenUsers[config.Username]; exists {
			return nil, fmt.Errorf("mieru username %q is already used by %s", config.Username, owner)
		}
		seenUsers[config.Username] = config.Tag
		binding := mitaPortBinding{Protocol: config.Transport}
		if config.PortRange != "" {
			binding.PortRange = config.PortRange
		} else {
			binding.Port = config.Port
		}
		result.PortBindings = append(result.PortBindings, binding)
		result.Users = append(result.Users, mitaUser{Name: config.Username, Password: config.Password})
		if config.MTU < result.MTU {
			result.MTU = config.MTU
		}
	}
	return result, nil
}
