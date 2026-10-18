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
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

const fleetTemplatesSettingKey = "fleetTemplates"
const fleetTemplateFormatVersion = 1

type FleetTemplateSections struct {
	TLS       bool `json:"tls"`
	Inbounds  bool `json:"inbounds"`
	Outbounds bool `json:"outbounds"`
	Clients   bool `json:"clients"`
	Route     bool `json:"route"`
	DNS       bool `json:"dns"`
}

type FleetTemplateInbound struct {
	ID             uint            `json:"id"`
	Type           string          `json:"type"`
	Tag            string          `json:"tag"`
	TLSID          uint            `json:"tlsId"`
	ExcludeFromSub bool            `json:"excludeFromSub"`
	Addrs          json.RawMessage `json:"addrs,omitempty"`
	Options        json.RawMessage `json:"options,omitempty"`
}

type FleetTemplateOutbound struct {
	Type    string          `json:"type"`
	Tag     string          `json:"tag"`
	Options json.RawMessage `json:"options,omitempty"`
}

type FleetTemplate struct {
	FormatVersion int                        `json:"formatVersion"`
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	SourceHost    string                     `json:"sourceHost,omitempty"`
	CreatedAt     time.Time                  `json:"createdAt"`
	Sections      FleetTemplateSections      `json:"sections"`
	TLS           []model.Tls                `json:"tls,omitempty"`
	Inbounds      []FleetTemplateInbound     `json:"inbounds,omitempty"`
	Outbounds     []FleetTemplateOutbound    `json:"outbounds,omitempty"`
	TLSRefs       map[uint]string            `json:"tlsRefs,omitempty"`
	InboundRefs   map[uint]string            `json:"inboundRefs,omitempty"`
	Clients       []model.Client             `json:"clients,omitempty"`
	Config        map[string]json.RawMessage `json:"config,omitempty"`
}

type FleetTemplateSummary struct {
	ID                    string                `json:"id"`
	Name                  string                `json:"name"`
	CreatedAt             time.Time             `json:"createdAt"`
	Sections              FleetTemplateSections `json:"sections"`
	UnsafeServerLocalData bool                  `json:"unsafeServerLocalData"`
}

type FleetTemplatePreview struct {
	TLSAdd            int      `json:"tlsAdd"`
	TLSUpdate         int      `json:"tlsUpdate"`
	InboundAdd        int      `json:"inboundAdd"`
	InboundUpdate     int      `json:"inboundUpdate"`
	InboundAddTags    []string `json:"inboundAddTags,omitempty"`
	InboundUpdateTags []string `json:"inboundUpdateTags,omitempty"`
	OutboundAdd       int      `json:"outboundAdd"`
	OutboundUpdate    int      `json:"outboundUpdate"`
	ClientAdd         int      `json:"clientAdd"`
	ClientUpdate      int      `json:"clientUpdate"`
	ConfigUpdates     []string `json:"configUpdates"`
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
	for _, template := range templates {
		if err := validateFleetTemplateFormat(template); err != nil {
			return nil, err
		}
	}
	return templates, nil
}

func validateFleetTemplateFormat(template FleetTemplate) error {
	if template.FormatVersion != fleetTemplateFormatVersion {
		return common.NewErrorf("配置模板格式不受支持（需要格式版本 %d）", fleetTemplateFormatVersion)
	}
	return nil
}

func validateFleetTemplateNoServerLocalCopy(template FleetTemplate) error {
	if template.Sections.Inbounds || len(template.Inbounds) > 0 ||
		template.Sections.Clients || len(template.Clients) > 0 ||
		template.Sections.TLS || len(template.TLS) > 0 ||
		len(template.InboundRefs) > 0 || len(template.TLSRefs) > 0 {
		return common.NewError("配置模板不能复制来源服务器的入站、用户或 TLS 身份数据；请新建仅包含出站、路由或 DNS 的模板。入站请使用“远程新建入站”，并在目标服务器使用本地用户和 TLS 证书")
	}
	return nil
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
		ID:                    t.ID,
		Name:                  t.Name,
		CreatedAt:             t.CreatedAt,
		Sections:              t.Sections,
		UnsafeServerLocalData: t.hasServerLocalData(),
	}
}

func (t FleetTemplate) hasServerLocalData() bool {
	return t.Sections.Inbounds || t.Sections.Clients || t.Sections.TLS ||
		len(t.Inbounds) > 0 || len(t.Clients) > 0 || len(t.TLS) > 0 ||
		len(t.InboundRefs) > 0 || len(t.TLSRefs) > 0
}

func (s *FleetService) CaptureFleetTemplate(name string, sections FleetTemplateSections, hostname string) (*FleetTemplate, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, common.NewError("模板名称不能为空")
	}
	if sections.Inbounds || sections.Clients || sections.TLS {
		return nil, common.NewError("配置模板仅支持出站、路由和 DNS；入站、用户及 TLS 证书必须在目标服务器本地创建或维护")
	}
	if !sections.Outbounds && !sections.Route && !sections.DNS {
		return nil, common.NewError("至少选择一项配置")
	}

	template := FleetTemplate{
		FormatVersion: fleetTemplateFormatVersion,
		ID:            common.Random(16),
		Name:          name,
		SourceHost:    fleetTemplateHostname(hostname),
		CreatedAt:     time.Now(),
		Sections:      sections,
		Config:        make(map[string]json.RawMessage),
	}
	db := database.GetDB()
	if sections.Outbounds {
		var outbounds []model.Outbound
		if err := db.Find(&outbounds).Error; err != nil {
			return nil, err
		}
		for _, outbound := range outbounds {
			template.Outbounds = append(template.Outbounds, FleetTemplateOutbound{
				Type: outbound.Type, Tag: outbound.Tag, Options: append(json.RawMessage(nil), outbound.Options...),
			})
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
	if err := validateFleetTemplateNoServerLocalCopy(*template); err != nil {
		return nil, err
	}
	return s.runFleetTemplateTargets(*template, targetIDs, "", true)
}

func (s *FleetService) DeployFleetTemplate(templateID string, targetIDs []string, canaryID string) ([]FleetTemplateTargetResult, error) {
	template, err := s.FindFleetTemplate(templateID)
	if err != nil {
		return nil, err
	}
	if err := validateFleetTemplateNoServerLocalCopy(*template); err != nil {
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
							if err == nil {
								err = validateFleetTemplatePreviewSupport(template, result.Preview)
							}
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

func validateFleetTemplatePreviewSupport(template FleetTemplate, preview *FleetTemplatePreview) error {
	if len(template.Outbounds) > 0 && preview.OutboundAdd+preview.OutboundUpdate != len(template.Outbounds) {
		return common.NewError("目标服务器版本不支持出站模板，请先更新目标服务器")
	}
	return nil
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
	if err := validateFleetTemplateFormat(template); err != nil {
		return nil, err
	}
	if err := validateFleetTemplateNoServerLocalCopy(template); err != nil {
		return nil, err
	}
	preview := &FleetTemplatePreview{}
	db := database.GetDB()
	if err := validateFleetTemplateOutbounds(template.Outbounds); err != nil {
		return nil, err
	}
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
			preview.InboundAddTags = append(preview.InboundAddTags, item.Tag)
		} else {
			preview.InboundUpdate++
			preview.InboundUpdateTags = append(preview.InboundUpdateTags, item.Tag)
		}
	}
	for _, item := range template.Outbounds {
		var count int64
		if err := db.Model(&model.Outbound{}).Where("tag = ?", item.Tag).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			preview.OutboundAdd++
		} else {
			preview.OutboundUpdate++
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
	if err := validateFleetTemplateNoServerLocalCopy(template); err != nil {
		return err
	}
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
		if err := s.applyFleetTemplateTx(tx, template, hostname); err != nil {
			return err
		}
		return snapshot.captureAfterImage(tx)
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
	if err := validateFleetTemplateFormat(template); err != nil {
		return err
	}
	if err := validateFleetTemplateNoServerLocalCopy(template); err != nil {
		return err
	}
	if err := validateFleetTemplateOutbounds(template.Outbounds); err != nil {
		return err
	}
	for _, source := range template.TLS {
		var target model.Tls
		err := tx.Where("name = ?", source.Name).First(&target).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		if database.IsNotFound(err) {
			target = model.Tls{Name: source.Name}
		}
		target.Server = replaceFleetTemplateHost(source.Server, template.SourceHost, hostname)
		target.Client = replaceFleetTemplateHost(source.Client, template.SourceHost, hostname)
		if err := tx.Save(&target).Error; err != nil {
			return err
		}
	}

	for _, source := range template.Outbounds {
		var target model.Outbound
		err := tx.Where("tag = ?", source.Tag).First(&target).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		if database.IsNotFound(err) {
			target = model.Outbound{Tag: source.Tag}
		}
		target.Type = source.Type
		target.Options = append(json.RawMessage(nil), source.Options...)
		if err := tx.Save(&target).Error; err != nil {
			return err
		}
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
			if tag := template.InboundRefs[sourceID]; tag != "" {
				var existingInbound model.Inbound
				err := tx.Select("id").Where("tag = ?", tag).First(&existingInbound).Error
				if database.IsNotFound(err) {
					return common.NewErrorf("用户 %s 需要目标机预先存在入站 %s；配置模板不会创建入站", source.Name, tag)
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
		if err := s.SettingService.SaveConfig(tx, raw); err != nil && !errors.Is(err, ErrNoChanges) {
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

func validateFleetTemplateOutbounds(outbounds []FleetTemplateOutbound) error {
	seen := make(map[string]struct{}, len(outbounds))
	for _, outbound := range outbounds {
		if strings.TrimSpace(outbound.Tag) == "" || strings.TrimSpace(outbound.Type) == "" {
			return common.NewError("出站模板的标签和类型不能为空")
		}
		if _, exists := seen[outbound.Tag]; exists {
			return common.NewErrorf("出站模板标签重复: %s", outbound.Tag)
		}
		seen[outbound.Tag] = struct{}{}
		if len(outbound.Options) == 0 {
			continue
		}
		var options map[string]json.RawMessage
		if err := json.Unmarshal(outbound.Options, &options); err != nil || options == nil {
			return common.NewErrorf("出站 %s 的参数不是 JSON 对象", outbound.Tag)
		}
		for _, key := range []string{"id", "type", "tag"} {
			if _, exists := options[key]; exists {
				return common.NewErrorf("出站 %s 的参数不能覆盖 %s", outbound.Tag, key)
			}
		}
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
