package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

const fleetTemplatesSettingKey = "fleetTemplates"

type FleetTemplateSections struct {
	TLS      bool `json:"tls"`
	Inbounds bool `json:"inbounds"`
	Clients  bool `json:"clients"`
	Route    bool `json:"route"`
	DNS      bool `json:"dns"`
}

type FleetTemplateInbound struct {
	ID      uint            `json:"id"`
	Type    string          `json:"type"`
	Tag     string          `json:"tag"`
	TLSID   uint            `json:"tlsId"`
	Addrs   json.RawMessage `json:"addrs,omitempty"`
	Options json.RawMessage `json:"options,omitempty"`
}

type FleetTemplate struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	SourceHost  string                     `json:"sourceHost,omitempty"`
	CreatedAt   time.Time                  `json:"createdAt"`
	Sections    FleetTemplateSections      `json:"sections"`
	TLS         []model.Tls                `json:"tls,omitempty"`
	Inbounds    []FleetTemplateInbound     `json:"inbounds,omitempty"`
	TLSRefs     map[uint]string            `json:"tlsRefs,omitempty"`
	InboundRefs map[uint]string            `json:"inboundRefs,omitempty"`
	Clients     []model.Client             `json:"clients,omitempty"`
	Config      map[string]json.RawMessage `json:"config,omitempty"`
}

type FleetTemplateSummary struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	CreatedAt time.Time             `json:"createdAt"`
	Sections  FleetTemplateSections `json:"sections"`
}

type FleetTemplatePreview struct {
	TLSAdd        int      `json:"tlsAdd"`
	TLSUpdate     int      `json:"tlsUpdate"`
	InboundAdd    int      `json:"inboundAdd"`
	InboundUpdate int      `json:"inboundUpdate"`
	ClientAdd     int      `json:"clientAdd"`
	ClientUpdate  int      `json:"clientUpdate"`
	ConfigUpdates []string `json:"configUpdates"`
}

type FleetTemplateTargetResult struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Success bool                  `json:"success"`
	Preview *FleetTemplatePreview `json:"preview,omitempty"`
	Error   string                `json:"error,omitempty"`
}

func (s *FleetService) GetFleetTemplates() ([]FleetTemplate, error) {
	var setting model.Setting
	err := database.GetDB().Where("key = ?", fleetTemplatesSettingKey).First(&setting).Error
	if database.IsNotFound(err) {
		return []FleetTemplate{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(setting.Value) == "" {
		return []FleetTemplate{}, nil
	}
	var templates []FleetTemplate
	if err := json.Unmarshal([]byte(setting.Value), &templates); err != nil {
		return nil, fmt.Errorf("配置模板数据损坏: %w", err)
	}
	return templates, nil
}

func (s *FleetService) GetFleetTemplateSummaries() ([]FleetTemplateSummary, error) {
	templates, err := s.GetFleetTemplates()
	if err != nil {
		return nil, err
	}
	summaries := make([]FleetTemplateSummary, 0, len(templates))
	for _, template := range templates {
		summaries = append(summaries, template.Summary())
	}
	return summaries, nil
}

func (t FleetTemplate) Summary() FleetTemplateSummary {
	return FleetTemplateSummary{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt,
		Sections:  t.Sections,
	}
}

func (s *FleetService) CaptureFleetTemplate(name string, sections FleetTemplateSections, hostname string) (*FleetTemplate, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, common.NewError("模板名称不能为空")
	}
	if !sections.TLS && !sections.Inbounds && !sections.Clients && !sections.Route && !sections.DNS {
		return nil, common.NewError("至少选择一项配置")
	}

	template := FleetTemplate{
		ID:          common.Random(16),
		Name:        name,
		SourceHost:  fleetTemplateHostname(hostname),
		CreatedAt:   time.Now(),
		Sections:    sections,
		Config:      make(map[string]json.RawMessage),
		TLSRefs:     make(map[uint]string),
		InboundRefs: make(map[uint]string),
	}
	db := database.GetDB()
	if sections.TLS {
		if err := db.Find(&template.TLS).Error; err != nil {
			return nil, err
		}
	}
	if sections.Inbounds {
		var inbounds []model.Inbound
		if err := db.Find(&inbounds).Error; err != nil {
			return nil, err
		}
		for _, inbound := range inbounds {
			template.InboundRefs[inbound.Id] = inbound.Tag
			template.Inbounds = append(template.Inbounds, FleetTemplateInbound{
				ID: inbound.Id, Type: inbound.Type, Tag: inbound.Tag, TLSID: inbound.TlsId,
				Addrs: append(json.RawMessage(nil), inbound.Addrs...), Options: append(json.RawMessage(nil), inbound.Options...),
			})
		}
	}
	if sections.Clients {
		if len(template.InboundRefs) == 0 {
			var inbounds []model.Inbound
			if err := db.Select("id", "tag").Find(&inbounds).Error; err != nil {
				return nil, err
			}
			for _, inbound := range inbounds {
				template.InboundRefs[inbound.Id] = inbound.Tag
			}
		}
		if err := db.Find(&template.Clients).Error; err != nil {
			return nil, err
		}
		for index := range template.Clients {
			template.Clients[index].Up = 0
			template.Clients[index].Down = 0
			template.Clients[index].TotalUp = 0
			template.Clients[index].TotalDown = 0
			template.Clients[index].History = nil
		}
	}
	if sections.Inbounds {
		var tlsRows []model.Tls
		if err := db.Select("id", "name").Find(&tlsRows).Error; err != nil {
			return nil, err
		}
		for _, tls := range tlsRows {
			template.TLSRefs[tls.Id] = tls.Name
		}
	}
	if sections.Route || sections.DNS {
		configValue, err := s.GetConfig()
		if err != nil {
			return nil, err
		}
		var configMap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(configValue), &configMap); err != nil {
			return nil, err
		}
		if sections.Route {
			template.Config["route"] = append(json.RawMessage(nil), configMap["route"]...)
		}
		if sections.DNS {
			template.Config["dns"] = append(json.RawMessage(nil), configMap["dns"]...)
		}
	}

	templates, err := s.GetFleetTemplates()
	if err != nil {
		return nil, err
	}
	templates = append(templates, template)
	if err := s.saveFleetTemplates(templates); err != nil {
		return nil, err
	}
	return &template, nil
}

func (s *FleetService) DeleteFleetTemplate(id string) error {
	templates, err := s.GetFleetTemplates()
	if err != nil {
		return err
	}
	filtered := templates[:0]
	found := false
	for _, template := range templates {
		if template.ID == strings.TrimSpace(id) {
			found = true
			continue
		}
		filtered = append(filtered, template)
	}
	if !found {
		return common.NewError("配置模板不存在")
	}
	return s.saveFleetTemplates(filtered)
}

func (s *FleetService) saveFleetTemplates(templates []FleetTemplate) error {
	raw, err := json.Marshal(templates)
	if err != nil {
		return err
	}
	return database.WithRetryTx(5, 100*time.Millisecond, func(tx *gorm.DB) error {
		return tx.Where("key = ?", fleetTemplatesSettingKey).
			Assign(model.Setting{Key: fleetTemplatesSettingKey, Value: string(raw)}).
			FirstOrCreate(&model.Setting{}).Error
	})
}

func (s *FleetService) FindFleetTemplate(id string) (*FleetTemplate, error) {
	templates, err := s.GetFleetTemplates()
	if err != nil {
		return nil, err
	}
	for index := range templates {
		if templates[index].ID == strings.TrimSpace(id) {
			return &templates[index], nil
		}
	}
	return nil, common.NewError("配置模板不存在")
}

func (s *FleetService) PreviewFleetTemplateTargets(templateID string, targetIDs []string) ([]FleetTemplateTargetResult, error) {
	template, err := s.FindFleetTemplate(templateID)
	if err != nil {
		return nil, err
	}
	return s.runFleetTemplateTargets(*template, targetIDs, "", true)
}

func (s *FleetService) DeployFleetTemplate(templateID string, targetIDs []string, canaryID string) ([]FleetTemplateTargetResult, error) {
	template, err := s.FindFleetTemplate(templateID)
	if err != nil {
		return nil, err
	}
	return s.runFleetTemplateTargets(*template, targetIDs, canaryID, false)
}

func (s *FleetService) runFleetTemplateTargets(template FleetTemplate, targetIDs []string, canaryID string, previewOnly bool) ([]FleetTemplateTargetResult, error) {
	ordered := orderFleetTemplateTargets(targetIDs, canaryID)
	if len(ordered) == 0 {
		return nil, common.NewError("至少选择一台目标服务器")
	}
	configs, err := s.loadFleetServers()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]FleetServer, len(configs))
	for _, config := range configs {
		byID[config.ID] = config
	}

	results := make([]FleetTemplateTargetResult, 0, len(ordered))
	for _, id := range ordered {
		result := FleetTemplateTargetResult{ID: id, Name: id}
		if id == "local" {
			result.Name = "本机"
			if previewOnly {
				result.Preview, err = (&ConfigService{}).PreviewFleetTemplate(template, template.SourceHost)
			} else {
				err = (&ConfigService{}).ApplyFleetTemplate(template, template.SourceHost)
			}
		} else {
			config, ok := byID[id]
			if !ok {
				err = common.NewError("服务器不存在")
			} else if !config.Enabled {
				err = common.NewError("服务器已停用")
			} else {
				result.Name = config.Name
				var token string
				token, err = s.decryptFleetToken(config.TokenEnc)
				if err == nil {
					action := "fleet-template-apply"
					if previewOnly {
						action = "fleet-template-preview"
					}
					var response fleetAPIResponse
					response, err = s.fetchFleetJSON(config.URL, token, action, template)
					if err == nil && previewOnly {
						raw, marshalErr := json.Marshal(response.Obj)
						if marshalErr != nil {
							err = marshalErr
						} else {
							result.Preview = &FleetTemplatePreview{}
							err = json.Unmarshal(raw, result.Preview)
						}
					}
				}
			}
		}
		result.Success = err == nil
		if err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)
		if err != nil && !previewOnly {
			break
		}
	}
	return results, nil
}

func (s *FleetService) fetchFleetJSON(baseURL, token, action string, payload interface{}) (fleetAPIResponse, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fleetAPIResponse{}, err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/apiv2/" + action
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return fleetAPIResponse{}, err
	}
	req.Header.Set("Token", token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fleetAPIResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fleetAPIResponse{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result fleetAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fleetAPIResponse{}, err
	}
	if !result.Success {
		return fleetAPIResponse{}, errors.New(result.Msg)
	}
	return result, nil
}

func orderFleetTemplateTargets(targetIDs []string, canaryID string) []string {
	seen := make(map[string]struct{}, len(targetIDs))
	remote := make([]string, 0, len(targetIDs))
	local := false
	for _, raw := range targetIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if id == "local" {
			local = true
		} else {
			remote = append(remote, id)
		}
	}
	ordered := make([]string, 0, len(seen))
	canaryID = strings.TrimSpace(canaryID)
	if canaryID != "" && canaryID != "local" {
		for index, id := range remote {
			if id == canaryID {
				ordered = append(ordered, id)
				remote = append(remote[:index], remote[index+1:]...)
				break
			}
		}
	}
	ordered = append(ordered, remote...)
	if local {
		ordered = append(ordered, "local")
	}
	return ordered
}

func (s *ConfigService) PreviewFleetTemplate(template FleetTemplate, hostname string) (*FleetTemplatePreview, error) {
	preview := &FleetTemplatePreview{}
	db := database.GetDB()
	for _, item := range template.TLS {
		var count int64
		if err := db.Model(&model.Tls{}).Where("name = ?", item.Name).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			preview.TLSAdd++
		} else {
			preview.TLSUpdate++
		}
	}
	for _, item := range template.Inbounds {
		var count int64
		if err := db.Model(&model.Inbound{}).Where("tag = ?", item.Tag).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			preview.InboundAdd++
		} else {
			preview.InboundUpdate++
		}
	}
	for _, item := range template.Clients {
		var count int64
		if err := db.Model(&model.Client{}).Where("name = ?", item.Name).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			preview.ClientAdd++
		} else {
			preview.ClientUpdate++
		}
	}
	for key := range template.Config {
		preview.ConfigUpdates = append(preview.ConfigUpdates, key)
	}
	tx := database.GetDB().Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer tx.Rollback()
	if err := s.applyFleetTemplateTx(tx, template, fleetTemplateHostname(hostname)); err != nil {
		return nil, err
	}
	return preview, nil
}

func (s *ConfigService) ApplyFleetTemplate(template FleetTemplate, hostname string) error {
	saveConfigMu.Lock()
	defer saveConfigMu.Unlock()

	hostname = fleetTemplateHostname(hostname)
	var snapshot *configSnapshot
	err := retryWriteTx(func(tx *gorm.DB) error {
		var err error
		snapshot, err = captureConfigSnapshot(tx, true)
		if err != nil {
			return err
		}
		return s.applyFleetTemplateTx(tx, template, hostname)
	})
	if err != nil {
		return err
	}

	applyErr := s.SettingService.RebuildAllManagedPortForwarding(&s.InboundService, &s.EndpointService)
	if applyErr == nil && masquePtr != nil {
		applyErr = masquePtr.SyncFromDB()
	}
	if applyErr == nil && mieruPtr != nil {
		applyErr = mieruPtr.SyncFromDB()
	}
	if applyErr == nil && corePtr != nil {
		if corePtr.IsRunning() {
			applyErr = s.RestartCore()
		} else if !IsTrafficBudgetBlocked() {
			applyErr = s.StartCore()
		}
	}
	if applyErr != nil {
		rollbackErr := s.compensateFailedSave(snapshot, "config", 0)
		if rollbackErr != nil {
			return errors.Join(applyErr, common.NewErrorf("模板应用失败且恢复旧配置失败: %v", rollbackErr))
		}
		return common.NewErrorf("模板应用失败，目标服务器已恢复旧配置: %v", applyErr)
	}
	markDataUpdated()
	return nil
}

func (s *ConfigService) applyFleetTemplateTx(tx *gorm.DB, template FleetTemplate, hostname string) error {
	tlsIDs := make(map[uint]uint)
	for _, source := range template.TLS {
		var target model.Tls
		err := tx.Where("name = ?", source.Name).First(&target).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		oldID := source.Id
		if database.IsNotFound(err) {
			target = model.Tls{Name: source.Name}
		}
		target.Server = replaceFleetTemplateHost(source.Server, template.SourceHost, hostname)
		target.Client = replaceFleetTemplateHost(source.Client, template.SourceHost, hostname)
		if err := tx.Save(&target).Error; err != nil {
			return err
		}
		tlsIDs[oldID] = target.Id
	}

	inboundIDs := make(map[uint]uint)
	for _, source := range template.Inbounds {
		var target model.Inbound
		err := tx.Where("tag = ?", source.Tag).First(&target).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		if database.IsNotFound(err) {
			target = model.Inbound{Tag: source.Tag}
		}
		target.Type = source.Type
		target.Addrs = replaceFleetTemplateHost(source.Addrs, template.SourceHost, hostname)
		target.Options = replaceFleetTemplateHost(source.Options, template.SourceHost, hostname)
		if mapped, ok := tlsIDs[source.TLSID]; ok {
			target.TlsId = mapped
		} else if source.TLSID == 0 {
			target.TlsId = 0
		} else if tlsName := template.TLSRefs[source.TLSID]; tlsName != "" {
			var existingTLS model.Tls
			err := tx.Select("id").Where("name = ?", tlsName).First(&existingTLS).Error
			if database.IsNotFound(err) {
				return common.NewErrorf("入站 %s 需要目标机已有 TLS %s，或在模板中同时勾选 TLS", source.Tag, tlsName)
			}
			if err != nil {
				return err
			}
			target.TlsId = existingTLS.Id
		}
		if err := normalizeInboundCompatibility(&target); err != nil {
			return err
		}
		if target.Type == "mieru" {
			if err := removeMieruLegacyPortRange(&target); err != nil {
				return err
			}
			if _, err := parseMieruInbound(&target); err != nil {
				return err
			}
		}
		if err := util.FillOutJson(&target, hostname); err != nil {
			return err
		}
		if spec, err := collectInboundForwardSpec(&target); err != nil {
			return err
		} else {
			if err := validateInboundPortRangesAgainstSSHProtocols(&target, spec.portRanges, spec.protocols); err != nil {
				return err
			}
			if err := validateManagedPortRangeProtocolConflicts(tx, "入站", target.Tag, target.Id, 0, spec.portRanges, spec.protocols); err != nil {
				return err
			}
		}
		if err := tx.Save(&target).Error; err != nil {
			return err
		}
		if err := syncManagedPortEntriesForInboundTx(tx, &target); err != nil {
			return err
		}
		inboundIDs[source.ID] = target.Id
	}

	for _, source := range template.Clients {
		var target model.Client
		err := tx.Where("name = ?", source.Name).First(&target).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		isNew := database.IsNotFound(err)
		if isNew {
			target = source
			target.Id = 0
		} else {
			target.Enable = source.Enable
			target.Config = source.Config
			target.Volume = source.Volume
			target.Expiry = source.Expiry
			target.UploadLimit = source.UploadLimit
			target.DownloadLimit = source.DownloadLimit
			target.Desc = source.Desc
			target.Group = source.Group
			target.DelayStart = source.DelayStart
			target.AutoReset = source.AutoReset
			target.ResetDays = source.ResetDays
			target.NextReset = source.NextReset
		}
		var sourceInboundIDs []uint
		if len(source.Inbounds) > 0 {
			if err := json.Unmarshal(source.Inbounds, &sourceInboundIDs); err != nil {
				return err
			}
		}
		mappedIDs := make([]uint, 0, len(sourceInboundIDs))
		for _, sourceID := range sourceInboundIDs {
			if targetID, ok := inboundIDs[sourceID]; ok {
				mappedIDs = append(mappedIDs, targetID)
				continue
			}
			if tag := template.InboundRefs[sourceID]; tag != "" {
				var existingInbound model.Inbound
				err := tx.Select("id").Where("tag = ?", tag).First(&existingInbound).Error
				if database.IsNotFound(err) {
					return common.NewErrorf("用户 %s 需要目标机已有入站 %s，或在模板中同时勾选入站", source.Name, tag)
				}
				if err != nil {
					return err
				}
				mappedIDs = append(mappedIDs, existingInbound.Id)
			}
		}
		target.Inbounds, _ = json.Marshal(mappedIDs)
		if err := s.ClientService.updateLinksWithFixedInbounds(tx, []*model.Client{&target}, hostname); err != nil {
			return err
		}
		if err := tx.Save(&target).Error; err != nil {
			return err
		}
	}

	if len(template.Config) > 0 {
		current, err := s.SettingService.getStringTx(tx, "config")
		if err != nil {
			return err
		}
		var configMap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(current), &configMap); err != nil {
			return err
		}
		for key, value := range template.Config {
			configMap[key] = replaceFleetTemplateHost(value, template.SourceHost, hostname)
		}
		raw, err := json.Marshal(configMap)
		if err != nil {
			return err
		}
		if err := s.SettingService.SaveConfig(tx, raw); err != nil {
			return err
		}
	}

	rawConfig, err := s.getConfig(tx, "")
	if err != nil {
		return err
	}
	if err := validateRuntimeConfig(*rawConfig); err != nil {
		return common.NewErrorf("模板生成的 Sing-Box 配置无效: %v", err)
	}
	return nil
}

func replaceFleetTemplateHost(value json.RawMessage, sourceHost, targetHost string) json.RawMessage {
	if len(value) == 0 || sourceHost == "" || targetHost == "" || sourceHost == targetHost {
		return append(json.RawMessage(nil), value...)
	}
	return json.RawMessage(strings.ReplaceAll(string(value), sourceHost, targetHost))
}

func fleetTemplateHostname(host string) string {
	host = strings.TrimSpace(host)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(parsed, "[]")
	}
	return strings.Trim(host, "[]")
}
