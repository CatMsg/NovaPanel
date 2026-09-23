package service

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func remarshalFleetObject(value interface{}, target interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func sourceIPFromSessionSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(source); err == nil {
		if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
			return ip.Unmap().String()
		}
	}
	if ip, err := netip.ParseAddr(strings.Trim(source, "[]")); err == nil {
		return ip.Unmap().String()
	}
	return ""
}

// SessionView is the protocol-neutral view used by the local and fleet APIs.
// It is generated from live trackers and is never persisted.
type SessionView struct {
	ID          string    `json:"id"`
	ServerID    string    `json:"serverId,omitempty"`
	ServerName  string    `json:"serverName,omitempty"`
	Kind        string    `json:"kind"`
	Inbound     string    `json:"inbound,omitempty"`
	Outbound    string    `json:"outbound,omitempty"`
	User        string    `json:"user,omitempty"`
	Network     string    `json:"network"`
	Source      string    `json:"source,omitempty"`
	SourceIP    string    `json:"sourceIp,omitempty"`
	Destination string    `json:"destination,omitempty"`
	Domain      string    `json:"domain,omitempty"`
	Protocol    string    `json:"protocol,omitempty"`
	Rule        string    `json:"rule,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	Upload      uint64    `json:"upload"`
	Download    uint64    `json:"download"`
}

type FleetSessions struct {
	Sessions  []SessionView     `json:"sessions"`
	Errors    map[string]string `json:"errors,omitempty"`
	CheckedAt time.Time         `json:"checkedAt"`
}

func GetLocalSessions() []SessionView {
	result := make([]SessionView, 0)
	if corePtr != nil && corePtr.IsRunning() {
		box := corePtr.GetInstance()
		if box != nil && box.ConnTracker() != nil {
			for _, session := range box.ConnTracker().Sessions() {
				result = append(result, SessionView{
					ID: "core:" + session.ID, Kind: "sing-box", Inbound: session.Inbound,
					Outbound: session.Outbound, User: session.User, Network: session.Network,
					Source: session.Source, SourceIP: sourceIPFromSessionSource(session.Source),
					Destination: session.Destination, Domain: session.Domain,
					Protocol: session.Protocol, Rule: session.Rule, StartedAt: session.StartedAt,
					Upload: session.Upload, Download: session.Download,
				})
			}
		}
	}
	if masque := GetMasqueService(); masque != nil {
		result = append(result, masque.sessionViews()...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAt.After(result[j].StartedAt) })
	return result
}

func CloseLocalSession(id string) error {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "core:") {
		if corePtr == nil || !corePtr.IsRunning() || corePtr.GetInstance() == nil || corePtr.GetInstance().ConnTracker() == nil {
			return fmt.Errorf("sing-box 未运行")
		}
		if !corePtr.GetInstance().ConnTracker().CloseSession(strings.TrimPrefix(id, "core:")) {
			return fmt.Errorf("连接已结束或不存在")
		}
		return nil
	}
	if strings.HasPrefix(id, "masque:") {
		if masque := GetMasqueService(); masque != nil && masque.closeSession(strings.TrimPrefix(id, "masque:")) {
			return nil
		}
		return fmt.Errorf("MASQUE 会话已结束或不存在")
	}
	return fmt.Errorf("无效的连接 ID")
}

func (s *MasqueService) sessionViews() []SessionView {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	runtimes := make([]*masqueRuntime, 0, len(s.runtimes))
	for _, runtime := range s.runtimes {
		runtimes = append(runtimes, runtime)
	}
	s.mu.Unlock()

	result := make([]SessionView, 0)
	for _, runtime := range runtimes {
		runtime.sessionMu.Lock()
		for _, session := range runtime.sessions {
			result = append(result, SessionView{
				ID:   "masque:" + runtime.tag + ":" + strconv.FormatUint(session.id, 10),
				Kind: "masque", Inbound: runtime.tag, User: session.identity.Name,
				Network: "ip", Source: session.remote, SourceIP: sourceIPFromSessionSource(session.remote), Destination: "CONNECT-IP",
				Protocol: "http3-connect-ip", StartedAt: session.startedAt,
				Upload: session.rxBytes.Load(), Download: session.txBytes.Load(),
			})
		}
		runtime.sessionMu.Unlock()
	}
	return result
}

func (s *MasqueService) closeSession(value string) bool {
	separator := strings.LastIndex(value, ":")
	if separator <= 0 || separator == len(value)-1 {
		return false
	}
	tag := value[:separator]
	id, err := strconv.ParseUint(value[separator+1:], 10, 64)
	if err != nil {
		return false
	}
	s.mu.Lock()
	runtime := s.runtimes[tag]
	s.mu.Unlock()
	if runtime == nil {
		return false
	}
	runtime.sessionMu.Lock()
	session := runtime.sessions[id]
	runtime.sessionMu.Unlock()
	if session == nil {
		return false
	}
	session.close()
	return true
}

func (s *FleetService) GetFleetSessions() (*FleetSessions, error) {
	configs, err := s.loadFleetServers()
	if err != nil {
		return nil, err
	}
	result := &FleetSessions{Sessions: GetLocalSessions(), Errors: make(map[string]string), CheckedAt: time.Now()}
	for index := range result.Sessions {
		result.Sessions[index].ServerID = "local"
		result.Sessions[index].ServerName = "本机"
	}

	var mutex sync.Mutex
	var waitGroup sync.WaitGroup
	for _, config := range configs {
		if !config.Enabled {
			continue
		}
		waitGroup.Add(1)
		go func(config FleetServer) {
			defer waitGroup.Done()
			token, decryptErr := s.decryptFleetToken(config.TokenEnc)
			if decryptErr != nil {
				mutex.Lock()
				result.Errors[config.Name] = "令牌解密失败"
				mutex.Unlock()
				return
			}
			response, fetchErr := s.fetchFleetAPI(config.URL, token, "sessions", "")
			if fetchErr != nil {
				mutex.Lock()
				result.Errors[config.Name] = fetchErr.Error()
				mutex.Unlock()
				return
			}
			var sessions []SessionView
			if decodeErr := remarshalFleetObject(response.Obj, &sessions); decodeErr != nil {
				mutex.Lock()
				result.Errors[config.Name] = decodeErr.Error()
				mutex.Unlock()
				return
			}
			for index := range sessions {
				sessions[index].ServerID = config.ID
				sessions[index].ServerName = config.Name
				if sessions[index].SourceIP == "" {
					sessions[index].SourceIP = sourceIPFromSessionSource(sessions[index].Source)
				}
			}
			mutex.Lock()
			result.Sessions = append(result.Sessions, sessions...)
			mutex.Unlock()
		}(config)
	}
	waitGroup.Wait()
	sort.Slice(result.Sessions, func(i, j int) bool { return result.Sessions[i].StartedAt.After(result.Sessions[j].StartedAt) })
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

func (s *FleetService) CloseFleetSession(serverID, sessionID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" || serverID == "local" {
		return CloseLocalSession(sessionID)
	}
	configs, err := s.loadFleetServers()
	if err != nil {
		return err
	}
	for _, config := range configs {
		if config.ID != serverID {
			continue
		}
		token, err := s.decryptFleetToken(config.TokenEnc)
		if err != nil {
			return fmt.Errorf("令牌解密失败: %w", err)
		}
		form := url.Values{}
		form.Set("id", sessionID)
		_, err = s.fetchFleetAPIRequest(http.MethodPost, config.URL, token, "session-close", "", strings.NewReader(form.Encode()))
		return err
	}
	return fmt.Errorf("服务器不存在: %s", serverID)
}
