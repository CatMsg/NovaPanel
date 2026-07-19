package service

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

const fleetSettingKey = "fleetServers"

type FleetServer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	TokenEnc string `json:"tokenEnc"`
	Enabled  bool   `json:"enabled"`
}

type FleetServerInput struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
}

type FleetServerView struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	URL             string                 `json:"url"`
	Enabled         bool                   `json:"enabled"`
	TokenSet        bool                   `json:"tokenSet"`
	Reachable       bool                   `json:"reachable"`
	LatencyMs       int64                  `json:"latencyMs"`
	CheckedAt       time.Time              `json:"checkedAt"`
	Error           string                 `json:"error,omitempty"`
	LastKnown       bool                   `json:"lastKnown,omitempty"`
	LastSuccessAt   string                 `json:"lastSuccessAt,omitempty"`
	System          map[string]interface{} `json:"system,omitempty"`
	Core            map[string]interface{} `json:"core,omitempty"`
	PublicIP        string                 `json:"publicIp,omitempty"`
	Uptime          int64                  `json:"uptime,omitempty"`
	OnlineUsers     int                    `json:"onlineUsers,omitempty"`
	OnlineInbounds  int                    `json:"onlineInbounds,omitempty"`
	OnlineOutbounds int                    `json:"onlineOutbounds,omitempty"`
	Clients         int                    `json:"clients,omitempty"`
	Inbounds        int                    `json:"inbounds,omitempty"`
	Outbounds       int                    `json:"outbounds,omitempty"`
	Endpoints       int                    `json:"endpoints,omitempty"`
	MasqueTotal     int                    `json:"masqueTotal,omitempty"`
	MasqueRunning   int                    `json:"masqueRunning,omitempty"`
	PortBackend     string                 `json:"portBackend,omitempty"`
	Listeners       int                    `json:"listeners,omitempty"`
	NatRules        int                    `json:"natRules,omitempty"`
}

type FleetSnapshot struct {
	Servers   []FleetServerView `json:"servers"`
	CheckedAt time.Time         `json:"checkedAt"`
}

type fleetAPIResponse struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

type FleetService struct {
	SettingService
}

var fleetLastKnown = struct {
	sync.RWMutex
	views map[string]FleetServerView
}{views: make(map[string]FleetServerView)}

func (s *FleetService) GetFleetStatus() map[string]interface{} {
	serverService := &ServerService{}
	status := serverService.GetStatus("sys,sbd,db")
	result := map[string]interface{}{
		"system":   (*status)["sys"],
		"core":     (*status)["sbd"],
		"database": (*status)["db"],
		"publicIp": serverService.GetPublicIP(),
		"ports":    serverService.GetPortStatus(),
	}

	online, err := (&StatsService{}).GetOnlines()
	if err == nil {
		result["online"] = map[string]interface{}{
			"users":     len(online.User),
			"inbounds":  len(online.Inbound),
			"outbounds": len(online.Outbound),
		}
	}
	if masque := GetMasqueService(); masque != nil {
		result["masque"] = masque.GetSummary()
	}
	return result
}

func (s *FleetService) GetFleet() (*FleetSnapshot, error) {
	configs, err := s.loadFleetServers()
	if err != nil {
		return nil, err
	}

	servers := make([]FleetServerView, 0, len(configs)+1)
	servers = append(servers, s.localFleetView())
	remoteViews := make([]FleetServerView, len(configs))
	var waitGroup sync.WaitGroup
	for index, config := range configs {
		if !config.Enabled {
			remoteViews[index] = FleetServerView{
				ID:       config.ID,
				Name:     config.Name,
				URL:      config.URL,
				Enabled:  false,
				TokenSet: config.TokenEnc != "",
				Error:    "已停用",
			}
			continue
		}
		waitGroup.Add(1)
		go func(index int, config FleetServer) {
			defer waitGroup.Done()
			remoteViews[index] = s.fetchFleetServer(config)
		}(index, config)
	}
	waitGroup.Wait()
	servers = append(servers, remoteViews...)

	return &FleetSnapshot{Servers: servers, CheckedAt: time.Now()}, nil
}

func (s *FleetService) GetFleetServer(id string) (*FleetServerView, error) {
	id = strings.TrimSpace(id)
	if id == "local" {
		view := s.localFleetView()
		return &view, nil
	}
	configs, err := s.loadFleetServers()
	if err != nil {
		return nil, err
	}
	for _, config := range configs {
		if config.ID == id {
			view := s.fetchFleetServer(config)
			return &view, nil
		}
	}
	return nil, fmt.Errorf("服务器不存在: %s", id)
}

func (s *FleetService) SaveFleet(inputs []FleetServerInput) error {
	if len(inputs) > 50 {
		return common.NewError("服务器数量不能超过 50 台")
	}

	old, err := s.loadFleetServers()
	if err != nil {
		return err
	}
	oldByID := make(map[string]FleetServer, len(old))
	for _, item := range old {
		oldByID[item.ID] = item
	}

	result := make([]FleetServer, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return common.NewError("服务器名称不能为空")
		}
		baseURL, err := normalizeFleetURL(input.URL)
		if err != nil {
			return err
		}
		id := strings.TrimSpace(input.ID)
		if id == "" {
			id = common.Random(16)
		}
		if _, ok := seen[id]; ok {
			return common.NewError("服务器 ID 重复")
		}
		seen[id] = struct{}{}

		item := FleetServer{ID: id, Name: name, URL: baseURL, Enabled: input.Enabled}
		previous := oldByID[id]
		if strings.TrimSpace(input.Token) != "" {
			item.TokenEnc, err = s.encryptFleetToken(strings.TrimSpace(input.Token))
			if err != nil {
				return err
			}
		} else {
			item.TokenEnc = previous.TokenEnc
		}
		if item.TokenEnc == "" {
			return fmt.Errorf("服务器 %q 尚未填写 API 令牌", name)
		}
		result = append(result, item)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return database.WithRetryTx(5, 100*time.Millisecond, func(tx *gorm.DB) error {
		return tx.Where("key = ?", fleetSettingKey).Assign(model.Setting{Key: fleetSettingKey, Value: string(raw)}).FirstOrCreate(&model.Setting{}).Error
	})
}

func (s *FleetService) loadFleetServers() ([]FleetServer, error) {
	var setting model.Setting
	err := database.GetDB().Where("key = ?", fleetSettingKey).First(&setting).Error
	if database.IsNotFound(err) {
		return []FleetServer{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(setting.Value) == "" {
		return []FleetServer{}, nil
	}
	var result []FleetServer
	if err := json.Unmarshal([]byte(setting.Value), &result); err != nil {
		return nil, fmt.Errorf("服务器集合配置损坏: %w", err)
	}
	return result, nil
}

func (s *FleetService) localFleetView() FleetServerView {
	view := FleetServerView{
		ID:        "local",
		Name:      "本机",
		URL:       "本机",
		Enabled:   true,
		TokenSet:  true,
		Reachable: true,
		LatencyMs: 0,
		CheckedAt: time.Now(),
	}
	s.applyFleetStatus(&view, s.GetFleetStatus())
	return view
}

func (s *FleetService) fetchFleetServer(config FleetServer) FleetServerView {
	view := FleetServerView{
		ID:        config.ID,
		Name:      config.Name,
		URL:       config.URL,
		Enabled:   config.Enabled,
		TokenSet:  config.TokenEnc != "",
		CheckedAt: time.Now(),
	}
	token, err := s.decryptFleetToken(config.TokenEnc)
	if err != nil {
		view.Error = "令牌解密失败"
		return view
	}

	started := time.Now()
	var status fleetAPIResponse
	var statusErr error
	for attempt := 0; attempt < 2; attempt++ {
		status, statusErr = s.fetchFleetAPI(config.URL, token, "fleet-status", "")
		if statusErr == nil {
			break
		}
		if attempt == 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if statusErr != nil {
		view.Error = statusErr.Error()
		view.LatencyMs = time.Since(started).Milliseconds()
		fleetLastKnown.RLock()
		previous, ok := fleetLastKnown.views[config.ID]
		fleetLastKnown.RUnlock()
		if ok {
			previous.ID = config.ID
			previous.Name = config.Name
			previous.URL = config.URL
			previous.Enabled = config.Enabled
			previous.TokenSet = config.TokenEnc != ""
			previous.Reachable = false
			previous.LastKnown = true
			previous.CheckedAt = view.CheckedAt
			previous.LatencyMs = view.LatencyMs
			previous.Error = view.Error
			return previous
		}
		return view
	}
	view.LatencyMs = time.Since(started).Milliseconds()
	view.Reachable = true
	view.LastSuccessAt = time.Now().Format(time.RFC3339)
	if payload, ok := status.Obj.(map[string]interface{}); ok {
		s.applyFleetStatus(&view, payload)
	}
	fleetLastKnown.Lock()
	fleetLastKnown.views[config.ID] = view
	fleetLastKnown.Unlock()
	return view
}

// FleetAction executes a safe operational action for a local or configured
// remote panel. Updates are delegated to the existing detached s-ui updater.
func (s *FleetService) FleetAction(id, action string) (interface{}, error) {
	id = strings.TrimSpace(id)
	action = strings.TrimSpace(action)
	if action != "logs" && action != "restart" && action != "update" && action != "update-status" {
		return nil, fmt.Errorf("不支持的服务器操作: %s", action)
	}
	if id == "local" {
		if action == "logs" {
			return (&ServerService{}).GetLogs("100", "info"), nil
		}
		if action == "update-status" {
			return GetUpdateStatus()
		}
		if action == "update" {
			if err := StartBackgroundUpdate(); err != nil {
				return nil, err
			}
			return map[string]interface{}{"scheduled": true, "state": "queued"}, nil
		}
		if err := (&PanelService{}).RestartPanel(3 * time.Second); err != nil {
			return nil, err
		}
		return map[string]interface{}{"scheduled": true}, nil
	}
	configs, err := s.loadFleetServers()
	if err != nil {
		return nil, err
	}
	for _, config := range configs {
		if config.ID != id {
			continue
		}
		if !config.Enabled {
			return nil, fmt.Errorf("服务器已停用")
		}
		token, err := s.decryptFleetToken(config.TokenEnc)
		if err != nil {
			return nil, fmt.Errorf("令牌解密失败: %w", err)
		}
		if action == "logs" {
			result, err := s.fetchFleetAPI(config.URL, token, "logs", "c=100&l=info")
			return result.Obj, err
		}
		if action == "update-status" {
			result, err := s.fetchFleetAPI(config.URL, token, "update-status", "")
			return result.Obj, err
		}
		form := url.Values{}
		form.Set("id", "local")
		form.Set("action", "update")
		if action == "update" {
			result, err := s.fetchFleetAPIRequest(http.MethodPost, config.URL, token, "fleet-action", "", strings.NewReader(form.Encode()))
			return result.Obj, err
		}
		result, err := s.fetchFleetAPIRequest(http.MethodPost, config.URL, token, "restartApp", "", nil)
		if err != nil {
			return nil, err
		}
		return result.Obj, nil
	}
	return nil, fmt.Errorf("服务器不存在: %s", id)
}

func (s *FleetService) applyFleetStatus(view *FleetServerView, payload map[string]interface{}) {
	if system, ok := payload["system"].(map[string]interface{}); ok {
		view.System = system
	}
	if core, ok := payload["core"].(map[string]interface{}); ok {
		view.Core = core
		if stats, ok := core["stats"].(map[string]interface{}); ok {
			view.Uptime = fleetInt64(stats["Uptime"])
		}
	}
	if publicIP, ok := payload["publicIp"].(string); ok {
		view.PublicIP = publicIP
	}
	applyFleetDatabaseInfo(view, payload["database"])
	if online, ok := payload["online"].(map[string]interface{}); ok {
		view.OnlineUsers = fleetInt(online["users"])
		view.OnlineInbounds = fleetInt(online["inbounds"])
		view.OnlineOutbounds = fleetInt(online["outbounds"])
	}
	if masque, ok := payload["masque"].(map[string]interface{}); ok {
		view.MasqueTotal = fleetInt(masque["total"])
		view.MasqueRunning = fleetInt(masque["running"])
	}
	if ports, ok := payload["ports"].(map[string]interface{}); ok {
		view.PortBackend, _ = ports["backend"].(string)
		view.Listeners = fleetSliceLength(ports["listeners"])
		view.NatRules = fleetSliceLength(ports["nat_ipv4"]) + fleetSliceLength(ports["nat_ipv6"])
	}
}

func applyFleetDatabaseInfo(view *FleetServerView, value interface{}) {
	switch databaseInfo := value.(type) {
	case map[string]interface{}:
		view.Clients = fleetInt(databaseInfo["clients"])
		view.Inbounds = fleetInt(databaseInfo["inbounds"])
		view.Outbounds = fleetInt(databaseInfo["outbounds"])
		view.Endpoints = fleetInt(databaseInfo["endpoints"])
	case map[string]int64:
		view.Clients = int(databaseInfo["clients"])
		view.Inbounds = int(databaseInfo["inbounds"])
		view.Outbounds = int(databaseInfo["outbounds"])
		view.Endpoints = int(databaseInfo["endpoints"])
	}
}

func (s *FleetService) fetchFleetAPI(baseURL, token, action, query string) (fleetAPIResponse, error) {
	return s.fetchFleetAPIRequest(http.MethodGet, baseURL, token, action, query, nil)
}

func (s *FleetService) fetchFleetAPIRequest(method, baseURL, token, action, query string, requestBody io.Reader) (fleetAPIResponse, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/apiv2/" + action
	if query != "" {
		endpoint += "?" + query
	}
	req, err := http.NewRequest(method, endpoint, requestBody)
	if err != nil {
		return fleetAPIResponse{}, err
	}
	req.Header.Set("Token", token)
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fleetAPIResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fleetAPIResponse{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fleetAPIResponse{}, err
	}
	var result fleetAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fleetAPIResponse{}, err
	}
	if !result.Success {
		if result.Msg == "" {
			result.Msg = "远端接口返回失败"
		}
		return fleetAPIResponse{}, errors.New(result.Msg)
	}
	return result, nil
}

func (s *FleetService) encryptFleetToken(token string) (string, error) {
	block, err := s.fleetCipher()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, block.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return "", err
	}
	sealed := block.Seal(nonce, nonce, []byte(token), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *FleetService) decryptFleetToken(value string) (string, error) {
	block, err := s.fleetCipher()
	if err != nil {
		return "", err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	nonceSize := block.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("invalid encrypted token")
	}
	plain, err := block.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *FleetService) fleetCipher() (cipher.AEAD, error) {
	secret, err := s.GetSecret()
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func normalizeFleetURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("服务器地址必须是 http 或 https URL")
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", fmt.Errorf("服务器地址不能包含账号、查询参数或片段")
	}
	return strings.TrimRight(value, "/"), nil
}

func fleetSliceLength(value interface{}) int {
	switch typed := value.(type) {
	case []interface{}:
		return len(typed)
	case []map[string]interface{}:
		return len(typed)
	case []PortListenEntry:
		return len(typed)
	case []PortNatEntry:
		return len(typed)
	default:
		return 0
	}
}

func fleetInt(value interface{}) int {
	return int(fleetInt64(value))
}

func fleetInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case uint32:
		return int64(typed)
	case uint64:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}
