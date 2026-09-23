package service

import (
	"fmt"
	"regexp"
	"strings"
)

type healthLocaleText struct {
	Titles  map[string]string
	Phrases map[string]string
}

var healthNumberPattern = regexp.MustCompile(`\d+(?:\.\d+)?`)

var healthLocales = map[string]healthLocaleText{
	"zhHant": {
		Titles: map[string]string{
			"database": "資料庫", "traffic-budget": "VPS 總流量", "sing-box-compatibility": "Sing-Box 設定相容性",
			"core": "Sing-Box 核心", "disk": "系統磁碟", "ports": "連接埠規則", "panel-tls": "面板 TLS",
			"subscription-tls": "訂閱 TLS", "subscription": "訂閱服務", "masque": "MASQUE",
			"mieru": "Mieru", "credentials": "管理員憑據", "login-protection": "登入防護", "update": "背景更新",
		},
		Phrases: map[string]string{
			"healthy": "正常", "warning": "需要注意", "error": "檢查失敗", "info": "資訊",
			"unsupported": "目前平台不支援", "disabled": "未啟用", "running": "核心運行正常", "stopped": "核心未運行",
			"paused": "因 VPS 總流量硬保護暫停", "used": "已使用 %s%%", "rules_ok": "%s 條受管規則一致",
			"issues": "發現 %s 個問題", "tls_disabled": "目前未啟用 TLS", "cert_days": "憑證剩餘 %s 天",
			"cert_expires": "憑證將在 %s 天內到期", "subscription_ok": "監聽連接埠 %s，路徑 %s",
			"banned": "已永久封鎖 %s 個來源 IP", "login_ok": "10 分鐘內失敗 10 次後永久封鎖",
			"traffic_normal": "%s", "traffic_warning": "已達預警門檻；%s", "traffic_critical": "已達嚴重門檻；%s",
			"traffic_blocked": "使用者池已耗盡，代理資料面已停止；%s", "traffic_error": "流量計量異常；%s",
			"update_running": "更新正在執行", "update_failed": "上次更新失敗", "update_ok": "沒有失敗的更新任務",
			"no_config": "尚未設定", "not_initialized": "服務未初始化",
			"inbounds_running": "%s 個入站運行正常", "inbounds_ratio": "%s/%s 個入站運行",
			"runtime_issues": "發現 %s 個運行問題", "attention": "發現 %s 個注意項", "watchdog": "資料面探活連續失敗 %s 次",
			"default_credentials": "仍在使用 admin/admin", "credentials_ok": "未使用預設管理員憑據",
		},
	},
	"en": {
		Titles: map[string]string{
			"database": "Database", "traffic-budget": "VPS traffic", "sing-box-compatibility": "Sing-Box compatibility",
			"core": "Sing-Box core", "disk": "System disk", "ports": "Port rules", "panel-tls": "Panel TLS",
			"subscription-tls": "Subscription TLS", "subscription": "Subscription service", "masque": "MASQUE",
			"mieru": "Mieru", "credentials": "Administrator credentials", "login-protection": "Login protection", "update": "Background update",
		},
		Phrases: map[string]string{
			"healthy": "Healthy", "warning": "Needs attention", "error": "Check failed", "info": "Informational",
			"unsupported": "Not supported on this platform", "disabled": "Not enabled", "running": "Running normally", "stopped": "Not running",
			"paused": "Paused by VPS traffic hard protection", "used": "Used %s%%", "rules_ok": "%s managed rules are consistent",
			"issues": "%s issues found", "tls_disabled": "TLS is not enabled", "cert_days": "Certificate has %s days remaining",
			"cert_expires": "Certificate expires within %s days", "subscription_ok": "Listening on port %s, path %s",
			"banned": "%s source IPs are permanently banned", "login_ok": "Permanent ban after 10 failures within 10 minutes",
			"traffic_normal": "%s", "traffic_warning": "Warning threshold reached; %s", "traffic_critical": "Critical threshold reached; %s",
			"traffic_blocked": "User pool exhausted and proxy data planes stopped; %s", "traffic_error": "Traffic metering error; %s",
			"update_running": "Update is running", "update_failed": "The last update failed", "update_ok": "No failed update tasks",
			"no_config": "No configuration present", "not_initialized": "Service is not initialized",
			"inbounds_running": "%s inbounds are running normally", "inbounds_ratio": "%s/%s inbounds are running",
			"runtime_issues": "%s runtime issues found", "attention": "%s items need attention", "watchdog": "Data-plane health check failed %s consecutive times",
			"default_credentials": "admin/admin is still in use", "credentials_ok": "Default administrator credentials are not in use",
		},
	},
	"ru": {
		Titles: map[string]string{
			"database": "База данных", "traffic-budget": "Трафик VPS", "sing-box-compatibility": "Совместимость Sing-Box",
			"core": "Ядро Sing-Box", "disk": "Системный диск", "ports": "Правила портов", "panel-tls": "TLS панели",
			"subscription-tls": "TLS подписки", "subscription": "Сервис подписки", "masque": "MASQUE",
			"mieru": "Mieru", "credentials": "Учётные данные администратора", "login-protection": "Защита входа", "update": "Фоновое обновление",
		},
		Phrases: map[string]string{
			"healthy": "Исправно", "warning": "Требует внимания", "error": "Проверка завершилась ошибкой", "info": "Информация",
			"unsupported": "Не поддерживается на этой платформе", "disabled": "Не включено", "running": "Работает нормально", "stopped": "Не запущено",
			"paused": "Приостановлено жёсткой защитой трафика VPS", "used": "Использовано %s%%", "rules_ok": "Управляемых правил без расхождений: %s",
			"issues": "Обнаружено проблем: %s", "tls_disabled": "TLS не включён", "cert_days": "До окончания сертификата: %s дн.",
			"cert_expires": "Сертификат истекает в течение %s дн.", "subscription_ok": "Порт %s, путь %s",
			"banned": "Навсегда заблокировано IP-адресов: %s", "login_ok": "Постоянная блокировка после 10 ошибок входа за 10 минут",
			"traffic_normal": "%s", "traffic_warning": "Достигнут порог предупреждения; %s", "traffic_critical": "Достигнут критический порог; %s",
			"traffic_blocked": "Пул пользователей исчерпан, прокси-данные остановлены; %s", "traffic_error": "Ошибка учёта трафика; %s",
			"update_running": "Обновление выполняется", "update_failed": "Последнее обновление завершилось ошибкой", "update_ok": "Нет неудачных задач обновления",
			"no_config": "Конфигурация отсутствует", "not_initialized": "Сервис не инициализирован",
			"inbounds_running": "Входящих подключений работает нормально: %s", "inbounds_ratio": "Работает входящих подключений: %s/%s",
			"runtime_issues": "Обнаружено проблем выполнения: %s", "attention": "Требуют внимания: %s", "watchdog": "Проверка канала данных не прошла %s раз подряд",
			"default_credentials": "По-прежнему используется admin/admin", "credentials_ok": "Стандартные учётные данные администратора не используются",
		},
	},
	"vi": {
		Titles: map[string]string{
			"database": "Cơ sở dữ liệu", "traffic-budget": "Lưu lượng VPS", "sing-box-compatibility": "Tương thích Sing-Box",
			"core": "Lõi Sing-Box", "disk": "Ổ đĩa hệ thống", "ports": "Quy tắc cổng", "panel-tls": "TLS panel",
			"subscription-tls": "TLS đăng ký", "subscription": "Dịch vụ đăng ký", "masque": "MASQUE",
			"mieru": "Mieru", "credentials": "Thông tin quản trị", "login-protection": "Bảo vệ đăng nhập", "update": "Cập nhật nền",
		},
		Phrases: map[string]string{
			"healthy": "Bình thường", "warning": "Cần chú ý", "error": "Kiểm tra thất bại", "info": "Thông tin",
			"unsupported": "Không được hỗ trợ trên nền tảng này", "disabled": "Chưa bật", "running": "Đang chạy bình thường", "stopped": "Không chạy",
			"paused": "Tạm dừng do bảo vệ cứng lưu lượng VPS", "used": "Đã dùng %s%%", "rules_ok": "%s quy tắc được quản lý khớp",
			"issues": "Phát hiện %s sự cố", "tls_disabled": "TLS chưa bật", "cert_days": "Chứng chỉ còn %s ngày",
			"cert_expires": "Chứng chỉ sẽ hết hạn trong %s ngày", "subscription_ok": "Lắng nghe cổng %s, đường dẫn %s",
			"banned": "%s IP nguồn đang bị chặn vĩnh viễn", "login_ok": "Chặn vĩnh viễn sau 10 lần thất bại trong 10 phút",
			"traffic_normal": "%s", "traffic_warning": "Đã đạt ngưỡng cảnh báo; %s", "traffic_critical": "Đã đạt ngưỡng nghiêm trọng; %s",
			"traffic_blocked": "Dung lượng người dùng đã hết và mặt phẳng dữ liệu proxy đã dừng; %s", "traffic_error": "Lỗi đo lưu lượng; %s",
			"update_running": "Đang cập nhật", "update_failed": "Lần cập nhật trước thất bại", "update_ok": "Không có tác vụ cập nhật thất bại",
			"no_config": "Chưa có cấu hình", "not_initialized": "Dịch vụ chưa khởi tạo",
			"inbounds_running": "%s inbound đang chạy bình thường", "inbounds_ratio": "%s/%s inbound đang chạy",
			"runtime_issues": "Phát hiện %s sự cố runtime", "attention": "%s mục cần chú ý", "watchdog": "Kiểm tra data-plane thất bại liên tiếp %s lần",
			"default_credentials": "Vẫn đang dùng admin/admin", "credentials_ok": "Không dùng thông tin quản trị mặc định",
		},
	},
	"fa": {
		Titles: map[string]string{
			"database": "پایگاه داده", "traffic-budget": "ترافیک VPS", "sing-box-compatibility": "سازگاری Sing-Box",
			"core": "هسته Sing-Box", "disk": "دیسک سیستم", "ports": "قوانین پورت", "panel-tls": "TLS پنل",
			"subscription-tls": "TLS اشتراک", "subscription": "سرویس اشتراک", "masque": "MASQUE",
			"mieru": "Mieru", "credentials": "اعتبارنامه مدیر", "login-protection": "حفاظت ورود", "update": "به\u200cروزرسانی پس\u200cزمینه",
		},
		Phrases: map[string]string{
			"healthy": "سالم", "warning": "نیازمند توجه", "error": "بررسی ناموفق بود", "info": "اطلاعات",
			"unsupported": "در این پلتفرم پشتیبانی نمی\u200cشود", "disabled": "فعال نشده", "running": "به\u200cطور عادی در حال اجرا", "stopped": "در حال اجرا نیست",
			"paused": "به\u200cدلیل حفاظت سخت ترافیک VPS متوقف شده", "used": "%s%% استفاده شده", "rules_ok": "%s قانون مدیریت\u200cشده سازگار است",
			"issues": "%s مشکل پیدا شد", "tls_disabled": "TLS فعال نیست", "cert_days": "%s روز از اعتبار گواهی باقی مانده",
			"cert_expires": "گواهی طی %s روز منقضی می\u200cشود", "subscription_ok": "پورت %s، مسیر %s",
			"banned": "%s IP مبدأ برای همیشه مسدود است", "login_ok": "مسدودی دائمی پس از ۱۰ خطا در ۱۰ دقیقه",
			"traffic_normal": "%s", "traffic_warning": "آستانه هشدار رسیده است؛ %s", "traffic_critical": "آستانه بحرانی رسیده است؛ %s",
			"traffic_blocked": "سهم کاربران تمام شده و دیتاپلین پروکسی متوقف است؛ %s", "traffic_error": "خطای اندازه\u200cگیری ترافیک؛ %s",
			"update_running": "به\u200cروزرسانی در حال اجراست", "update_failed": "آخرین به\u200cروزرسانی ناموفق بود", "update_ok": "وظیفه به\u200cروزرسانی ناموفقی وجود ندارد",
			"no_config": "پیکربندی وجود ندارد", "not_initialized": "سرویس مقداردهی نشده است",
			"inbounds_running": "%s ورودی به\u200cطور عادی فعال است", "inbounds_ratio": "%s/%s ورودی فعال است",
			"runtime_issues": "%s مشکل زمان اجرا پیدا شد", "attention": "%s مورد نیازمند توجه است", "watchdog": "بررسی دیتاپلین %s بار پیاپی ناموفق بود",
			"default_credentials": "هنوز admin/admin استفاده می\u200cشود", "credentials_ok": "اعتبارنامه پیش\u200cفرض مدیر استفاده نمی\u200cشود",
		},
	},
}

func LocalizeHealthReport(report *HealthReport, language string) *HealthReport {
	if report == nil {
		return nil
	}
	language = normalizeAlertLanguage(language)
	if language == "zhHans" {
		return report
	}
	locale, ok := healthLocales[language]
	if !ok {
		return report
	}
	copyReport := *report
	copyReport.Checks = make([]HealthCheck, len(report.Checks))
	for index, check := range report.Checks {
		localized := check
		if title := locale.Titles[check.ID]; title != "" {
			localized.Title = title
		}
		localized.Summary = localizeHealthSummary(language, check, report.Diagnostics)
		localized.Detail = localizeHealthDetail(language, check.Detail)
		copyReport.Checks[index] = localized
	}
	return &copyReport
}

func localizeHealthSummary(language string, check HealthCheck, diagnostics map[string]interface{}) string {
	switch check.ID {
	case "traffic-budget":
		if status, ok := diagnostics["trafficBudget"].(TrafficBudgetStatus); ok {
			summary := localizedTrafficSummary(language, status)
			switch status.Level {
			case "warning":
				return healthPhrase(language, "traffic_warning", summary)
			case "critical":
				return healthPhrase(language, "traffic_critical", summary)
			case "blocked":
				return healthPhrase(language, "traffic_blocked", summary)
			case "error":
				return healthPhrase(language, "traffic_error", summary)
			default:
				if !status.Enabled {
					return healthPhrase(language, "disabled")
				}
				if !status.Supported {
					return healthPhrase(language, "unsupported")
				}
				return healthPhrase(language, "traffic_normal", summary)
			}
		}
	case "core":
		if strings.Contains(check.Summary, "流量") {
			return healthPhrase(language, "paused")
		}
		if check.Status == "ok" {
			return healthPhrase(language, "running")
		}
		return healthPhrase(language, "stopped")
	case "disk":
		if values := healthNumbers(check.Summary); len(values) > 0 {
			return healthPhrase(language, "used", values[0])
		}
	case "ports":
		values := healthNumbers(check.Summary)
		if check.Status == "ok" && len(values) > 0 {
			return healthPhrase(language, "rules_ok", values[0])
		}
		if check.Status == "info" {
			return healthPhrase(language, "unsupported")
		}
		if len(values) > 0 {
			return healthPhrase(language, "issues", values[0])
		}
	case "panel-tls", "subscription-tls":
		if check.Status == "info" {
			return healthPhrase(language, "tls_disabled")
		}
		values := healthNumbers(check.Summary)
		if check.Status == "ok" && len(values) > 0 {
			return healthPhrase(language, "cert_days", values[0])
		}
		if check.Status == "warning" && len(values) > 0 {
			return healthPhrase(language, "cert_expires", values[0])
		}
	case "subscription":
		if check.Status == "ok" {
			port := ""
			path := ""
			if values := healthNumbers(check.Summary); len(values) > 0 {
				port = values[0]
			}
			if marker := strings.Index(check.Summary, "路径 "); marker >= 0 {
				path = strings.TrimSpace(check.Summary[marker+len("路径 "):])
			}
			if port != "" && path != "" {
				return healthPhrase(language, "subscription_ok", port, path)
			}
		}
	case "login-protection":
		if check.Status == "info" {
			return healthPhrase(language, "unsupported")
		}
		if check.Status == "warning" {
			if values := healthNumbers(check.Summary); len(values) > 0 {
				return healthPhrase(language, "banned", values[0])
			}
		}
		if check.Status == "ok" {
			return healthPhrase(language, "login_ok")
		}
	case "masque", "mieru":
		if strings.Contains(check.Summary, "流量") {
			return healthPhrase(language, "paused")
		}
		if strings.Contains(check.Summary, "未初始化") {
			return healthPhrase(language, "not_initialized")
		}
		if strings.Contains(check.Summary, "未配置") {
			return healthPhrase(language, "no_config")
		}
		values := healthNumbers(check.Summary)
		if strings.Contains(check.Summary, "/") && len(values) >= 2 {
			return healthPhrase(language, "inbounds_ratio", values[0], values[1])
		}
		if strings.Contains(check.Summary, "运行问题") && len(values) > 0 {
			return healthPhrase(language, "runtime_issues", values[0])
		}
		if strings.Contains(check.Summary, "注意项") && len(values) > 0 {
			return healthPhrase(language, "attention", values[0])
		}
		if strings.Contains(check.Summary, "连续失败") && len(values) > 0 {
			return healthPhrase(language, "watchdog", values[0])
		}
		if check.Status == "ok" && len(values) > 0 {
			return healthPhrase(language, "inbounds_running", values[0])
		}
	case "credentials":
		if strings.Contains(check.Summary, "admin/admin") {
			return healthPhrase(language, "default_credentials")
		}
		if check.Status == "ok" {
			return healthPhrase(language, "credentials_ok")
		}
	case "update":
		if strings.Contains(check.Summary, "正在") {
			return healthPhrase(language, "update_running")
		}
		if strings.Contains(check.Summary, "失败") {
			return healthPhrase(language, "update_failed")
		}
		if check.Status == "ok" {
			return healthPhrase(language, "update_ok")
		}
	case "database", "sing-box-compatibility":
		if check.Status == "ok" {
			return healthPhrase(language, "healthy")
		}
	}
	return healthPhrase(language, check.Status)
}

func localizeHealthDetail(language, detail string) string {
	switch strings.TrimSpace(detail) {
	case "磁盘空间即将耗尽":
		return healthPhrase(language, "error")
	case "建议尽快清理日志、缓存或旧备份":
		return healthPhrase(language, "warning")
	default:
		return detail
	}
}

func healthPhrase(language, key string, args ...interface{}) string {
	locale, ok := healthLocales[normalizeAlertLanguage(language)]
	if !ok {
		return ""
	}
	value := locale.Phrases[key]
	if value == "" {
		value = locale.Phrases["info"]
	}
	if len(args) == 0 {
		return value
	}
	return fmt.Sprintf(value, args...)
}

func healthNumbers(value string) []string {
	return healthNumberPattern.FindAllString(value, -1)
}
