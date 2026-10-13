package service

import (
	"fmt"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

type MasqueDiagnostic struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (s *MasqueService) GetSummary() map[string]int {
	result := map[string]int{"total": 0, "running": 0}
	inbounds, err := s.loadMasqueInbounds()
	if err != nil {
		return result
	}
	result["total"] = len(inbounds)
	for _, inbound := range inbounds {
		if corePtr != nil && corePtr.IsRunning() {
			if _, exists := corePtr.GetInstance().Endpoint().Get(inbound.Tag); exists {
				result["running"]++
			}
		}
	}
	return result
}

func (s *MasqueService) GetStatus(tag string) (map[string]interface{}, error) {
	var inbound model.Inbound
	if err := database.GetDB().Preload("Tls").Where("type = ? AND tag = ?", "masque", tag).First(&inbound).Error; err != nil {
		return nil, err
	}
	config, err := parseMasqueInbound(&inbound)
	if err != nil {
		return nil, err
	}
	users, err := loadMasqueUsers(database.GetDB(), &inbound)
	if err != nil {
		return nil, err
	}
	running := false
	active := 0
	if corePtr != nil && corePtr.IsRunning() {
		_, running = corePtr.GetInstance().Endpoint().Get(tag)
		for _, session := range corePtr.GetInstance().ConnTracker().Sessions() {
			if session.Inbound == tag {
				active++
			}
		}
	}
	compatible := masqueMihomoEnabled(&inbound)
	detail := "内置用户态协议栈；无系统 TUN / NAT 规则。sing-box 使用 JSON 订阅；TCP/UDP 按面板用户统计和限速。"
	if compatible {
		detail += " 已开启 Mihomo 密钥认证，可使用 Clash / Mihomo 订阅；同一套密钥同时只允许一个隧道。"
	} else {
		detail += " 未开启 Mihomo 兼容，Clash 订阅不会包含此入站。"
	}
	return map[string]interface{}{
		"tag": tag, "host": config.Host, "port": config.Port, "network": "HTTP/3", "running": running,
		"configured_users": len(users), "active_connections": active,
		"mihomo_compatible": compatible,
		"client_subnet":     config.Address.String(), "template": "/.well-known/masque/ip/{target}/{ipproto}/",
		"cert_source": "TLS template", "bind_addr": fmt.Sprintf("UDP :%d", config.Port),
		"diagnostics": []MasqueDiagnostic{{ID: "native", Status: "info", Title: "原生 sing-box MASQUE", Detail: detail}},
	}, nil
}

func (s *MasqueService) healthSummary() (map[string]int, []string) {
	summary := s.GetSummary()
	summary["errors"], summary["warnings"] = 0, 0
	inbounds, err := s.loadMasqueInbounds()
	if err != nil {
		summary["errors"]++
		return summary, []string{err.Error()}
	}
	var details []string
	for _, inbound := range inbounds {
		if _, err := s.GetStatus(inbound.Tag); err != nil {
			summary["errors"]++
			details = append(details, inbound.Tag+": "+err.Error())
		}
	}
	return summary, details
}
