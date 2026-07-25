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
	active := r.active
	lastConnect := r.lastConnect
	lastClose := r.lastClose
	lastError := r.lastError
	totalSessions := r.nextSession
	takeovers := r.takeovers
	r.sessionMu.Unlock()

	result := map[string]interface{}{
		"session_active": false,
		"total_sessions": totalSessions,
		"takeover_count": takeovers,
		"rx_bytes":       r.rxBytes.Load(),
		"tx_bytes":       r.txBytes.Load(),
		"rx_packets":     r.rxPackets.Load(),
		"tx_packets":     r.txPackets.Load(),
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
	if active != nil {
		result["session_active"] = true
		result["session_id"] = active.id
		result["session_started_at"] = active.startedAt.Format(time.RFC3339)
		result["session_uptime_seconds"] = int64(time.Since(active.startedAt).Seconds())
		result["client_addr"] = active.remote
		result["session_rx_bytes"] = active.rxBytes.Load()
		result["session_tx_bytes"] = active.txBytes.Load()
		result["session_rx_packets"] = active.rxPackets.Load()
		result["session_tx_packets"] = active.txPackets.Load()
	}
	return result
}

func buildMasqueDiagnostics(config *masqueEndpointConfig, runtime *masqueRuntime, cert masqueCertificateSnapshot, certErr error, startError string) []MasqueDiagnostic {
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
		detail := "节点内置证书"
		if cert.Source == "file" {
			detail = fmt.Sprintf("文件证书，已热重载 %d 次", cert.ReloadCount)
		}
		checks = append(checks, MasqueDiagnostic{ID: "certificate", Status: "ok", Title: "TLS 证书", Detail: detail})
	}

	checks = append(checks, masqueTunDiagnostics(runtime)...)

	if runtime != nil {
		runtime.sessionMu.Lock()
		active := runtime.active
		runtime.sessionMu.Unlock()
		if active != nil {
			checks = append(checks, MasqueDiagnostic{ID: "session", Status: "ok", Title: "CONNECT-IP 会话", Detail: fmt.Sprintf("会话 #%d，客户端 %s", active.id, active.remote)})
		} else {
			checks = append(checks, MasqueDiagnostic{ID: "session", Status: "info", Title: "CONNECT-IP 会话", Detail: "当前没有活动客户端"})
		}
	}
	return checks
}

func (s *MasqueService) healthSummary() (map[string]int, []string) {
	summary := s.GetSummary()
	summary["warnings"] = 0
	summary["errors"] = 0
	details := make([]string, 0)

	endpoints, err := s.loadMasqueEndpoints()
	if err != nil {
		summary["errors"]++
		return summary, []string{err.Error()}
	}
	for _, endpoint := range endpoints {
		if endpoint == nil {
			continue
		}
		status, err := s.GetStatus(endpoint.Tag)
		if err != nil {
			summary["errors"]++
			details = append(details, endpoint.Tag+": "+err.Error())
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
			details = append(details, fmt.Sprintf("%s: %s - %s", endpoint.Tag, check.Title, check.Detail))
		}
	}
	return summary, details
}
