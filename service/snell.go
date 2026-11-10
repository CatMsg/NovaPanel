package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
)

const (
	snellV6ModeDefault   = "default"
	snellV6ModeUnshaped  = "unshaped"
	snellV6ModeUnsafeRaw = "unsafe-raw"
	snellUserModeShared  = "shared"
	snellUserModePerUser = "per-user"
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
		if len(previous.Options) > 0 {
			var oldOptions map[string]json.RawMessage
			if err := json.Unmarshal(previous.Options, &oldOptions); err != nil {
				return fmt.Errorf("读取现有 Snell PSK 失败: %w", err)
			}
			_ = json.Unmarshal(oldOptions["psk"], &psk)
		}
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
	userMode := options["user_mode"]
	var userModeValue string
	if len(userMode) > 0 {
		if err := json.Unmarshal(userMode, &userModeValue); err != nil {
			return common.NewError("Snell 用户认证模式无效")
		}
	}
	if strings.TrimSpace(userModeValue) == "" && previous != nil && previous.Type == "snell" {
		if len(previous.Options) > 0 {
			var previousOptions map[string]json.RawMessage
			if err := json.Unmarshal(previous.Options, &previousOptions); err != nil {
				return fmt.Errorf("读取现有 Snell 用户认证模式失败: %w", err)
			}
			_ = json.Unmarshal(previousOptions["user_mode"], &userModeValue)
		}
	}
	userModeValue = strings.TrimSpace(userModeValue)
	if userModeValue == "" {
		userModeValue = snellUserModeShared
	}
	switch userModeValue {
	case snellUserModeShared, snellUserModePerUser:
	default:
		return common.NewErrorf("不支持 Snell 用户认证模式 %q", userModeValue)
	}
	options["user_mode"] = mustMarshalRaw(userModeValue)
	// Runtime users are always rebuilt from local client bindings, never trusted
	// from an inbound request or copied from another server.
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
	var userMode string
	if raw, ok := options["user_mode"]; ok {
		if err := json.Unmarshal(raw, &userMode); err != nil {
			return common.NewError("Snell 用户认证模式无效")
		}
	}
	userMode = strings.TrimSpace(userMode)
	if userMode == "" {
		userMode = snellUserModeShared
	}
	if userMode != snellUserModeShared && userMode != snellUserModePerUser {
		return common.NewErrorf("不支持 Snell 用户认证模式 %q", userMode)
	}
	options["user_mode"] = mustMarshalRaw(userMode)
	encoded, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("序列化 Snell v6 配置失败: %w", err)
	}
	inbound.Options = encoded
	return nil
}

func SnellUsesPerUserAuth(inbound *model.Inbound) bool {
	if inbound == nil || inbound.Type != "snell" {
		return false
	}
	var options map[string]json.RawMessage
	if json.Unmarshal(inbound.Options, &options) != nil {
		return false
	}
	var userMode string
	_ = json.Unmarshal(options["user_mode"], &userMode)
	return strings.TrimSpace(userMode) == snellUserModePerUser
}

func ensureSnellClientUserKey(client *model.Client, rotate bool) (string, bool, error) {
	if client == nil {
		return "", false, common.NewError("Snell 用户配置缺失")
	}
	configs := make(map[string]json.RawMessage)
	if len(client.Config) > 0 {
		if err := json.Unmarshal(client.Config, &configs); err != nil {
			return "", false, fmt.Errorf("解析用户 %q 配置失败: %w", client.Name, err)
		}
	}
	if configs == nil {
		configs = make(map[string]json.RawMessage)
	}
	var snellConfig map[string]json.RawMessage
	if raw := configs["snell"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &snellConfig); err != nil {
			return "", false, fmt.Errorf("解析用户 %q 的 Snell 配置失败: %w", client.Name, err)
		}
	}
	if snellConfig == nil {
		snellConfig = make(map[string]json.RawMessage)
	}
	var userKey string
	_ = json.Unmarshal(snellConfig["userkey"], &userKey)
	changed := rotate || strings.TrimSpace(userKey) == ""
	if changed {
		var err error
		userKey, err = newSnellSecret()
		if err != nil {
			return "", false, err
		}
		snellConfig["userkey"] = mustMarshalRaw(userKey)
	}
	if strings.TrimSpace(client.Name) != "" {
		snellConfig["name"] = mustMarshalRaw(client.Name)
	}
	encodedSnell, err := json.Marshal(snellConfig)
	if err != nil {
		return "", false, err
	}
	configs["snell"] = encodedSnell
	encoded, err := json.Marshal(configs)
	if err != nil {
		return "", false, err
	}
	if !jsonEqual(client.Config, encoded) {
		changed = true
		client.Config = encoded
	}
	return userKey, changed, nil
}

func jsonEqual(left, right []byte) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil &&
		json.Unmarshal(right, &rightValue) == nil &&
		reflect.DeepEqual(leftValue, rightValue)
}

func newSnellSecret() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("生成 Snell v6 密钥失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}
