package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

const defaultFailoverTestURL = "https://www.gstatic.com/generate_204"

type FailoverPolicy struct {
	Tag               string   `json:"tag"`
	Members           []string `json:"members"`
	TestURL           string   `json:"testUrl"`
	IntervalSeconds   int      `json:"intervalSeconds"`
	FailureThreshold  int      `json:"failureThreshold"`
	RecoveryThreshold int      `json:"recoveryThreshold"`
	Enabled           bool     `json:"enabled"`
}

type FailoverProbe struct {
	Tag   string `json:"tag"`
	OK    bool   `json:"ok"`
	Delay uint16 `json:"delay"`
	Error string `json:"error,omitempty"`
}

type FailoverStatus struct {
	Policy      FailoverPolicy  `json:"policy"`
	Current     string          `json:"current"`
	Candidate   string          `json:"candidate,omitempty"`
	Pending     int             `json:"pending"`
	LastChecked string          `json:"lastChecked,omitempty"`
	LastSwitch  string          `json:"lastSwitch,omitempty"`
	Error       string          `json:"error,omitempty"`
	Probes      []FailoverProbe `json:"probes"`
}

type FailoverService struct {
	config *ConfigService

	mu         sync.RWMutex
	statuses   map[string]FailoverStatus
	pendingFor map[string]string
	lastRun    map[string]time.Time
	cancel     chan struct{}
	done       chan struct{}
	runMu      sync.Mutex
}

var failoverPtr *FailoverService

func NewFailoverService(configService *ConfigService) *FailoverService {
	service := &FailoverService{
		config:     configService,
		statuses:   make(map[string]FailoverStatus),
		pendingFor: make(map[string]string),
		lastRun:    make(map[string]time.Time),
	}
	failoverPtr = service
	return service
}

func (s *FailoverService) Start() {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	s.cancel = make(chan struct{})
	s.done = make(chan struct{})
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-cancel:
				return
			}
		}
	}()
}

func (s *FailoverService) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	close(cancel)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func (s *FailoverService) GetPolicies() ([]FailoverPolicy, error) {
	raw, err := (&SettingService{}).getString("outboundFailover")
	if err != nil {
		return nil, err
	}
	var policies []FailoverPolicy
	if err := json.Unmarshal([]byte(raw), &policies); err != nil {
		return nil, err
	}
	if policies == nil {
		policies = []FailoverPolicy{}
	}
	return policies, nil
}

func (s *FailoverService) SavePolicy(policy FailoverPolicy) error {
	policy = normalizeFailoverPolicy(policy)
	if err := validateFailoverPolicy(policy); err != nil {
		return err
	}
	if err := validateSelectorPolicy(policy); err != nil {
		return err
	}
	policies, err := s.GetPolicies()
	if err != nil {
		return err
	}
	replaced := false
	for index := range policies {
		if policies[index].Tag == policy.Tag {
			policies[index] = policy
			replaced = true
			break
		}
	}
	if !replaced {
		policies = append(policies, policy)
	}
	if err := saveFailoverPolicies(policies); err != nil {
		return err
	}
	markDataUpdated()
	go s.runOnce()
	return nil
}

func (s *FailoverService) DeletePolicy(tag string) error {
	tag = strings.TrimSpace(tag)
	policies, err := s.GetPolicies()
	if err != nil {
		return err
	}
	filtered := policies[:0]
	found := false
	for _, policy := range policies {
		if policy.Tag == tag {
			found = true
			continue
		}
		filtered = append(filtered, policy)
	}
	if !found {
		return nil
	}
	if err := saveFailoverPolicies(filtered); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.statuses, tag)
	delete(s.pendingFor, tag)
	delete(s.lastRun, tag)
	s.mu.Unlock()
	markDataUpdated()
	return nil
}

func (s *FailoverService) Statuses() ([]FailoverStatus, error) {
	policies, err := s.GetPolicies()
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]FailoverStatus, 0, len(policies))
	for _, policy := range policies {
		status, exists := s.statuses[policy.Tag]
		if !exists {
			status = FailoverStatus{Policy: policy, Probes: []FailoverProbe{}}
		}
		status.Policy = policy
		result = append(result, status)
	}
	return result, nil
}

func normalizeFailoverPolicy(policy FailoverPolicy) FailoverPolicy {
	policy.Tag = strings.TrimSpace(policy.Tag)
	policy.TestURL = strings.TrimSpace(policy.TestURL)
	if policy.TestURL == "" {
		policy.TestURL = defaultFailoverTestURL
	}
	members := make([]string, 0, len(policy.Members))
	for _, member := range policy.Members {
		members = append(members, strings.TrimSpace(member))
	}
	policy.Members = uniqueStrings(members)
	if policy.IntervalSeconds == 0 {
		policy.IntervalSeconds = 30
	}
	if policy.FailureThreshold == 0 {
		policy.FailureThreshold = 2
	}
	if policy.RecoveryThreshold == 0 {
		policy.RecoveryThreshold = 2
	}
	return policy
}

func validateFailoverPolicy(policy FailoverPolicy) error {
	if policy.Tag == "" {
		return errors.New("策略组标签不能为空")
	}
	if len(policy.Members) < 2 {
		return errors.New("有序回退至少需要两个成员")
	}
	probeURL, err := url.ParseRequestURI(policy.TestURL)
	if err != nil || (probeURL.Scheme != "http" && probeURL.Scheme != "https") || probeURL.Host == "" {
		return errors.New("探测地址必须是有效的 HTTP 或 HTTPS URL")
	}
	if policy.IntervalSeconds < 10 || policy.IntervalSeconds > 3600 {
		return errors.New("探测间隔必须在 10 到 3600 秒之间")
	}
	if policy.FailureThreshold < 1 || policy.FailureThreshold > 10 || policy.RecoveryThreshold < 1 || policy.RecoveryThreshold > 10 {
		return errors.New("失败与恢复阈值必须在 1 到 10 之间")
	}
	return nil
}

func validateSelectorPolicy(policy FailoverPolicy) error {
	var outbound model.Outbound
	if err := database.GetDB().Where("tag = ? AND type = ?", policy.Tag, "selector").First(&outbound).Error; err != nil {
		return fmt.Errorf("找不到 Selector 策略组 %s: %w", policy.Tag, err)
	}
	var options struct {
		Outbounds []string `json:"outbounds"`
	}
	if err := json.Unmarshal(outbound.Options, &options); err != nil {
		return err
	}
	for _, member := range policy.Members {
		if !slices.Contains(options.Outbounds, member) {
			return fmt.Errorf("%s 不是策略组 %s 的成员", member, policy.Tag)
		}
	}
	return nil
}

func saveFailoverPolicies(policies []FailoverPolicy) error {
	raw, err := json.Marshal(policies)
	if err != nil {
		return err
	}
	return (&SettingService{}).setString("outboundFailover", string(raw))
}

func (s *FailoverService) runOnce() {
	if !s.runMu.TryLock() {
		return
	}
	defer s.runMu.Unlock()
	if s.config == nil || corePtr == nil || !corePtr.IsRunning() {
		return
	}
	policies, err := s.GetPolicies()
	if err != nil {
		return
	}
	now := time.Now()
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}
		s.mu.RLock()
		lastRun := s.lastRun[policy.Tag]
		s.mu.RUnlock()
		if !lastRun.IsZero() && now.Sub(lastRun) < time.Duration(policy.IntervalSeconds)*time.Second {
			continue
		}
		s.evaluate(policy, now)
	}
}

func (s *FailoverService) evaluate(policy FailoverPolicy, now time.Time) {
	status := FailoverStatus{Policy: policy, LastChecked: now.Format(time.RFC3339), Probes: make([]FailoverProbe, 0, len(policy.Members))}
	current, runtimeMembers, err := selectorState(policy.Tag)
	if err != nil {
		status.Error = err.Error()
		s.storeStatus(status, now)
		return
	}
	status.Current = current
	for _, member := range policy.Members {
		if !slices.Contains(runtimeMembers, member) {
			status.Error = "策略组运行态成员已变化，请重新保存回退策略"
			s.storeStatus(status, now)
			return
		}
		probe := s.config.CheckOutbound(member, policy.TestURL)
		status.Probes = append(status.Probes, FailoverProbe{Tag: member, OK: probe.OK, Delay: probe.Delay, Error: probe.Error})
		if probe.OK {
			status.Candidate = member
			break
		}
	}
	if status.Candidate == "" {
		status.Error = "所有成员探测失败，保留当前出口"
		s.storeStatus(status, now)
		return
	}

	s.mu.Lock()
	previous := s.statuses[policy.Tag]
	if status.Candidate == current {
		delete(s.pendingFor, policy.Tag)
		status.Pending = 0
		status.LastSwitch = previous.LastSwitch
		s.statuses[policy.Tag] = status
		s.lastRun[policy.Tag] = now
		s.mu.Unlock()
		return
	}
	if s.pendingFor[policy.Tag] != status.Candidate {
		s.pendingFor[policy.Tag] = status.Candidate
		status.Pending = 1
	} else {
		status.Pending = previous.Pending + 1
	}
	required := policy.FailureThreshold
	if slices.Index(policy.Members, status.Candidate) < slices.Index(policy.Members, current) {
		required = policy.RecoveryThreshold
	}
	status.LastSwitch = previous.LastSwitch
	s.statuses[policy.Tag] = status
	s.lastRun[policy.Tag] = now
	s.mu.Unlock()

	if status.Pending < required {
		return
	}
	if err := selectOutbound(policy.Tag, status.Candidate); err != nil {
		status.Error = err.Error()
		s.storeStatus(status, now)
		return
	}
	status.Current = status.Candidate
	status.Pending = 0
	status.LastSwitch = now.Format(time.RFC3339)
	s.mu.Lock()
	delete(s.pendingFor, policy.Tag)
	s.statuses[policy.Tag] = status
	s.mu.Unlock()
}

func (s *FailoverService) storeStatus(status FailoverStatus, now time.Time) {
	s.mu.Lock()
	if previous, exists := s.statuses[status.Policy.Tag]; exists {
		status.LastSwitch = previous.LastSwitch
	}
	s.statuses[status.Policy.Tag] = status
	s.lastRun[status.Policy.Tag] = now
	s.mu.Unlock()
}

func GetFailoverService() (*FailoverService, error) {
	if failoverPtr == nil {
		return nil, errors.New("failover service is not initialized")
	}
	return failoverPtr, nil
}
