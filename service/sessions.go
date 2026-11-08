package service

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
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
	ID            string    `json:"id"`
	ServerID      string    `json:"serverId,omitempty"`
	ServerName    string    `json:"serverName,omitempty"`
	Kind          string    `json:"kind"`
	Inbound       string    `json:"inbound,omitempty"`
	Outbound      string    `json:"outbound,omitempty"`
	DNSResolver   string    `json:"dnsResolver,omitempty"`
	User          string    `json:"user,omitempty"`
	UserDesc      string    `json:"userDesc,omitempty"`
	Network       string    `json:"network"`
	Source        string    `json:"source,omitempty"`
	SourceIP      string    `json:"sourceIp,omitempty"`
	SourceIPs     []string  `json:"sourceIps,omitempty"`
	SourceIPScope string    `json:"sourceIpScope,omitempty"`
	Destination   string    `json:"destination,omitempty"`
	Domain        string    `json:"domain,omitempty"`
	Protocol      string    `json:"protocol,omitempty"`
	Rule          string    `json:"rule,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	Upload        uint64    `json:"upload"`
	Download      uint64    `json:"download"`
}

type FleetSessions struct {
	Sessions  []SessionView     `json:"sessions"`
	Errors    map[string]string `json:"errors,omitempty"`
	CheckedAt time.Time         `json:"checkedAt"`
}

type sessionDNSResolverCacheEntry struct {
	resolver  string
	expiresAt time.Time
}

var sessionDNSResolverCache = struct {
	sync.Mutex
	entries map[string]sessionDNSResolverCacheEntry
}{entries: make(map[string]sessionDNSResolverCacheEntry)}

func GetLocalSessions() []SessionView {
	result := make([]SessionView, 0)
	mieru := GetMieruService()
	if corePtr != nil && corePtr.IsRunning() {
		box := corePtr.GetInstance()
		if box != nil && box.ConnTracker() != nil {
			for _, session := range box.ConnTracker().Sessions() {
				view := SessionView{
					ID: "core:" + session.ID, Kind: "sing-box", Inbound: session.Inbound,
					Outbound: session.Outbound, User: session.User, Network: session.Network,
					Source: session.Source, SourceIP: sourceIPFromSessionSource(session.Source),
					Destination: session.Destination, Domain: session.Domain,
					Protocol: session.Protocol, Rule: session.Rule, StartedAt: session.StartedAt,
					Upload: session.Upload, Download: session.Download,
				}
				applyMieruSourceIPs(&view, mieru)
				result = append(result, view)
			}
		}
	}
	if masque := GetMasqueService(); masque != nil {
		result = append(result, masque.sessionViews()...)
	}
	applySessionDNSResolvers(result)
	applySessionUserDescriptions(result)
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAt.After(result[j].StartedAt) })
	return result
}

func applySessionDNSResolvers(sessions []SessionView) {
	for index := range sessions {
		session := &sessions[index]
		if strings.TrimSpace(session.Domain) == "" {
			continue
		}
		base := core.RouteExplainInput{
			Domain:   session.Domain,
			Inbound:  session.Inbound,
			User:     session.User,
			Protocol: "dns",
		}
		aInput := base
		aInput.QueryType = "A"
		aaaaInput := base
		aaaaInput.QueryType = "AAAA"
		session.DNSResolver = combineSessionDNSResolvers(
			cachedSessionDNSResolver(aInput),
			cachedSessionDNSResolver(aaaaInput),
		)
	}
}

func cachedSessionDNSResolver(input core.RouteExplainInput) string {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.Domain), "."))
	key := strings.Join([]string{
		domain,
		strings.TrimSpace(input.Inbound),
		strings.TrimSpace(input.User),
		strings.ToUpper(strings.TrimSpace(input.QueryType)),
	}, "\x00")
	now := time.Now()
	sessionDNSResolverCache.Lock()
	if entry, ok := sessionDNSResolverCache.entries[key]; ok && now.Before(entry.expiresAt) {
		sessionDNSResolverCache.Unlock()
		return entry.resolver
	}
	sessionDNSResolverCache.Unlock()

	resolver := ""
	startCoreMu.Lock()
	if corePtr != nil && corePtr.IsRunning() {
		if result, err := core.ExplainDNS(input); err == nil && result != nil {
			resolver = result.Server
		}
	}
	startCoreMu.Unlock()

	sessionDNSResolverCache.Lock()
	for existingKey, entry := range sessionDNSResolverCache.entries {
		if !now.Before(entry.expiresAt) {
			delete(sessionDNSResolverCache.entries, existingKey)
		}
	}
	if len(sessionDNSResolverCache.entries) >= 512 {
		sessionDNSResolverCache.entries = make(map[string]sessionDNSResolverCacheEntry)
	}
	sessionDNSResolverCache.entries[key] = sessionDNSResolverCacheEntry{
		resolver: resolver, expiresAt: now.Add(10 * time.Second),
	}
	sessionDNSResolverCache.Unlock()
	return resolver
}

func combineSessionDNSResolvers(ipv4, ipv6 string) string {
	if ipv4 == "" {
		return ipv6
	}
	if ipv6 == "" || ipv4 == ipv6 {
		return ipv4
	}
	return fmt.Sprintf("A: %s · AAAA: %s", ipv4, ipv6)
}

func applySessionUserDescriptions(sessions []SessionView) {
	if len(sessions) == 0 {
		return
	}
	db := database.GetDB()
	if db == nil {
		return
	}

	userNames := make([]string, 0, len(sessions))
	seenNames := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		if strings.TrimSpace(session.User) == "" {
			continue
		}
		if _, seen := seenNames[session.User]; seen {
			continue
		}
		seenNames[session.User] = struct{}{}
		userNames = append(userNames, session.User)
	}
	if len(userNames) == 0 {
		return
	}

	var clients []model.Client
	if err := db.Select("name", "desc", "enable", "id").Where("name IN ?", userNames).Order("enable DESC, id ASC").Find(&clients).Error; err != nil {
		return
	}
	applySessionUserDescriptionsFromClients(sessions, clients)
}

func applySessionUserDescriptionsFromClients(sessions []SessionView, clients []model.Client) {
	userDescriptions := make(map[string]string, len(clients))
	for _, client := range clients {
		if _, exists := userDescriptions[client.Name]; !exists {
			userDescriptions[client.Name] = client.Desc
		}
	}
	for index := range sessions {
		sessions[index].UserDesc = userDescriptions[sessions[index].User]
	}
}

func applyMieruSourceIPs(view *SessionView, mieru *MieruService) {
	if view == nil || mieru == nil || view.SourceIP != "" {
		return
	}
	view.SourceIPs = mieru.SourceIPsForUser(view.Inbound, view.User)
	if len(view.SourceIPs) == 0 {
		return
	}
	view.SourceIPScope = "user"
	if len(view.SourceIPs) == 1 {
		view.SourceIP = view.SourceIPs[0]
	}
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

func (s *MasqueService) sessionViews() []SessionView { return nil }

func (s *MasqueService) closeSession(value string) bool { return false }

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
