package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

type PreflightCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type PreflightReport struct {
	Valid     bool             `json:"valid"`
	Changed   bool             `json:"changed"`
	Object    string           `json:"object"`
	Action    string           `json:"action"`
	CheckedAt string           `json:"checkedAt"`
	Checks    []PreflightCheck `json:"checks"`
	Warnings  []string         `json:"warnings"`
	Impacts   []string         `json:"impacts"`
}

// PreflightSave runs the same database-side validation as Save in a transaction
// that is always rolled back. External side effects are deliberately skipped.
func (s *ConfigService) PreflightSave(obj, act string, data json.RawMessage, initUsers, hostname string) (*PreflightReport, error) {
	report := &PreflightReport{
		Object:    obj,
		Action:    act,
		CheckedAt: time.Now().Format(time.RFC3339),
		Checks:    make([]PreflightCheck, 0, 3),
		Warnings:  make([]string, 0),
		Impacts:   preflightImpacts(obj),
	}
	if !json.Valid(data) {
		return preflightFailure(report, "JSON 格式", "提交内容不是有效 JSON", errors.New("invalid JSON payload"))
	}

	db := database.GetDB()
	if db == nil {
		return preflightFailure(report, "数据库", "数据库尚未初始化", errors.New("database is not initialized"))
	}
	tx := db.Begin()
	if tx.Error != nil {
		return preflightFailure(report, "数据库", "无法创建预检事务", tx.Error)
	}
	err := s.preflightSaveTx(tx, obj, act, data, initUsers, hostname, report)
	rollbackErr := tx.Rollback().Error
	if err == nil && rollbackErr != nil {
		err = rollbackErr
	}
	if errors.Is(err, ErrNoChanges) {
		report.Valid = true
		report.Changed = false
		report.Checks = append(report.Checks, PreflightCheck{Name: "变更检测", Status: "info", Detail: "配置没有变化"})
		return report, nil
	}
	if err != nil {
		return preflightFailure(report, "业务校验", err.Error(), err)
	}
	report.Valid = true
	report.Changed = true
	report.Checks = append(report.Checks,
		PreflightCheck{Name: "JSON 格式", Status: "ok", Detail: "结构可解析"},
		PreflightCheck{Name: "数据库约束", Status: "ok", Detail: "临时事务演练通过，未写入正式数据"},
		PreflightCheck{Name: "外部操作", Status: "info", Detail: "端口、核心和进程操作将在正式保存后执行"},
	)
	return report, nil
}

func (s *ConfigService) preflightSaveTx(tx *gorm.DB, obj, act string, data json.RawMessage, initUsers, hostname string, report *PreflightReport) error {
	var err error
	switch obj {
	case "clients":
		_, err = s.ClientService.Save(tx, act, data, hostname)
	case "tls":
		_, err = s.TlsService.Save(tx, act, data, hostname)
	case "inbounds":
		_, err = s.InboundService.Save(tx, act, data, initUsers, hostname)
	case "outbounds":
		_, err = s.OutboundService.Save(tx, act, data)
	case "services":
		_, err = s.ServicesService.Save(tx, act, data)
	case "endpoints":
		if act == "new" || act == "edit" {
			var endpoint model.Endpoint
			if err := endpoint.UnmarshalJSON(data); err != nil {
				return err
			}
			if endpoint.Type == "warp" {
				if err := preflightWarpEndpoint(tx, &endpoint); err != nil {
					return err
				}
				report.Warnings = append(report.Warnings, "WARP 注册需要访问远端服务，将在正式保存时执行")
				return nil
			}
		}
		_, err = s.EndpointService.Save(tx, act, data)
	case "config":
		err = s.SettingService.SaveConfig(tx, data)
	case "settings":
		_, err = s.SettingService.Save(tx, data)
	default:
		return common.NewError("unknown object: ", obj)
	}
	if err != nil {
		return err
	}
	if obj == "settings" {
		return nil
	}
	configData := ""
	if obj == "config" {
		configData = string(data)
	}
	rawConfig, err := s.getConfig(tx, configData)
	if err != nil {
		return err
	}
	return validateRuntimeConfig(*rawConfig)
}

func preflightWarpEndpoint(tx *gorm.DB, endpoint *model.Endpoint) error {
	if endpoint.Tag == "" {
		return errors.New("WARP 节点标签不能为空")
	}
	if endpoint.Id > 0 {
		var count int64
		if err := tx.Model(&model.Endpoint{}).Where("id = ?", endpoint.Id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("WARP 节点不存在: %d", endpoint.Id)
		}
	}
	return nil
}

func preflightFailure(report *PreflightReport, name, detail string, err error) (*PreflightReport, error) {
	report.Valid = false
	report.Checks = append(report.Checks, PreflightCheck{Name: name, Status: "error", Detail: detail})
	return report, err
}

func preflightImpacts(obj string) []string {
	switch strings.TrimSpace(obj) {
	case "inbounds":
		return []string{"入站配置", "用户链接", "受管端口规则", "Sing-Box 运行态"}
	case "endpoints":
		return []string{"节点配置", "受管端口规则", "MASQUE 或 Sing-Box 运行态"}
	case "settings":
		return []string{"面板或订阅服务设置", "相关监听端口"}
	case "tls":
		return []string{"TLS 配置", "关联入站与服务"}
	case "clients":
		return []string{"用户配置", "关联入站运行态", "订阅链接"}
	case "config":
		return []string{"Sing-Box 全局配置", "核心重启"}
	default:
		return []string{"数据库配置", "Sing-Box 运行态"}
	}
}
