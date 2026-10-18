package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
	"github.com/CatMsg/NovaPanel/util/common"
)

type FleetInboundTLSProfile struct {
	ID                     uint     `json:"id"`
	Name                   string   `json:"name"`
	ServerEnabled          bool     `json:"serverEnabled"`
	ServerName             string   `json:"serverName,omitempty"`
	ACMEDomains            []string `json:"acmeDomains,omitempty"`
	CertificateFingerprint string   `json:"certificateFingerprint,omitempty"`
}

type FleetInboundCapabilities struct {
	Hostname         string                   `json:"hostname"`
	SubscriptionHost string                   `json:"subscriptionHost"`
	TLSProfiles      []FleetInboundTLSProfile `json:"tlsProfiles"`
	InboundTags      []string                 `json:"inboundTags"`
}

type FleetInboundTargetInfo struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Success      bool                      `json:"success"`
	Capabilities *FleetInboundCapabilities `json:"capabilities,omitempty"`
	Error        string                    `json:"error,omitempty"`
}

type FleetInboundTargetSelection struct {
	ID         string `json:"id"`
	TLSID      uint   `json:"tlsId"`
	ListenPort int    `json:"listenPort"`
}

type FleetInboundCreatePlan struct {
	InboundDraft json.RawMessage               `json:"inboundDraft"`
	Targets      []FleetInboundTargetSelection `json:"targets"`
}

type FleetInboundRemoteRequest struct {
	InboundDraft json.RawMessage `json:"inboundDraft"`
}

type FleetInboundRemoteCreated struct {
	ID  uint   `json:"id"`
	Tag string `json:"tag"`
}

type FleetInboundTargetResult struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Tag     string           `json:"tag,omitempty"`
	Success bool             `json:"success"`
	Created bool             `json:"created"`
	Preview *PreflightReport `json:"preview,omitempty"`
	Error   string           `json:"error,omitempty"`
}

type fleetInboundJob struct {
	config      FleetServer
	token       string
	tag         string
	payload     FleetInboundRemoteRequest
	resultIndex int
}

func (s *FleetService) LocalFleetInboundCapabilities(hostname string) (*FleetInboundCapabilities, error) {
	localHost := fleetTemplateHostname(hostname)
	if localHost == "" {
		return nil, common.NewError("无法确定本机面板域名")
	}
	subscriptionHost, err := s.GetSubDomain()
	if err != nil {
		return nil, err
	}
	subscriptionHost = fleetTemplateHostname(subscriptionHost)
	if subscriptionHost == "" {
		subscriptionHost = localHost
	}

	var tlsConfigs []model.Tls
	if err := database.GetDB().Order("name ASC").Find(&tlsConfigs).Error; err != nil {
		return nil, err
	}
	profiles := make([]FleetInboundTLSProfile, 0, len(tlsConfigs))
	for _, tlsConfig := range tlsConfigs {
		profiles = append(profiles, summarizeFleetInboundTLS(tlsConfig))
	}
	var inboundTags []string
	if err := database.GetDB().Model(&model.Inbound{}).Order("tag ASC").Pluck("tag", &inboundTags).Error; err != nil {
		return nil, err
	}
	return &FleetInboundCapabilities{
		Hostname:         localHost,
		SubscriptionHost: subscriptionHost,
		TLSProfiles:      profiles,
		InboundTags:      inboundTags,
	}, nil
}

func summarizeFleetInboundTLS(tlsConfig model.Tls) FleetInboundTLSProfile {
	profile := FleetInboundTLSProfile{ID: tlsConfig.Id, Name: tlsConfig.Name}
	var server map[string]json.RawMessage
	if err := json.Unmarshal(tlsConfig.Server, &server); err != nil {
		return profile
	}
	_ = json.Unmarshal(server["enabled"], &profile.ServerEnabled)
	_ = json.Unmarshal(server["server_name"], &profile.ServerName)
	var acme struct {
		Domain []string `json:"domain"`
	}
	if err := json.Unmarshal(server["acme"], &acme); err == nil {
		profile.ACMEDomains = acme.Domain
	}
	var certificates []string
	if err := json.Unmarshal(server["certificate"], &certificates); err == nil {
		for _, value := range certificates {
			block, _ := pem.Decode([]byte(value))
			if block == nil || block.Type != "CERTIFICATE" {
				continue
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				continue
			}
			fingerprint := sha256.Sum256(certificate.Raw)
			profile.CertificateFingerprint = hex.EncodeToString(fingerprint[:])
			break
		}
	}
	return profile
}

func (s *FleetService) GetFleetInboundTargetInfo(targetIDs []string) []FleetInboundTargetInfo {
	infos := make([]FleetInboundTargetInfo, 0, len(targetIDs))
	configs, err := s.loadFleetServers()
	if err != nil {
		return []FleetInboundTargetInfo{{Error: err.Error()}}
	}
	byID := make(map[string]FleetServer, len(configs))
	for _, config := range configs {
		byID[config.ID] = config
	}
	seen := make(map[string]struct{}, len(targetIDs))
	type pendingTarget struct {
		index  int
		config FleetServer
	}
	pending := make([]pendingTarget, 0, len(targetIDs))
	for _, rawID := range targetIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		info := FleetInboundTargetInfo{ID: id, Name: id}
		config, ok := byID[id]
		switch {
		case id == "local":
			info.Error = "远程创建仅支持服务器集合中的远端服务器"
		case !ok:
			info.Error = "服务器不存在"
		case !config.Enabled:
			info.Name = config.Name
			info.Error = "服务器已停用"
		default:
			info.Name = config.Name
			pending = append(pending, pendingTarget{index: len(infos), config: config})
		}
		infos = append(infos, info)
	}
	var wait sync.WaitGroup
	limit := make(chan struct{}, 8)
	for _, target := range pending {
		wait.Add(1)
		go func(target pendingTarget) {
			defer wait.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			capabilities, fetchErr := s.fetchFleetInboundCapabilities(target.config)
			if fetchErr != nil {
				infos[target.index].Error = fetchErr.Error()
				return
			}
			infos[target.index].Capabilities = capabilities
			infos[target.index].Success = true
		}(target)
	}
	wait.Wait()
	return infos
}

func (s *FleetService) fetchFleetInboundCapabilities(config FleetServer) (*FleetInboundCapabilities, error) {
	token, err := s.decryptFleetToken(config.TokenEnc)
	if err != nil {
		return nil, err
	}
	response, err := s.fetchFleetAPI(config.URL, token, "fleet-inbound-capabilities", "")
	if err != nil {
		return nil, fmt.Errorf("读取目标机证书与域名失败: %w", err)
	}
	raw, err := json.Marshal(response.Obj)
	if err != nil {
		return nil, err
	}
	var capabilities FleetInboundCapabilities
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		return nil, fmt.Errorf("目标机能力响应格式无效: %w", err)
	}
	if capabilities.Hostname == "" || capabilities.SubscriptionHost == "" {
		return nil, errors.New("目标机未返回有效的面板域名或订阅域名")
	}
	return &capabilities, nil
}

func (s *FleetService) PreviewFleetInboundCreate(plan FleetInboundCreatePlan) ([]FleetInboundTargetResult, error) {
	jobs, results, err := s.buildFleetInboundJobs(plan)
	if err != nil {
		return nil, err
	}
	allPassed := s.preflightFleetInboundJobs(jobs, results)
	if !allPassed {
		for index := range results {
			if results[index].Error == "" && results[index].Preview == nil {
				results[index].Success = false
				results[index].Error = "未预检：至少一台目标服务器未通过计划校验"
			}
		}
	}
	return results, nil
}

func (s *FleetService) CreateFleetInbound(plan FleetInboundCreatePlan) ([]FleetInboundTargetResult, error) {
	jobs, results, err := s.buildFleetInboundJobs(plan)
	if err != nil {
		return nil, err
	}

	allPassed := s.preflightFleetInboundJobs(jobs, results)
	if !allPassed {
		for index := range results {
			if results[index].Error == "" {
				results[index].Success = false
				results[index].Error = "未预检：至少一台目标服务器的计划无效，本批次未创建任何入站"
			}
		}
	}
	if !allPassed {
		for _, job := range jobs {
			result := &results[job.resultIndex]
			if result.Error == "" {
				result.Success = false
				result.Error = "未创建：至少一台目标服务器未通过预检，本批次未创建任何入站"
			}
		}
		return results, nil
	}

	failed := false
	for _, job := range jobs {
		result := &results[job.resultIndex]
		if failed {
			result.Success = false
			result.Error = "未执行：此前服务器创建失败"
			continue
		}
		response, createErr := s.fetchFleetJSON(job.config.URL, job.token, "fleet-inbound-create", job.payload)
		if createErr != nil {
			result.Success = false
			result.Error = fmt.Sprintf("创建请求失败，远端可能已保存，请刷新目标机核对后再重试：%v", createErr)
			failed = true
			continue
		}
		var created FleetInboundRemoteCreated
		raw, marshalErr := json.Marshal(response.Obj)
		if marshalErr == nil {
			marshalErr = json.Unmarshal(raw, &created)
		}
		if marshalErr != nil || created.Tag != job.tag || created.ID == 0 {
			result.Success = false
			result.Error = "目标机返回的入站标识不匹配，请刷新目标机核对"
			failed = true
			continue
		}
		result.Success = true
		result.Created = true
	}
	return results, nil
}

func (s *FleetService) preflightFleetInboundJobs(jobs []fleetInboundJob, results []FleetInboundTargetResult) bool {
	allPassed := len(jobs) == len(results)
	var wait sync.WaitGroup
	limit := make(chan struct{}, 8)
	for _, item := range jobs {
		job := item
		wait.Add(1)
		go func() {
			defer wait.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			response, requestErr := s.fetchFleetJSON(job.config.URL, job.token, "fleet-inbound-preflight", job.payload)
			if requestErr != nil {
				results[job.resultIndex].Error = requestErr.Error()
				results[job.resultIndex].Success = false
				return
			}
			preview, decodeErr := decodeFleetInboundPreview(response.Obj)
			if decodeErr != nil {
				results[job.resultIndex].Error = decodeErr.Error()
				results[job.resultIndex].Success = false
				return
			}
			results[job.resultIndex].Preview = preview
			results[job.resultIndex].Success = preview.Valid
			if !preview.Valid {
				results[job.resultIndex].Error = fleetInboundPreflightError(preview)
			}
		}()
	}
	wait.Wait()
	for _, result := range results {
		if !result.Success {
			allPassed = false
		}
	}
	return allPassed
}

func (s *FleetService) buildFleetInboundJobs(plan FleetInboundCreatePlan) ([]fleetInboundJob, []FleetInboundTargetResult, error) {
	if !json.Valid(plan.InboundDraft) {
		return nil, nil, common.NewError("请选择协议并填写有效的全新入站配置")
	}
	var draft model.Inbound
	if err := draft.UnmarshalJSON(plan.InboundDraft); err != nil {
		return nil, nil, fmt.Errorf("解析新入站配置失败: %w", err)
	}
	if !fleetInboundProtocolSupported(draft.Type) || !(&InboundService{}).hasUser(draft.Type) {
		return nil, nil, common.NewErrorf("不支持远程创建 %q 入站", draft.Type)
	}
	if draft.Type == "shadowtls" {
		var options map[string]json.RawMessage
		_ = json.Unmarshal(draft.Options, &options)
		var version int
		_ = json.Unmarshal(options["version"], &version)
		if version != 3 {
			return nil, nil, common.NewError("ShadowTLS 仅支持 v3 用户认证模式，才能绑定目标机用户")
		}
	}
	if len(plan.Targets) == 0 {
		return nil, nil, common.NewError("至少选择一台远端服务器")
	}
	if len(plan.Targets) > 50 {
		return nil, nil, common.NewError("一次最多选择 50 台目标服务器")
	}

	configs, err := s.loadFleetServers()
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]FleetServer, len(configs))
	for _, config := range configs {
		byID[config.ID] = config
	}
	nameCounts := make(map[string]int, len(plan.Targets))
	for _, selection := range plan.Targets {
		if config, ok := byID[strings.TrimSpace(selection.ID)]; ok {
			nameCounts[normalizedFleetServerName(config.Name)]++
		}
	}
	type capabilityResult struct {
		capabilities *FleetInboundCapabilities
		err          error
	}
	capabilityResults := make([]capabilityResult, len(plan.Targets))
	var capabilityWait sync.WaitGroup
	capabilityLimit := make(chan struct{}, 8)
	scheduled := make(map[string]struct{}, len(plan.Targets))
	for index, selection := range plan.Targets {
		id := strings.TrimSpace(selection.ID)
		config, exists := byID[id]
		if id == "" || id == "local" || !exists || !config.Enabled {
			continue
		}
		if _, duplicate := scheduled[id]; duplicate {
			continue
		}
		scheduled[id] = struct{}{}
		capabilityWait.Add(1)
		go func(index int, config FleetServer) {
			defer capabilityWait.Done()
			capabilityLimit <- struct{}{}
			defer func() { <-capabilityLimit }()
			capabilityResults[index].capabilities, capabilityResults[index].err = s.fetchFleetInboundCapabilities(config)
		}(index, config)
	}
	capabilityWait.Wait()
	results := make([]FleetInboundTargetResult, len(plan.Targets))
	jobs := make([]fleetInboundJob, 0, len(plan.Targets))
	seen := make(map[string]struct{}, len(plan.Targets))
	for index, selection := range plan.Targets {
		id := strings.TrimSpace(selection.ID)
		result := FleetInboundTargetResult{ID: id, Name: id}
		results[index] = result
		if id == "" {
			results[index].Error = "目标服务器 ID 不能为空"
			continue
		}
		if _, exists := seen[id]; exists {
			results[index].Error = "目标服务器重复"
			continue
		}
		seen[id] = struct{}{}
		config, ok := byID[id]
		if id == "local" {
			results[index].Error = "远程创建仅支持服务器集合中的远端服务器"
			continue
		}
		if !ok {
			results[index].Error = "服务器不存在"
			continue
		}
		results[index].Name = config.Name
		if !config.Enabled {
			results[index].Error = "服务器已停用"
			continue
		}
		capabilities := capabilityResults[index].capabilities
		if capErr := capabilityResults[index].err; capErr != nil {
			results[index].Error = capErr.Error()
			continue
		}
		if capabilities == nil {
			results[index].Error = "目标服务器未返回 TLS/订阅信息"
			continue
		}

		port := selection.ListenPort
		if port == 0 {
			port = 20000 + common.RandomInt(40000)
		}
		if port < 1 || port > 65535 {
			results[index].Error = fmt.Sprintf("目标端口 %d 超出有效范围 1-65535", port)
			continue
		}
		tlsID := selection.TLSID
		if !inboundSupportsTLSProfile(draft.Type) || draft.Type == "masque" {
			tlsID = 0
		}
		draft.TlsId = 0
		tlsRequired, tlsErr := fleetInboundTLSRequired(&draft)
		if tlsErr != nil {
			results[index].Error = tlsErr.Error()
			continue
		}
		if tlsRequired && tlsID == 0 {
			results[index].Error = "该协议或版本需要 TLS，请为目标服务器选择自己的 TLS 配置"
			continue
		}
		if tlsID > 0 && !fleetInboundTLSProfileEnabled(capabilities.TLSProfiles, tlsID) {
			results[index].Error = "所选 TLS 配置不属于目标服务器或服务端 TLS 未启用，请刷新目标列表后重试"
			continue
		}
		tag := fleetInboundTargetTagForProtocol(config, draft.Type, nameCounts[normalizedFleetServerName(config.Name)] > 1, capabilities.InboundTags)
		payload, payloadErr := buildFleetInboundRemotePayload(plan.InboundDraft, capabilities.SubscriptionHost, tag, port, tlsID)
		if payloadErr != nil {
			results[index].Error = payloadErr.Error()
			continue
		}
		token, tokenErr := s.decryptFleetToken(config.TokenEnc)
		if tokenErr != nil {
			results[index].Error = tokenErr.Error()
			continue
		}
		results[index].Tag = tag
		jobs = append(jobs, fleetInboundJob{config: config, token: token, tag: tag, payload: payload, resultIndex: index})
	}
	return jobs, results, nil
}

func buildFleetInboundRemotePayload(draftJSON json.RawMessage, targetHost, targetTag string, listenPort int, tlsID uint) (FleetInboundRemoteRequest, error) {
	targetHost = fleetTemplateHostname(targetHost)
	if targetHost == "" {
		return FleetInboundRemoteRequest{}, errors.New("目标机订阅域名为空")
	}
	var draft map[string]json.RawMessage
	if err := json.Unmarshal(draftJSON, &draft); err != nil {
		return FleetInboundRemoteRequest{}, fmt.Errorf("解析新入站配置失败: %w", err)
	}
	var inboundType string
	if err := json.Unmarshal(draft["type"], &inboundType); err != nil || !fleetInboundProtocolSupported(inboundType) {
		return FleetInboundRemoteRequest{}, common.NewError("远程新建协议类型无效或不受支持")
	}
	if strings.TrimSpace(targetTag) == "" || listenPort < 1 || listenPort > 65535 {
		return FleetInboundRemoteRequest{}, common.NewError("目标入站名称或监听端口无效")
	}

	data := map[string]json.RawMessage{
		"type":             mustMarshalRaw(inboundType),
		"tag":              mustMarshalRaw(targetTag),
		"listen":           mustMarshalRaw("::"),
		"listen_port":      mustMarshalRaw(listenPort),
		"tls_id":           mustMarshalRaw(tlsID),
		"exclude_from_sub": mustMarshalRaw(false),
		"addrs":            mustMarshalRaw([]map[string]interface{}{{"server": targetHost, "server_port": listenPort}}),
	}
	for _, key := range fleetInboundDraftOptionKeys(inboundType) {
		if value, exists := draft[key]; exists {
			data[key] = append(json.RawMessage(nil), value...)
		}
	}
	if inboundType == "shadowsocks" {
		delete(data, "password")
	}
	if inboundType == "hysteria" {
		var obfs string
		if err := json.Unmarshal(draft["obfs"], &obfs); err == nil && strings.TrimSpace(obfs) != "" {
			data["obfs"] = mustMarshalRaw(fleetGenerateSecretMarker)
		} else {
			delete(data, "obfs")
		}
	}
	if inboundType == "hysteria2" {
		var obfs map[string]json.RawMessage
		if err := json.Unmarshal(draft["obfs"], &obfs); err == nil {
			var obfsType string
			_ = json.Unmarshal(obfs["type"], &obfsType)
			if obfsType == "salamander" {
				data["obfs"] = mustMarshalRaw(map[string]string{"type": obfsType, "password": fleetGenerateSecretMarker})
			} else {
				delete(data, "obfs")
			}
		}
	}
	if inboundType == "masque" {
		data["server"] = mustMarshalRaw(targetHost)
		data["version"] = mustMarshalRaw([]int{3})
		data["system"] = mustMarshalRaw(false)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return FleetInboundRemoteRequest{}, err
	}
	return FleetInboundRemoteRequest{InboundDraft: raw}, nil
}

func fleetInboundDraftOptionKeys(inboundType string) []string {
	common := []string{"network", "tcp_fast_open", "tcp_multi_path", "udp_fragment", "udp_timeout", "detour", "disable_tcp_keep_alive", "tcp_keep_alive", "tcp_keep_alive_interval"}
	protocol := map[string][]string{
		"http":        {"version"},
		"shadowsocks": {"method", "network", "multiplex", "managed", "server_ports", "hop_interval", "hop_interval_max", "disable_chrome_parrot"},
		"vmess":       {"transport", "multiplex"},
		"trojan":      {"transport", "multiplex", "fallback"},
		"naive":       {"quic_congestion_control", "insecure_concurrency", "udp_over_tcp", "extra_headers"},
		"hysteria":    {"up_mbps", "down_mbps", "obfs", "idle_timeout", "keep_alive_period", "stream_receive_window", "connection_receive_window", "max_concurrent_streams", "initial_packet_size", "disable_path_mtu_discovery"},
		"shadowtls":   {"version", "handshake", "handshake_for_server_name", "strict_mode", "wildcard_sni"},
		"tuic":        {"congestion_control", "auth_timeout", "zero_rtt_handshake", "heartbeat"},
		"hysteria2":   {"up_mbps", "down_mbps", "obfs", "ignore_client_bandwidth", "masquerade", "bbr_profile", "idle_timeout", "keep_alive_period", "stream_receive_window", "connection_receive_window", "max_concurrent_streams", "initial_packet_size", "disable_path_mtu_discovery", "brutal_debug"},
		"vless":       {"transport", "multiplex"},
		"anytls":      {"padding_scheme"},
		"mieru":       {"transport", "multiplexing", "handshake_mode", "traffic_pattern", "mtu"},
		"masque":      {"mihomo_compatible", "path", "mtu"},
	}
	return append(common, protocol[inboundType]...)
}

func mustMarshalRaw(value interface{}) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func fleetInboundTargetTagForProtocol(server FleetServer, inboundType string, duplicateName bool, existingTags []string) string {
	serverPart := fleetInboundTagSlug(server.Name)
	if serverPart == "" {
		serverPart = fleetInboundTagSlug(server.ID)
	}
	if duplicateName {
		serverPart += "-" + fleetInboundTagSlug(shortFleetServerID(server.ID))
	}
	protocolPart := inboundType
	if inboundType == "hysteria" {
		protocolPart = "hy"
	} else if inboundType == "hysteria2" {
		protocolPart = "hy2"
	} else if inboundType == "shadowsocks" {
		protocolPart = "ss"
	}
	base := strings.Trim(strings.Join([]string{serverPart, protocolPart}, "-"), "-")
	used := make(map[string]struct{}, len(existingTags))
	for _, tag := range existingTags {
		used[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	if _, exists := used[strings.ToLower(base)]; !exists {
		return base
	}
	for suffix := 2; suffix < 10000; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if _, exists := used[strings.ToLower(candidate)]; !exists {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%s", base, common.Random(6))
}

func fleetInboundTagSlug(value string) string {
	var result strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			result.WriteRune(r)
			lastDash = false
		} else if !lastDash && result.Len() > 0 {
			result.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func normalizedFleetServerName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func shortFleetServerID(id string) string {
	id = strings.TrimSpace(id)
	runes := []rune(id)
	if len(runes) > 6 {
		return string(runes[:6])
	}
	return id
}

func fleetInboundProtocolSupported(inboundType string) bool {
	switch inboundType {
	case "mixed", "socks", "http", "shadowsocks", "vmess", "trojan", "naive", "hysteria", "shadowtls", "tuic", "hysteria2", "vless", "anytls", "mieru", "masque":
		return true
	default:
		return false
	}
}

func inboundSupportsTLSProfile(inboundType string) bool {
	switch inboundType {
	case "http", "vmess", "trojan", "naive", "hysteria", "tuic", "hysteria2", "vless", "anytls":
		return true
	default:
		return false
	}
}

func fleetInboundTLSRequired(inbound *model.Inbound) (bool, error) {
	if inbound == nil {
		return false, errors.New("missing inbound draft")
	}
	switch inbound.Type {
	case "hysteria", "hysteria2", "tuic", "naive", "anytls":
		return true, nil
	case "http":
		version, err := util.SelectedHTTPInboundVersion(inbound.Options)
		if err != nil {
			return false, err
		}
		return version >= 2, nil
	default:
		return false, nil
	}
}

func fleetInboundTLSProfileEnabled(profiles []FleetInboundTLSProfile, id uint) bool {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile.ServerEnabled
		}
	}
	return false
}

func decodeFleetInboundPreview(value interface{}) (*PreflightReport, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var preview PreflightReport
	if err := json.Unmarshal(raw, &preview); err != nil {
		return nil, fmt.Errorf("目标机预检响应格式无效: %w", err)
	}
	return &preview, nil
}

func fleetInboundPreflightError(preview *PreflightReport) string {
	if preview == nil {
		return "目标机预检失败"
	}
	for _, check := range preview.Checks {
		if check.Status == "error" && strings.TrimSpace(check.Detail) != "" {
			return check.Detail
		}
	}
	return "目标机预检未通过"
}

const fleetGenerateSecretMarker = "__generate_per_target__"

func prepareFleetInboundRemotePayloadForTarget(data json.RawMessage, hostname string) (json.RawMessage, string, int, error) {
	if !json.Valid(data) {
		return nil, "", 0, common.NewError("新入站配置不是有效 JSON")
	}
	var request FleetInboundRemoteRequest
	if err := json.Unmarshal(data, &request); err != nil || !json.Valid(request.InboundDraft) {
		return nil, "", 0, common.NewError("远程创建请求缺少有效的新入站配置")
	}
	var inbound model.Inbound
	if err := inbound.UnmarshalJSON(request.InboundDraft); err != nil {
		return nil, "", 0, err
	}
	if !fleetInboundProtocolSupported(inbound.Type) || !(&InboundService{}).hasUser(inbound.Type) {
		return nil, "", 0, common.NewError("目标机不支持该代理入站类型")
	}
	port, err := getInboundListenPort(&inbound)
	if err != nil {
		return nil, "", 0, err
	}
	if strings.TrimSpace(inbound.Tag) == "" {
		return nil, "", 0, common.NewError("目标入站名称不能为空")
	}
	if !inboundSupportsTLSProfile(inbound.Type) || inbound.Type == "masque" {
		inbound.TlsId = 0
	}
	tlsRequired, err := fleetInboundTLSRequired(&inbound)
	if err != nil {
		return nil, "", 0, err
	}
	if tlsRequired && inbound.TlsId == 0 {
		return nil, "", 0, common.NewError("该协议或版本需要 TLS，请选择目标服务器本地且已启用服务端 TLS 的配置")
	}
	if inbound.TlsId > 0 {
		var tlsConfig model.Tls
		if err := database.GetDB().First(&tlsConfig, inbound.TlsId).Error; err != nil {
			return nil, "", 0, common.NewError("所选 TLS 配置不存在于目标服务器")
		}
		if !summarizeFleetInboundTLS(tlsConfig).ServerEnabled {
			return nil, "", 0, common.NewError("目标服务器所选 TLS 配置未启用服务端 TLS")
		}
	}
	if inbound.Type == "shadowtls" {
		var options map[string]json.RawMessage
		_ = json.Unmarshal(inbound.Options, &options)
		var version int
		_ = json.Unmarshal(options["version"], &version)
		if version != 3 {
			return nil, "", 0, common.NewError("ShadowTLS 仅支持 v3 用户认证模式，才能绑定目标机用户")
		}
	}
	capabilities, err := (&FleetService{}).LocalFleetInboundCapabilities(hostname)
	if err != nil {
		return nil, "", 0, fmt.Errorf("读取目标机本地订阅域名失败: %w", err)
	}
	clean, err := buildFleetInboundRemotePayload(request.InboundDraft, capabilities.SubscriptionHost, inbound.Tag, port, inbound.TlsId)
	if err != nil {
		return nil, "", 0, err
	}
	var targetInbound model.Inbound
	if err := targetInbound.UnmarshalJSON(clean.InboundDraft); err != nil {
		return nil, "", 0, err
	}
	if err := prepareFleetInboundTargetSecrets(&targetInbound); err != nil {
		return nil, "", 0, err
	}
	cleanBytes, err := targetInbound.MarshalFull()
	if err != nil {
		return nil, "", 0, err
	}
	raw, err := json.Marshal(*cleanBytes)
	if err != nil {
		return nil, "", 0, err
	}
	var existing model.Inbound
	if err := database.GetDB().Where("tag = ?", targetInbound.Tag).First(&existing).Error; err == nil {
		return nil, "", 0, common.NewErrorf("目标机已存在入站名称 %q", targetInbound.Tag)
	} else if !database.IsNotFound(err) {
		return nil, "", 0, err
	}
	userIDs, enabledUsers, err := fleetLocalInboundUserIDs()
	if err != nil {
		return nil, "", 0, err
	}
	if enabledUsers == 0 {
		return nil, "", 0, common.NewError("目标机没有启用用户；为避免创建无认证监听，请先启用至少一个本机用户")
	}
	return raw, userIDs, enabledUsers, nil
}

func prepareFleetInboundTargetSecrets(inbound *model.Inbound) error {
	if inbound == nil {
		return errors.New("missing target inbound")
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(inbound.Options, &options); err != nil {
		return err
	}
	switch inbound.Type {
	case "shadowsocks":
		var method string
		_ = json.Unmarshal(options["method"], &method)
		if method == "none" {
			return common.NewError("远程多用户 Shadowsocks 不支持 none 加密方式")
		}
		password := common.Random(32)
		if strings.HasPrefix(method, "2022") {
			keyBytes := 32
			if method == "2022-blake3-aes-128-gcm" {
				keyBytes = 16
			}
			key := make([]byte, keyBytes)
			if _, err := rand.Read(key); err != nil {
				return fmt.Errorf("为目标机生成 Shadowsocks 2022 密钥失败: %w", err)
			}
			password = base64.StdEncoding.EncodeToString(key)
		}
		options["password"] = mustMarshalRaw(password)
	case "hysteria":
		var obfs string
		_ = json.Unmarshal(options["obfs"], &obfs)
		if obfs == fleetGenerateSecretMarker {
			options["obfs"] = mustMarshalRaw(common.Random(32))
		}
	case "hysteria2":
		var obfs map[string]json.RawMessage
		if err := json.Unmarshal(options["obfs"], &obfs); err == nil && len(obfs) > 0 {
			var obfsType, password string
			_ = json.Unmarshal(obfs["type"], &obfsType)
			_ = json.Unmarshal(obfs["password"], &password)
			if obfsType == "salamander" && password == fleetGenerateSecretMarker {
				obfs["password"] = mustMarshalRaw(common.Random(32))
				options["obfs"], _ = json.Marshal(obfs)
			}
		}
	}
	inbound.Options, _ = json.Marshal(options)
	return nil
}

func fleetLocalInboundUserIDs() (string, int, error) {
	var clients []model.Client
	if err := database.GetDB().Select("id", "enable").Order("id ASC").Find(&clients).Error; err != nil {
		return "", 0, err
	}
	ids := make([]string, 0, len(clients))
	enabled := 0
	for _, client := range clients {
		ids = append(ids, strconv.FormatUint(uint64(client.Id), 10))
		if client.Enable {
			enabled++
		}
	}
	return strings.Join(ids, ","), enabled, nil
}

func FleetInboundRemotePreflight(data json.RawMessage, hostname string) (*PreflightReport, error) {
	normalized, userIDs, enabledUsers, err := prepareFleetInboundRemotePayloadForTarget(data, hostname)
	if err != nil {
		return nil, err
	}
	report, err := (&ConfigService{}).PreflightSave("inbounds", "new", normalized, userIDs, hostname)
	if err != nil || report == nil || !report.Valid {
		return report, err
	}
	var inbound model.Inbound
	if err := inbound.UnmarshalJSON(normalized); err != nil {
		return nil, err
	}
	report.Checks = append(report.Checks, PreflightCheck{Name: "目标机用户绑定", Status: "ok", Detail: fmt.Sprintf("将绑定目标机全部用户（%d 个启用用户）；未复制 CN2 用户或凭据", enabledUsers)})
	if err := checkFleetInboundListenPort(&inbound); err != nil {
		report.Valid = false
		report.Checks = append(report.Checks, PreflightCheck{Name: "目标机监听端口", Status: "error", Detail: err.Error()})
		return report, nil
	}
	report.Checks = append(report.Checks, PreflightCheck{Name: "目标机监听端口", Status: "ok", Detail: "目标机当前可绑定所需 TCP/UDP 端口"})
	return report, nil
}

func checkFleetInboundListenPort(inbound *model.Inbound) error {
	if inbound == nil {
		return errors.New("missing inbound")
	}
	spec, err := collectInboundForwardSpec(inbound)
	if err != nil {
		return err
	}
	full, err := inbound.MarshalFull()
	if err != nil {
		return err
	}
	listen, _ := (*full)["listen"].(string)
	address := net.JoinHostPort(strings.TrimSpace(listen), strconv.Itoa(spec.listenPort))
	for _, protocol := range spec.protocols {
		switch protocol {
		case "tcp":
			listener, listenErr := net.Listen("tcp", address)
			if listenErr != nil {
				return fmt.Errorf("目标端口 %d/TCP 已占用或无法绑定到 %s: %w", spec.listenPort, address, listenErr)
			}
			if closeErr := listener.Close(); closeErr != nil {
				return fmt.Errorf("释放 TCP 端口预检监听失败: %w", closeErr)
			}
		case "udp":
			packet, listenErr := net.ListenPacket("udp", address)
			if listenErr != nil {
				return fmt.Errorf("目标端口 %d/UDP 已占用或无法绑定到 %s: %w", spec.listenPort, address, listenErr)
			}
			if closeErr := packet.Close(); closeErr != nil {
				return fmt.Errorf("释放 UDP 端口预检监听失败: %w", closeErr)
			}
		default:
			return fmt.Errorf("不支持预检传输协议 %q", protocol)
		}
	}
	return nil
}

func FleetInboundRemoteCreate(data json.RawMessage, username, hostname string) (FleetInboundRemoteCreated, error) {
	normalized, userIDs, _, err := prepareFleetInboundRemotePayloadForTarget(data, hostname)
	if err != nil {
		return FleetInboundRemoteCreated{}, err
	}
	report, err := FleetInboundRemotePreflight(mustMarshalRaw(FleetInboundRemoteRequest{InboundDraft: normalized}), hostname)
	if err != nil || report == nil || !report.Valid {
		if err == nil {
			err = errors.New(fleetInboundPreflightError(report))
		}
		return FleetInboundRemoteCreated{}, fmt.Errorf("目标机预检未通过: %w", err)
	}
	_, changed, err := (&ConfigService{}).Save("inbounds", "new", normalized, userIDs, username, hostname)
	if err != nil {
		return FleetInboundRemoteCreated{}, err
	}
	if !changed {
		return FleetInboundRemoteCreated{}, common.NewError("目标机未创建新入站")
	}
	var request struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(normalized, &request); err != nil || strings.TrimSpace(request.Tag) == "" {
		return FleetInboundRemoteCreated{}, common.NewError("已保存入站，但无法读取返回标签")
	}
	var inbound model.Inbound
	if err := database.GetDB().Select("id").Where("tag = ?", request.Tag).First(&inbound).Error; err != nil {
		return FleetInboundRemoteCreated{}, err
	}
	return FleetInboundRemoteCreated{ID: inbound.Id, Tag: request.Tag}, nil
}
