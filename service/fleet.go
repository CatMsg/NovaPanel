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
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	URL         string                 `json:"url"`
	Enabled     bool                   `json:"enabled"`
	TokenSet    bool                   `json:"tokenSet"`
	Reachable   bool                   `json:"reachable"`
	LatencyMs   int64                  `json:"latencyMs"`
	CheckedAt   time.Time              `json:"checkedAt"`
	Error       string                 `json:"error,omitempty"`
	System      map[string]interface{} `json:"system,omitempty"`
	Core        map[string]interface{} `json:"core,omitempty"`
	PortBackend string                 `json:"portBackend,omitempty"`
	Listeners   int                    `json:"listeners,omitempty"`
	NatRules    int                    `json:"natRules,omitempty"`
}

type FleetSnapshot struct {
	Servers   []FleetServerView `json:"servers"`
	CheckedAt time.Time         `json:"checkedAt"`
}

type fleetAPIResponse struct {
	Success bool                   `json:"success"`
	Msg     string                 `json:"msg"`
	Obj     map[string]interface{} `json:"obj"`
}

type FleetService struct {
	SettingService
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
	serverService := &ServerService{}
	system := serverService.GetSystemInfo()
	core := map[string]interface{}{"running": false}
	if corePtr != nil {
		core = serverService.GetSingboxInfo()
	}
	ports := serverService.GetPortStatus()
	return FleetServerView{
		ID:          "local",
		Name:        "本机",
		URL:         "本机",
		Enabled:     true,
		TokenSet:    true,
		Reachable:   true,
		LatencyMs:   0,
		CheckedAt:   time.Now(),
		System:      system,
		Core:        core,
		PortBackend: fmt.Sprint(ports["backend"]),
		Listeners:   fleetSliceLength(ports["listeners"]),
		NatRules:    fleetSliceLength(ports["nat_ipv4"]) + fleetSliceLength(ports["nat_ipv6"]),
	}
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
	status, statusErr := s.fetchFleetAPI(config.URL, token, "status", "r=sys,sbd")
	if statusErr != nil {
		view.Error = statusErr.Error()
		view.LatencyMs = time.Since(started).Milliseconds()
		return view
	}
	view.LatencyMs = time.Since(started).Milliseconds()
	view.Reachable = true
	if system, ok := status.Obj["sys"].(map[string]interface{}); ok {
		view.System = system
	}
	if core, ok := status.Obj["sbd"].(map[string]interface{}); ok {
		view.Core = core
	}

	ports, portsErr := s.fetchFleetAPI(config.URL, token, "ports", "")
	if portsErr == nil {
		if backend, ok := ports.Obj["backend"].(string); ok {
			view.PortBackend = backend
		}
		view.Listeners = fleetSliceLength(ports.Obj["listeners"])
		view.NatRules = fleetSliceLength(ports.Obj["nat_ipv4"]) + fleetSliceLength(ports.Obj["nat_ipv6"])
	} else {
		view.Error = "状态正常，端口信息获取失败: " + portsErr.Error()
	}
	return view
}

func (s *FleetService) fetchFleetAPI(baseURL, token, action, query string) (fleetAPIResponse, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/apiv2/" + action
	if query != "" {
		endpoint += "?" + query
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fleetAPIResponse{}, err
	}
	req.Header.Set("Token", token)
	req.Header.Set("Accept", "application/json")
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
