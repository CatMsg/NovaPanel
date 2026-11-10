package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
)

const (
	snellV6ModeDefault   = "default"
	snellV6ModeUnshaped  = "unshaped"
	snellV6ModeUnsafeRaw = "unsafe-raw"
	snellGeneratePSKMark = "__generate_per_target__"
)

func prepareSnellInbound(inbound *model.Inbound, previous *model.Inbound) error {
	if inbound == nil {
		return common.NewError("Snell 入站配置缺失")
	}
	if inbound.TlsId != 0 {
		return common.NewError("Snell v6 不使用 TLS 模板，请移除 TLS 配置")
	}
	options := make(map[string]json.RawMessage)
	if len(inbound.Options) > 0 {
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			return fmt.Errorf("解析 Snell v6 配置失败: %w", err)
		}
	}
	if options == nil {
		options = make(map[string]json.RawMessage)
	}

	version := 6
	if raw, ok := options["version"]; ok {
		if err := json.Unmarshal(raw, &version); err != nil {
			return common.NewError("Snell 仅支持 v6")
		}
	}
	if version != 6 {
		return common.NewError("面板受管入站仅支持 Snell v6")
	}
	options["version"] = mustMarshalRaw(version)

	mode := snellV6ModeDefault
	if raw, ok := options["mode"]; ok {
		if err := json.Unmarshal(raw, &mode); err != nil {
			return common.NewError("Snell v6 流量模式无效")
		}
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = snellV6ModeDefault
	}
	switch mode {
	case snellV6ModeDefault, snellV6ModeUnshaped, snellV6ModeUnsafeRaw:
	default:
		return common.NewErrorf("不支持 Snell v6 流量模式 %q", mode)
	}
	options["mode"] = mustMarshalRaw(mode)

	var psk string
	if raw, ok := options["psk"]; ok {
		if err := json.Unmarshal(raw, &psk); err != nil {
			return common.NewError("Snell v6 PSK 无效")
		}
	}
	if strings.TrimSpace(psk) == "" && previous != nil && previous.Type == "snell" {
		var oldOptions map[string]json.RawMessage
		if err := json.Unmarshal(previous.Options, &oldOptions); err != nil {
			return fmt.Errorf("读取现有 Snell PSK 失败: %w", err)
		}
		_ = json.Unmarshal(oldOptions["psk"], &psk)
	}
	if strings.TrimSpace(psk) == "" {
		var err error
		psk, err = newSnellSecret()
		if err != nil {
			return err
		}
	}
	if psk == snellGeneratePSKMark {
		return common.NewError("Snell PSK 尚未由目标服务器生成")
	}
	if size := len([]byte(psk)); size < 12 || size > 255 {
		return common.NewError("Snell v6 PSK 长度必须为 12-255 字节")
	}
	options["psk"] = mustMarshalRaw(psk)
	// The current Mihomo Snell v6 client authenticates with the shared PSK only.
	// Keep this inbound in single-key mode until Mihomo supports per-user userkey.
	delete(options, "users")

	encoded, err := json.Marshal(options)
	if err != nil {
		return err
	}
	inbound.Options = encoded
	return nil
}

func ensureSnellModeOption(inbound *model.Inbound) error {
	if inbound == nil || inbound.Type != "snell" {
		return nil
	}
	options := make(map[string]json.RawMessage)
	if len(inbound.Options) > 0 {
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			return fmt.Errorf("解析 Snell v6 配置失败: %w", err)
		}
	}
	var mode string
	if raw, ok := options["mode"]; ok {
		if err := json.Unmarshal(raw, &mode); err != nil {
			return common.NewError("Snell v6 流量模式无效")
		}
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = snellV6ModeDefault
	}
	options["mode"] = mustMarshalRaw(mode)
	encoded, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("序列化 Snell v6 配置失败: %w", err)
	}
	inbound.Options = encoded
	return nil
}

func newSnellSecret() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("生成 Snell v6 密钥失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}
