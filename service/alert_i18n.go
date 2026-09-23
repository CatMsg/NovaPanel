package service

import "fmt"

var supportedAlertLanguages = map[string]struct{}{
	"en": {}, "zhHans": {}, "zhHant": {}, "ru": {}, "vi": {}, "fa": {},
}

var alertTexts = map[string]map[string]string{
	"zhHans": {
		"test":             "NovaPanel 告警测试\n通知通道配置正常。",
		"login":            "NovaPanel 登录安全告警\nIP %s 在 10 分钟内连续失败 10 次，Fail2ban 已按当前永久封禁与白名单策略处理。",
		"health":           "NovaPanel 健康告警\n%s",
		"recovery":         "NovaPanel 恢复通知\n此前的非流量健康异常已恢复，当前没有 warning/error。",
		"traffic_warning":  "NovaPanel 流量预警\n%s",
		"traffic_critical": "NovaPanel 流量严重\n%s",
		"traffic_blocked":  "NovaPanel 流量硬保护\n代理数据面已停止；%s",
		"traffic_error":    "NovaPanel 流量计量异常\n%s",
		"traffic_recovery": "NovaPanel 流量恢复\n服务器总流量状态已恢复正常；%s",
	},
	"zhHant": {
		"test":             "NovaPanel 告警測試\n通知通道設定正常。",
		"login":            "NovaPanel 登入安全告警\nIP %s 在 10 分鐘內連續失敗 10 次，Fail2ban 已依目前永久封鎖與白名單策略處理。",
		"health":           "NovaPanel 健康告警\n%s",
		"recovery":         "NovaPanel 恢復通知\n先前的非流量健康異常已恢復，目前沒有 warning/error。",
		"traffic_warning":  "NovaPanel 流量預警\n%s",
		"traffic_critical": "NovaPanel 流量嚴重\n%s",
		"traffic_blocked":  "NovaPanel 流量硬保護\n代理資料面已停止；%s",
		"traffic_error":    "NovaPanel 流量計量異常\n%s",
		"traffic_recovery": "NovaPanel 流量恢復\n伺服器總流量狀態已恢復正常；%s",
	},
	"en": {
		"test":             "NovaPanel alert test\nThe notification channel is working.",
		"login":            "NovaPanel login security alert\nIP %s failed 10 logins within 10 minutes. Fail2ban applied the current permanent-ban and allowlist policy.",
		"health":           "NovaPanel health alert\n%s",
		"recovery":         "NovaPanel recovery\nPrevious non-traffic health issues have recovered; there are no warning/error checks now.",
		"traffic_warning":  "NovaPanel traffic warning\n%s",
		"traffic_critical": "NovaPanel traffic critical\n%s",
		"traffic_blocked":  "NovaPanel traffic hard protection\nProxy data planes have stopped; %s",
		"traffic_error":    "NovaPanel traffic metering error\n%s",
		"traffic_recovery": "NovaPanel traffic recovered\nServer traffic has returned to normal; %s",
	},
	"ru": {
		"test":             "Тест уведомлений NovaPanel\nКанал уведомлений работает.",
		"login":            "Предупреждение безопасности NovaPanel\nIP %s совершил 10 неудачных входов за 10 минут. Fail2ban применил текущие правила постоянной блокировки и белого списка.",
		"health":           "Предупреждение NovaPanel\n%s",
		"recovery":         "Восстановление NovaPanel\nПредыдущие проблемы, не связанные с трафиком, устранены; warning/error отсутствуют.",
		"traffic_warning":  "Предупреждение о трафике NovaPanel\n%s",
		"traffic_critical": "Критический трафик NovaPanel\n%s",
		"traffic_blocked":  "Жёсткая защита трафика NovaPanel\nПрокси-данные остановлены; %s",
		"traffic_error":    "Ошибка учёта трафика NovaPanel\n%s",
		"traffic_recovery": "Трафик NovaPanel восстановлен\nОбщий трафик сервера вернулся в норму; %s",
	},
	"vi": {
		"test":             "Kiểm tra cảnh báo NovaPanel\nKênh thông báo hoạt động bình thường.",
		"login":            "Cảnh báo bảo mật NovaPanel\nIP %s đăng nhập thất bại 10 lần trong 10 phút. Fail2ban đã áp dụng chính sách cấm vĩnh viễn và danh sách cho phép hiện tại.",
		"health":           "Cảnh báo sức khỏe NovaPanel\n%s",
		"recovery":         "NovaPanel đã phục hồi\nCác lỗi sức khỏe không liên quan đến lưu lượng đã phục hồi; hiện không còn warning/error.",
		"traffic_warning":  "Cảnh báo lưu lượng NovaPanel\n%s",
		"traffic_critical": "Lưu lượng nghiêm trọng NovaPanel\n%s",
		"traffic_blocked":  "Bảo vệ cứng lưu lượng NovaPanel\nMặt phẳng dữ liệu proxy đã dừng; %s",
		"traffic_error":    "Lỗi đo lưu lượng NovaPanel\n%s",
		"traffic_recovery": "Lưu lượng NovaPanel đã phục hồi\nTổng lưu lượng máy chủ đã trở lại bình thường; %s",
	},
	"fa": {
		"test":             "آزمایش هشدار NovaPanel\nکانال اعلان به درستی کار می کند.",
		"login":            "هشدار امنیت ورود NovaPanel\nIP %s طی ۱۰ دقیقه ۱۰ ورود ناموفق داشت. Fail2ban سیاست فعلی مسدودسازی دائمی و فهرست مجاز را اعمال کرد.",
		"health":           "هشدار سلامت NovaPanel\n%s",
		"recovery":         "بازیابی NovaPanel\nمشکلات سلامت غیرترافیکی قبلی برطرف شده اند و اکنون warning/error وجود ندارد.",
		"traffic_warning":  "هشدار ترافیک NovaPanel\n%s",
		"traffic_critical": "ترافیک بحرانی NovaPanel\n%s",
		"traffic_blocked":  "حفاظت سخت ترافیک NovaPanel\nدیتاپلین پروکسی متوقف شده است؛ %s",
		"traffic_error":    "خطای اندازه گیری ترافیک NovaPanel\n%s",
		"traffic_recovery": "ترافیک NovaPanel بازیابی شد\nترافیک کل سرور به حالت عادی بازگشته است؛ %s",
	},
}

func normalizeAlertLanguage(language string) string {
	if _, ok := supportedAlertLanguages[language]; ok {
		return language
	}
	return "zhHans"
}

func localizedAlertText(language, key string, args ...interface{}) string {
	language = normalizeAlertLanguage(language)
	value := alertTexts[language][key]
	if value == "" {
		value = alertTexts["zhHans"][key]
	}
	if len(args) == 0 {
		return value
	}
	return fmt.Sprintf(value, args...)
}

func localizedTrafficSummary(language string, status TrafficBudgetStatus) string {
	switch normalizeAlertLanguage(language) {
	case "en":
		return fmt.Sprintf("Used %.1f%% (%.2f / %.2f GB), user pool remaining %.2f GB", status.UsedPercent, float64(status.UsedBytes)/1e9, float64(status.ClientPoolBytes)/1e9, float64(status.PoolRemainingBytes)/1e9)
	case "ru":
		return fmt.Sprintf("Использовано %.1f%% (%.2f / %.2f GB), остаток пула пользователей %.2f GB", status.UsedPercent, float64(status.UsedBytes)/1e9, float64(status.ClientPoolBytes)/1e9, float64(status.PoolRemainingBytes)/1e9)
	case "vi":
		return fmt.Sprintf("Đã dùng %.1f%% (%.2f / %.2f GB), dung lượng người dùng còn %.2f GB", status.UsedPercent, float64(status.UsedBytes)/1e9, float64(status.ClientPoolBytes)/1e9, float64(status.PoolRemainingBytes)/1e9)
	case "fa":
		return fmt.Sprintf("%.1f%% استفاده شده (%.2f / %.2f GB)، سهم باقی مانده کاربران %.2f GB", status.UsedPercent, float64(status.UsedBytes)/1e9, float64(status.ClientPoolBytes)/1e9, float64(status.PoolRemainingBytes)/1e9)
	case "zhHant":
		return fmt.Sprintf("已使用 %.1f%%（%.2f / %.2f GB），使用者池剩餘 %.2f GB", status.UsedPercent, float64(status.UsedBytes)/1e9, float64(status.ClientPoolBytes)/1e9, float64(status.PoolRemainingBytes)/1e9)
	default:
		return fmt.Sprintf("已使用 %.1f%%（%.2f / %.2f GB），用户池剩余 %.2f GB", status.UsedPercent, float64(status.UsedBytes)/1e9, float64(status.ClientPoolBytes)/1e9, float64(status.PoolRemainingBytes)/1e9)
	}
}
