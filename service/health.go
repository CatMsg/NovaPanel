package service

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database"
)

type HealthCheck struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
	Action  string `json:"action,omitempty"`
}

type HealthReport struct {
	Status      string                 `json:"status"`
	CheckedAt   string                 `json:"checkedAt"`
	DurationMs  int64                  `json:"durationMs"`
	Summary     map[string]int         `json:"summary"`
	Checks      []HealthCheck          `json:"checks"`
	Diagnostics map[string]interface{} `json:"diagnostics"`
}

type HealthService struct {
	SettingService
	ServerService
}

func (s *HealthService) GetHealthReport(force bool) *HealthReport {
	started := time.Now()
	if force {
		clearServerStatusCache("sys", "sbd", "db", "dsk", "ports", "publicip")
	}

	report := &HealthReport{
		Status:      "healthy",
		CheckedAt:   started.Format(time.RFC3339),
		Summary:     map[string]int{"ok": 0, "warning": 0, "error": 0, "info": 0},
		Checks:      make([]HealthCheck, 0, 10),
		Diagnostics: make(map[string]interface{}),
	}
	report.add(s.checkDatabase())
	report.add(s.checkCore())
	report.add(s.checkDisk())
	report.add(s.checkPorts(report.Diagnostics))
	report.add(s.checkTLS("panel-tls", "面板 TLS", s.GetCertFile, s.GetKeyFile))
	report.add(s.checkTLS("subscription-tls", "订阅 TLS", s.GetSubCertFile, s.GetSubKeyFile))
	report.add(s.checkSubscription())
	report.add(s.checkMasque(report.Diagnostics))
	report.add(s.checkMieru(report.Diagnostics))
	report.add(s.checkDefaultCredentials())
	report.add(s.checkUpdate(report.Diagnostics))

	sort.SliceStable(report.Checks, func(i, j int) bool {
		return healthWeight(report.Checks[i].Status) > healthWeight(report.Checks[j].Status)
	})
	if report.Summary["error"] > 0 {
		report.Status = "critical"
	} else if report.Summary["warning"] > 0 {
		report.Status = "warning"
	}
	report.DurationMs = time.Since(started).Milliseconds()
	return report
}

func (r *HealthReport) add(check HealthCheck) {
	if _, ok := r.Summary[check.Status]; !ok {
		check.Status = "info"
	}
	r.Checks = append(r.Checks, check)
	r.Summary[check.Status]++
}

func (s *HealthService) checkDatabase() HealthCheck {
	db := database.GetDB()
	if db == nil {
		return HealthCheck{ID: "database", Title: "数据库", Status: "error", Summary: "数据库未初始化", Action: "restart"}
	}
	var result string
	if err := db.Raw("PRAGMA quick_check").Scan(&result).Error; err != nil {
		return HealthCheck{ID: "database", Title: "数据库", Status: "error", Summary: "SQLite 检查失败", Detail: err.Error()}
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return HealthCheck{ID: "database", Title: "数据库", Status: "error", Summary: "SQLite 完整性异常", Detail: result}
	}
	return HealthCheck{ID: "database", Title: "数据库", Status: "ok", Summary: "SQLite quick_check 通过"}
}

func (s *HealthService) checkCore() HealthCheck {
	info := s.ServerService.GetSingboxInfo()
	running, _ := info["running"].(bool)
	if !running {
		return HealthCheck{ID: "core", Title: "Sing-Box 核心", Status: "error", Summary: "核心未运行", Action: "restart-core"}
	}
	return HealthCheck{ID: "core", Title: "Sing-Box 核心", Status: "ok", Summary: "核心运行正常"}
}

func (s *HealthService) checkDisk() HealthCheck {
	info := s.ServerService.GetDiskInfo()
	used := healthUint64(info["current"])
	total := healthUint64(info["total"])
	if total == 0 {
		return HealthCheck{ID: "disk", Title: "系统磁盘", Status: "warning", Summary: "无法读取磁盘容量"}
	}
	percent := float64(used) / float64(total) * 100
	check := HealthCheck{ID: "disk", Title: "系统磁盘", Status: "ok", Summary: fmt.Sprintf("已使用 %.1f%%", percent)}
	if percent >= 95 {
		check.Status, check.Detail = "error", "磁盘空间即将耗尽"
	} else if percent >= 85 {
		check.Status, check.Detail = "warning", "建议尽快清理日志、缓存或旧备份"
	}
	return check
}

func (s *HealthService) checkPorts(diagnostics map[string]interface{}) HealthCheck {
	status := s.ServerService.GetPortStatus()
	diagnostics["ports"] = status
	drift, ok := status["drift"].(PortDriftReport)
	if !ok {
		return HealthCheck{ID: "ports", Title: "端口规则", Status: "warning", Summary: "无法解析端口漂移报告", Action: "ports"}
	}
	if drift.Status == "healthy" {
		return HealthCheck{ID: "ports", Title: "端口规则", Status: "ok", Summary: fmt.Sprintf("%d 条受管规则一致", drift.DesiredRules), Action: "ports"}
	}
	if drift.Status == "unsupported" {
		return HealthCheck{ID: "ports", Title: "端口规则", Status: "info", Summary: "当前系统不支持防火墙规则检查", Detail: "仅在 Linux 服务器上执行", Action: "ports"}
	}
	severity := "warning"
	if drift.Status == "drift" {
		severity = "error"
	}
	return HealthCheck{ID: "ports", Title: "端口规则", Status: severity, Summary: fmt.Sprintf("发现 %d 个规则问题", drift.IssueCount), Detail: drift.Status, Action: "reconcile-ports"}
}

func (s *HealthService) checkTLS(id, title string, certGetter, keyGetter func() (string, error)) HealthCheck {
	certFile, certErr := certGetter()
	keyFile, keyErr := keyGetter()
	if certErr != nil || keyErr != nil {
		return HealthCheck{ID: id, Title: title, Status: "warning", Summary: "读取证书设置失败", Detail: errorsText(certErr, keyErr)}
	}
	certFile, keyFile = strings.TrimSpace(certFile), strings.TrimSpace(keyFile)
	if certFile == "" && keyFile == "" {
		return HealthCheck{ID: id, Title: title, Status: "info", Summary: "当前未启用 TLS"}
	}
	if certFile == "" || keyFile == "" {
		return HealthCheck{ID: id, Title: title, Status: "error", Summary: "证书与密钥未成对配置", Action: "settings"}
	}
	if info, err := os.Stat(keyFile); err != nil || info.IsDir() || info.Size() == 0 {
		return HealthCheck{ID: id, Title: title, Status: "error", Summary: "密钥文件不可用", Detail: keyFile, Action: "settings"}
	}
	cert, err := loadHealthCertificate(certFile)
	if err != nil {
		return HealthCheck{ID: id, Title: title, Status: "error", Summary: "证书文件不可解析", Detail: err.Error(), Action: "settings"}
	}
	certPEM, certReadErr := os.ReadFile(certFile)
	keyPEM, keyReadErr := os.ReadFile(keyFile)
	if certReadErr != nil || keyReadErr != nil {
		return HealthCheck{ID: id, Title: title, Status: "error", Summary: "证书或密钥文件不可读取", Detail: errorsText(certReadErr, keyReadErr), Action: "settings"}
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return HealthCheck{ID: id, Title: title, Status: "error", Summary: "证书与私钥不匹配", Detail: err.Error(), Action: "settings"}
	}
	remaining := time.Until(cert.NotAfter)
	check := HealthCheck{ID: id, Title: title, Status: "ok", Summary: fmt.Sprintf("证书剩余 %d 天", int(remaining.Hours()/24)), Detail: cert.NotAfter.Format(time.RFC3339), Action: "settings"}
	if time.Now().Before(cert.NotBefore) {
		check.Status, check.Summary = "error", "证书尚未生效"
	} else if remaining <= 0 {
		check.Status, check.Summary = "error", "证书已过期"
	} else if remaining < 14*24*time.Hour {
		check.Status, check.Summary = "warning", fmt.Sprintf("证书将在 %d 天内到期", int(remaining.Hours()/24))
	}
	return check
}

func (s *HealthService) checkSubscription() HealthCheck {
	port, portErr := s.GetSubPort()
	path, pathErr := s.GetSubPath()
	if portErr != nil || pathErr != nil || port < 1 || port > 65535 {
		return HealthCheck{ID: "subscription", Title: "订阅服务", Status: "error", Summary: "订阅监听配置无效", Detail: errorsText(portErr, pathErr), Action: "settings"}
	}
	return HealthCheck{ID: "subscription", Title: "订阅服务", Status: "ok", Summary: fmt.Sprintf("监听端口 %d，路径 %s", port, path), Action: "settings"}
}

func (s *HealthService) checkMasque(diagnostics map[string]interface{}) HealthCheck {
	masque := GetMasqueService()
	if masque == nil {
		return HealthCheck{ID: "masque", Title: "MASQUE", Status: "info", Summary: "服务未初始化"}
	}
	summary, details := masque.healthSummary()
	diagnostics["masque"] = summary
	if summary["total"] == 0 {
		return HealthCheck{ID: "masque", Title: "MASQUE", Status: "info", Summary: "未配置 MASQUE 节点"}
	}
	if summary["running"] != summary["total"] {
		return HealthCheck{ID: "masque", Title: "MASQUE", Status: "error", Summary: fmt.Sprintf("%d/%d 个节点运行", summary["running"], summary["total"]), Action: "endpoints"}
	}
	if summary["errors"] > 0 {
		return HealthCheck{ID: "masque", Title: "MASQUE", Status: "error", Summary: fmt.Sprintf("发现 %d 个运行问题", summary["errors"]), Detail: strings.Join(details, "\n"), Action: "endpoints"}
	}
	if summary["warnings"] > 0 {
		return HealthCheck{ID: "masque", Title: "MASQUE", Status: "warning", Summary: fmt.Sprintf("发现 %d 个注意项", summary["warnings"]), Detail: strings.Join(details, "\n"), Action: "endpoints"}
	}
	return HealthCheck{ID: "masque", Title: "MASQUE", Status: "ok", Summary: fmt.Sprintf("%d 个节点运行正常", summary["running"]), Action: "endpoints"}
}

func (s *HealthService) checkMieru(diagnostics map[string]interface{}) HealthCheck {
	mieru := GetMieruService()
	if mieru == nil {
		return HealthCheck{ID: "mieru", Title: "Mieru", Status: "info", Summary: "服务未初始化"}
	}
	summary, details := mieru.healthSummary()
	diagnostics["mieru"] = summary
	if summary["total"] == 0 {
		return HealthCheck{ID: "mieru", Title: "Mieru", Status: "info", Summary: "未配置 Mieru 节点"}
	}
	if summary["running"] != summary["total"] {
		return HealthCheck{ID: "mieru", Title: "Mieru", Status: "error", Summary: fmt.Sprintf("%d/%d 个节点运行", summary["running"], summary["total"]), Detail: strings.Join(details, "\n"), Action: "endpoints"}
	}
	return HealthCheck{ID: "mieru", Title: "Mieru", Status: "ok", Summary: fmt.Sprintf("%d 个节点运行正常", summary["running"]), Action: "endpoints"}
}

func (s *HealthService) checkDefaultCredentials() HealthCheck {
	db := database.GetDB()
	if db == nil {
		return HealthCheck{ID: "credentials", Title: "管理员凭据", Status: "warning", Summary: "无法检查管理员凭据"}
	}
	var count int64
	if err := db.Table("users").Where("username = ? AND password = ?", "admin", "admin").Count(&count).Error; err != nil {
		return HealthCheck{ID: "credentials", Title: "管理员凭据", Status: "warning", Summary: "默认凭据检查失败", Detail: err.Error()}
	}
	if count > 0 {
		return HealthCheck{ID: "credentials", Title: "管理员凭据", Status: "error", Summary: "仍在使用 admin/admin", Action: "admins"}
	}
	return HealthCheck{ID: "credentials", Title: "管理员凭据", Status: "ok", Summary: "未使用默认管理员凭据"}
}

func (s *HealthService) checkUpdate(diagnostics map[string]interface{}) HealthCheck {
	status, err := GetUpdateStatus()
	if err != nil {
		return HealthCheck{ID: "update", Title: "后台更新", Status: "warning", Summary: "无法读取更新状态", Detail: err.Error()}
	}
	diagnostics["update"] = status
	state, _ := status["state"].(string)
	switch state {
	case "failed":
		return HealthCheck{ID: "update", Title: "后台更新", Status: "warning", Summary: "上次更新失败", Detail: fmt.Sprint(status["message"]), Action: "fleet"}
	case "running", "queued":
		return HealthCheck{ID: "update", Title: "后台更新", Status: "info", Summary: "更新正在执行", Action: "fleet"}
	default:
		return HealthCheck{ID: "update", Title: "后台更新", Status: "ok", Summary: "没有失败的更新任务", Action: "fleet"}
	}
}

func clearServerStatusCache(keys ...string) {
	serverStatusCache.mu.Lock()
	defer serverStatusCache.mu.Unlock()
	for _, key := range keys {
		delete(serverStatusCache.entries, key)
	}
}

func loadHealthCertificate(path string) (*x509.Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("certificate PEM is invalid: %s", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

func healthWeight(status string) int {
	switch status {
	case "error":
		return 4
	case "warning":
		return 3
	case "info":
		return 2
	default:
		return 1
	}
}

func healthUint64(value interface{}) uint64 {
	switch typed := value.(type) {
	case uint64:
		return typed
	case int64:
		if typed > 0 {
			return uint64(typed)
		}
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	}
	return 0
}

func errorsText(errs ...error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return strings.Join(parts, "; ")
}
