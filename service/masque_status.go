package service

import (
	"fmt"
	"time"
)

type MasqueDiagnostic struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (r *masqueRuntime) statusSnapshot() map[string]interface{} {
	if r == nil {
		return map[string]interface{}{}
	}

	r.sessionMu.Lock()
	lastConnect := r.lastConnect
	lastClose := r.lastClose
	lastError := r.lastError
	totalSessions := r.nextSession
	sessions := make([]map[string]interface{}, 0, len(r.sessions))
	activeUsers := make(map[string]struct{})
	for _, session := range r.sessions {
		activeUsers[session.identity.Name] = struct{}{}
		sessions = append(sessions, map[string]interface{}{
			"id": session.id, "user": session.identity.Name, "client_addr": session.remote,
			"started_at":     session.startedAt.Format(time.RFC3339),
			"uptime_seconds": int64(time.Since(session.startedAt).Seconds()),
			"rx_bytes":       session.rxBytes.Load(), "tx_bytes": session.txBytes.Load(),
			"rx_packets": session.rxPackets.Load(), "tx_packets": session.txPackets.Load(),
		})
	}
	r.sessionMu.Unlock()

	result := map[string]interface{}{
		"session_active":   len(sessions) > 0,
		"active_sessions":  len(sessions),
		"active_users":     len(activeUsers),
		"configured_users": len(r.clients),
		"sessions":         sessions,
		"total_sessions":   totalSessions,
		"takeover_count":   uint64(0),
		"rx_bytes":         r.rxBytes.Load(),
		"tx_bytes":         r.txBytes.Load(),
		"rx_packets":       r.rxPackets.Load(),
		"tx_packets":       r.txPackets.Load(),
	}
	if !lastConnect.IsZero() {
		result["last_connected_at"] = lastConnect.Format(time.RFC3339)
	}
	if !lastClose.IsZero() {
		result["last_disconnected_at"] = lastClose.Format(time.RFC3339)
	}
	if lastError != "" {
		result["last_error"] = lastError
	}
	return result
}

func buildMasqueDiagnostics(config *masqueInboundConfig, runtime *masqueRuntime, cert masqueCertificateSnapshot, certErr error, startError string) []MasqueDiagnostic {
	checks := make([]MasqueDiagnostic, 0, 7)

	if config != nil && config.Network == "quic" {
		checks = append(checks, MasqueDiagnostic{ID: "transport", Status: "ok", Title: "传输协议", Detail: "HTTP/3 CONNECT-IP / QUIC"})
	} else {
		network := ""
		if config != nil {
			network = config.Network
		}
		checks = append(checks, MasqueDiagnostic{ID: "transport", Status: "error", Title: "传输协议", Detail: fmt.Sprintf("不支持 %q，仅支持 quic", network)})
	}

	if runtime != nil && runtime.running.Load() {
		checks = append(checks, MasqueDiagnostic{ID: "listener", Status: "ok", Title: "UDP 监听", Detail: runtime.bindAddr})
	} else {
		detail := startError
		if detail == "" {
			detail = "服务未运行"
		}
		checks = append(checks, MasqueDiagnostic{ID: "listener", Status: "error", Title: "UDP 监听", Detail: detail})
	}

	if certErr != nil {
		checks = append(checks, MasqueDiagnostic{ID: "certificate", Status: "error", Title: "TLS 证书", Detail: certErr.Error()})
	} else if cert.LastError != "" {
		checks = append(checks, MasqueDiagnostic{ID: "certificate", Status: "error", Title: "TLS 证书热重载", Detail: cert.LastError})
	} else if !cert.NotBefore.IsZero() && time.Now().Before(cert.NotBefore) {
		checks = append(checks, MasqueDiagnostic{ID: "certificate", Status: "error", Title: "TLS 证书", Detail: "证书尚未生效"})
	} else if !cert.NotAfter.IsZero() && time.Now().After(cert.NotAfter) {
		checks = append(checks, MasqueDiagnostic{ID: "certificate", Status: "error", Title: "TLS 证书", Detail: "证书已过期"})
	} else if !cert.NotAfter.IsZero() && time.Until(cert.NotAfter) <= 14*24*time.Hour {
		checks = append(checks, MasqueDiagnostic{ID: "certificate", Status: "warning", Title: "TLS 证书", Detail: "证书将在 14 天内过期"})
	} else {
		detail := "入站内置证书"
		if cert.Source == "file" {
			detail = fmt.Sprintf("文件证书，已热重载 %d 次", cert.ReloadCount)
		}
		checks = append(checks, MasqueDiagnostic{ID: "certificate", Status: "ok", Title: "TLS 证书", Detail: detail})
	}

	checks = append(checks, masqueTunDiagnostics(runtime)...)

	if runtime != nil {
		runtime.sessionMu.Lock()
		activeSessions := len(runtime.sessions)
		activeUsers := len(runtime.userSessions)
		runtime.sessionMu.Unlock()
		if activeSessions > 0 {
			checks = append(checks, MasqueDiagnostic{ID: "session", Status: "ok", Title: "CONNECT-IP 会话", Detail: fmt.Sprintf("%d 个活动会话，%d 位用户", activeSessions, activeUsers)})
		} else {
			checks = append(checks, MasqueDiagnostic{ID: "session", Status: "info", Title: "CONNECT-IP 会话", Detail: "当前没有活动客户端"})
		}
		if len(runtime.clients) == 0 {
			checks = append(checks, MasqueDiagnostic{ID: "clients", Status: "warning", Title: "授权用户", Detail: "尚未给此入站分配用户"})
		} else {
			checks = append(checks, MasqueDiagnostic{ID: "clients", Status: "ok", Title: "授权用户", Detail: fmt.Sprintf("已配置 %d 位用户", len(runtime.clients))})
		}
	}
	return checks
}

func (s *MasqueService) healthSummary() (map[string]int, []string) {
	summary := s.GetSummary()
	summary["warnings"] = 0
	summary["errors"] = 0
	details := make([]string, 0)

	inbounds, err := s.loadMasqueInbounds()
	if err != nil {
		summary["errors"]++
		return summary, []string{err.Error()}
	}
	for _, inbound := range inbounds {
		if inbound == nil {
			continue
		}
		status, err := s.GetStatus(inbound.Tag)
		if err != nil {
			summary["errors"]++
			details = append(details, inbound.Tag+": "+err.Error())
			continue
		}
		checks, _ := status["diagnostics"].([]MasqueDiagnostic)
		for _, check := range checks {
			if check.Status != "error" && check.Status != "warning" {
				continue
			}
			if check.Status == "error" {
				summary["errors"]++
			} else {
				summary["warnings"]++
			}
			details = append(details, fmt.Sprintf("%s: %s - %s", inbound.Tag, check.Title, check.Detail))
		}
	}
	return summary, details
}
