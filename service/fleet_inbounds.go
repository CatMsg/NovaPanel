package service

import (
	"crypto/sha256"
	"crypto/x509"
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
	ServerName             string   `json:"serverName,omitempty"`
	ACMEDomains            []string `json:"acmeDomains,omitempty"`
	CertificateFingerprint string   `json:"certificateFingerprint,omitempty"`
}

type FleetInboundCapabilities struct {
	Hostname         string                   `json:"hostname"`
	SubscriptionHost string                   `json:"subscriptionHost"`
	TLSProfiles      []FleetInboundTLSProfile `json:"tlsProfiles"`
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
	SourceInboundID uint                          `json:"sourceInboundId"`
	BaseTag         string                        `json:"baseTag"`
	Targets         []FleetInboundTargetSelection `json:"targets"`
}

type FleetInboundRemoteRequest struct {
	Inbound json.RawMessage `json:"inbound"`
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
	return &FleetInboundCapabilities{
		Hostname:         localHost,
		SubscriptionHost: subscriptionHost,
		TLSProfiles:      profiles,
	}, nil
}

func summarizeFleetInboundTLS(tlsConfig model.Tls) FleetInboundTLSProfile {
	profile := FleetInboundTLSProfile{ID: tlsConfig.Id, Name: tlsConfig.Name}
	var server map[string]json.RawMessage
	if err := json.Unmarshal(tlsConfig.Server, &server); err != nil {
		return profile
	}
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
	if plan.SourceInboundID == 0 {
		return nil, nil, common.NewError("请选择一个本机入站作为协议配置原型")
	}
	plan.BaseTag = strings.TrimSpace(plan.BaseTag)
	if plan.BaseTag == "" {
		return nil, nil, common.NewError("入站基础名称不能为空")
	}
	if len(plan.Targets) == 0 {
		return nil, nil, common.NewError("至少选择一台远端服务器")
	}
	if len(plan.Targets) > 50 {
		return nil, nil, common.NewError("一次最多选择 50 台目标服务器")
	}

	var source model.Inbound
	if err := database.GetDB().Preload("Tls").First(&source, plan.SourceInboundID).Error; err != nil {
		return nil, nil, fmt.Errorf("读取本机入站原型失败: %w", err)
	}
	if !(&InboundService{}).hasUser(source.Type) {
		return nil, nil, common.NewErrorf("入站类型 %s 不能作为代理入站原型", source.Type)
	}
	sourcePort, err := getInboundListenPort(&source)
	if err != nil {
		return nil, nil, err
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
	fingerprintTargets := make(map[string][]int)
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
			port = sourcePort
		}
		if port < 1 || port > 65535 {
			results[index].Error = fmt.Sprintf("目标端口 %d 超出有效范围 1-65535", port)
			continue
		}
		tlsID := selection.TLSID
		if !inboundSupportsTLSProfile(source.Type) || source.Type == "masque" {
			tlsID = 0
		}
		tlsRequired, tlsErr := fleetInboundTLSRequired(&source)
		if tlsErr != nil {
			results[index].Error = tlsErr.Error()
			continue
		}
		if source.TlsId > 0 && inboundSupportsTLSProfile(source.Type) {
			tlsRequired = true
		}
		if tlsRequired && tlsID == 0 {
			results[index].Error = "该协议或版本需要 TLS，请为目标服务器选择自己的 TLS 配置"
			continue
		}
		if tlsID > 0 && !fleetInboundTLSProfileExists(capabilities.TLSProfiles, tlsID) {
			results[index].Error = "所选 TLS 配置不属于目标服务器，请刷新目标列表后重试"
			continue
		}
		tag := fleetInboundTargetTagForServer(plan.BaseTag, config, nameCounts[normalizedFleetServerName(config.Name)] > 1)
		payload, payloadErr := buildFleetInboundRemotePayload(&source, capabilities.SubscriptionHost, tag, port, tlsID)
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
		if fingerprint := fleetInboundTLSFingerprint(capabilities.TLSProfiles, tlsID); fingerprint != "" {
			fingerprintTargets[fingerprint] = append(fingerprintTargets[fingerprint], index)
		}
	}
	invalidFingerprintTargets := make(map[int]struct{})
	for _, indices := range fingerprintTargets {
		if len(indices) < 2 {
			continue
		}
		for _, index := range indices {
			results[index].Error = "所选 TLS 证书与其他目标服务器相同；请为每台服务器配置独立证书"
			results[index].Success = false
			invalidFingerprintTargets[index] = struct{}{}
		}
	}
	if len(invalidFingerprintTargets) > 0 {
		filtered := jobs[:0]
		for _, job := range jobs {
			if _, invalid := invalidFingerprintTargets[job.resultIndex]; !invalid {
				filtered = append(filtered, job)
			}
		}
		jobs = filtered
	}
	return jobs, results, nil
}

func buildFleetInboundRemotePayload(source *model.Inbound, targetHost, targetTag string, listenPort int, tlsID uint) (FleetInboundRemoteRequest, error) {
	if source == nil {
		return FleetInboundRemoteRequest{}, errors.New("missing inbound prototype")
	}
	targetHost = fleetTemplateHostname(targetHost)
	if targetHost == "" {
		return FleetInboundRemoteRequest{}, errors.New("目标机订阅域名为空")
	}
	full, err := source.MarshalFull()
	if err != nil {
		return FleetInboundRemoteRequest{}, err
	}
	data := *full
	delete(data, "id")
	delete(data, "tls")
	data["type"] = source.Type
	data["tag"] = targetTag
	data["tls_id"] = tlsID
	data["out_json"] = map[string]interface{}{}
	if _, exists := data["listen_port"]; exists {
		data["listen_port"] = listenPort
	}
	addrs, err := buildFleetInboundAddresses(source.Addrs, targetHost, listenPort)
	if err != nil {
		return FleetInboundRemoteRequest{}, err
	}
	data["addrs"] = addrs
	if source.Type == "masque" {
		data["server"] = targetHost
		delete(data, "address")
		data["tls_id"] = 0
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return FleetInboundRemoteRequest{}, err
	}
	return FleetInboundRemoteRequest{Inbound: raw}, nil
}

func buildFleetInboundAddresses(raw json.RawMessage, targetHost string, targetPort int) ([]map[string]interface{}, error) {
	var addresses []map[string]interface{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &addresses); err != nil {
			return nil, fmt.Errorf("解析原型入站地址失败: %w", err)
		}
	}
	if len(addresses) == 0 {
		return []map[string]interface{}{{"server": targetHost, "server_port": targetPort}}, nil
	}
	for _, address := range addresses {
		address["server"] = targetHost
		address["server_port"] = targetPort
		delete(address, "tls")
	}
	return addresses, nil
}

func fleetInboundTargetTag(baseTag, serverName string) string {
	baseTag = strings.TrimSpace(baseTag)
	serverName = strings.Join(strings.Fields(strings.TrimSpace(serverName)), " ")
	if baseTag == "" || serverName == "" {
		return ""
	}
	return fmt.Sprintf("%s [%s]", baseTag, serverName)
}

func fleetInboundTargetTagForServer(baseTag string, server FleetServer, duplicateName bool) string {
	tag := fleetInboundTargetTag(baseTag, server.Name)
	if duplicateName {
		tag += " {" + shortFleetServerID(server.ID) + "}"
	}
	return tag
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
		return false, errors.New("missing inbound prototype")
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

func fleetInboundTLSProfileExists(profiles []FleetInboundTLSProfile, id uint) bool {
	for _, profile := range profiles {
		if profile.ID == id {
			return true
		}
	}
	return false
}

func fleetInboundTLSFingerprint(profiles []FleetInboundTLSProfile, id uint) string {
	if id == 0 {
		return ""
	}
	for _, profile := range profiles {
		if profile.ID == id {
			return strings.ToLower(strings.TrimSpace(profile.CertificateFingerprint))
		}
	}
	return ""
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

func FleetInboundRemotePreflight(data json.RawMessage, hostname string) (*PreflightReport, error) {
	if !json.Valid(data) {
		return nil, common.NewError("入站配置不是有效 JSON")
	}
	report, err := (&ConfigService{}).PreflightSave("inbounds", "new", data, "", hostname)
	if err != nil || report == nil || !report.Valid {
		return report, err
	}
	var inbound model.Inbound
	if err := inbound.UnmarshalJSON(data); err != nil {
		return nil, err
	}
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
	if !json.Valid(data) {
		return FleetInboundRemoteCreated{}, common.NewError("入站配置不是有效 JSON")
	}
	report, err := FleetInboundRemotePreflight(data, hostname)
	if err != nil || report == nil || !report.Valid {
		if err == nil {
			err = errors.New(fleetInboundPreflightError(report))
		}
		return FleetInboundRemoteCreated{}, fmt.Errorf("目标机预检未通过: %w", err)
	}
	_, changed, err := (&ConfigService{}).Save("inbounds", "new", data, "", username, hostname)
	if err != nil {
		return FleetInboundRemoteCreated{}, err
	}
	if !changed {
		return FleetInboundRemoteCreated{}, common.NewError("目标机未创建新入站")
	}
	var request struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(data, &request); err != nil || strings.TrimSpace(request.Tag) == "" {
		return FleetInboundRemoteCreated{}, common.NewError("已保存入站，但无法读取返回标签")
	}
	var inbound model.Inbound
	if err := database.GetDB().Select("id").Where("tag = ?", request.Tag).First(&inbound).Error; err != nil {
		return FleetInboundRemoteCreated{}, err
	}
	return FleetInboundRemoteCreated{ID: inbound.Id, Tag: request.Tag}, nil
}
