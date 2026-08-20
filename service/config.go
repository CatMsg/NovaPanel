package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

var (
	lastUpdate        atomic.Int64
	corePtr           *core.Core
	startCoreMu       sync.Mutex
	lastStartFailTime time.Time
	startCooldown     = 15 * time.Second
	saveConfigMu      sync.Mutex
)

func CurrentDataVersion() int64 {
	now := time.Now().UnixMilli()
	lastUpdate.CompareAndSwap(0, now)
	return lastUpdate.Load()
}

func markDataUpdated() {
	timestamp := time.Now().UnixMilli()
	for {
		current := lastUpdate.Load()
		next := timestamp
		if next <= current {
			next = current + 1
		}
		if lastUpdate.CompareAndSwap(current, next) {
			return
		}
	}
}

type ConfigService struct {
	ClientService
	TlsService
	SettingService
	InboundService
	OutboundService
	ServicesService
	EndpointService
}

type SingBoxConfig struct {
	Log          json.RawMessage   `json:"log"`
	Dns          json.RawMessage   `json:"dns"`
	Ntp          json.RawMessage   `json:"ntp"`
	Inbounds     []json.RawMessage `json:"inbounds"`
	Outbounds    []json.RawMessage `json:"outbounds"`
	Services     []json.RawMessage `json:"services"`
	Endpoints    []json.RawMessage `json:"endpoints"`
	Route        json.RawMessage   `json:"route"`
	Experimental json.RawMessage   `json:"experimental"`
}

type postCommitAction struct {
	name string
	run  func() error
}

func NewConfigService(core *core.Core) *ConfigService {
	corePtr = core
	return &ConfigService{}
}

func (s *ConfigService) GetConfig(data string) (*[]byte, error) {
	return s.getConfig(database.GetDB(), data)
}

func (s *ConfigService) getConfig(db *gorm.DB, data string) (*[]byte, error) {
	var err error
	if len(data) == 0 {
		data, err = s.SettingService.getStringTx(db, "config")
		if err != nil {
			return nil, err
		}
	}
	singboxConfig := SingBoxConfig{}
	err = json.Unmarshal([]byte(data), &singboxConfig)
	if err != nil {
		return nil, err
	}

	singboxConfig.Inbounds, err = s.InboundService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	singboxConfig.Outbounds, err = s.OutboundService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	singboxConfig.Services, err = s.ServicesService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	singboxConfig.Endpoints, err = s.EndpointService.GetAllConfig(db)
	if err != nil {
		return nil, err
	}
	rawConfig, err := json.MarshalIndent(singboxConfig, "", "  ")
	if err != nil {
		return nil, err
	}
	return &rawConfig, nil
}

func (s *ConfigService) StartCore() error {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()
	if corePtr.IsRunning() {
		return nil
	}
	if time.Since(lastStartFailTime) < startCooldown {
		remaining := time.Until(lastStartFailTime.Add(startCooldown))
		if remaining < 0 {
			remaining = 0
		}
		logger.Info("start core cooldown ", remaining.Round(time.Second))
		return common.NewErrorf("sing-box 启动冷却中，请在 %s 后重试", remaining.Round(time.Second))
	}

	logger.Info("starting core")
	var rawConfig *[]byte
	err := database.RetryOnLocked(3, 100*time.Millisecond, func() error {
		var err error
		rawConfig, err = s.GetConfig("")
		return err
	})
	if err != nil {
		return err
	}
	if err = validateRuntimeConfig(*rawConfig); err != nil {
		logger.Error("start sing-box err (validate config):", err.Error())
		return err
	}
	err = corePtr.Start(*rawConfig)
	if err != nil {
		lastStartFailTime = time.Now()
		logger.Error("start sing-box err:", err.Error())
		return err
	}
	logger.Info("sing-box started")
	return nil
}

func (s *ConfigService) RestartCore() error {
	return s.restartCoreWithConfig(nil)
}

func (s *ConfigService) restartCoreWithConfig(config json.RawMessage) error {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()

	var rawConfig *[]byte
	err := database.RetryOnLocked(3, 100*time.Millisecond, func() error {
		var err error
		rawConfig, err = s.GetConfig(string(config))
		return err
	})
	if err != nil {
		logger.Error("restart sing-box err (get config):", err.Error())
		return err
	}
	if err := validateRuntimeConfig(*rawConfig); err != nil {
		logger.Error("restart sing-box err (validate config):", err.Error())
		return err
	}
	if corePtr.IsRunning() {
		if err := corePtr.Stop(); err != nil {
			logger.Error("restart sing-box err (stop):", err.Error())
			return err
		}
	}
	if err := corePtr.Start(*rawConfig); err != nil {
		logger.Error("restart sing-box err (start):", err.Error())
		return err
	}
	logger.Info("sing-box restarted with new config")
	return nil
}

func (s *ConfigService) StopCore() error {
	err := corePtr.Stop()
	if err != nil {
		return err
	}
	logger.Info("sing-box stopped")
	return nil
}

func (s *ConfigService) CheckOutbound(tag string, link string) core.CheckOutboundResult {
	if tag == "" {
		return core.CheckOutboundResult{Error: "missing query parameter: tag"}
	}
	if corePtr == nil || !corePtr.IsRunning() {
		return core.CheckOutboundResult{Error: "core not running"}
	}
	return core.CheckOutbound(corePtr.GetCtx(), tag, link)
}

func (s *ConfigService) Save(obj string, act string, data json.RawMessage, initUsers string, loginUser string, hostname string) ([]string, bool, error) {
	saveConfigMu.Lock()
	defer saveConfigMu.Unlock()

	var objs []string = []string{obj}
	var postCommit func() error
	var snapshot *configSnapshot
	var changeID uint64
	err := retryWriteTx(func(tx *gorm.DB) error {
		var err error
		snapshot, err = captureConfigSnapshot(tx, obj == "settings")
		if err != nil {
			return err
		}

		switch obj {
		case "clients":
			var inboundIds []uint
			inboundIds, err = s.ClientService.Save(tx, act, data, hostname)
			if err == nil && len(inboundIds) > 0 {
				objs = append(objs, "inbounds")
				postCommit, err = s.InboundService.BuildRestartInboundsAction(tx, inboundIds)
				if err != nil {
					return common.NewErrorf("failed to update users for inbounds: %v", err)
				}
			}
		case "tls":
			postCommit, err = s.TlsService.Save(tx, act, data, hostname)
			objs = append(objs, "clients", "inbounds")
		case "inbounds":
			postCommit, err = s.InboundService.Save(tx, act, data, initUsers, hostname)
			objs = append(objs, "clients")
		case "outbounds":
			_, err = s.OutboundService.Save(tx, act, data)
			if err == nil && corePtr != nil && corePtr.IsRunning() {
				postCommit = func() error { return s.restartCoreWithConfig(nil) }
			}
		case "services":
			postCommit, err = s.ServicesService.Save(tx, act, data)
		case "endpoints":
			postCommit, err = s.EndpointService.Save(tx, act, data)
		case "config":
			err = s.SettingService.SaveConfig(tx, data)
			if err != nil {
				return err
			}
			configData := make(json.RawMessage, len(data))
			copy(configData, data)
			postCommit = func() error { return s.restartCoreWithConfig(configData) }
		case "settings":
			postCommit, err = s.SettingService.Save(tx, data)
		default:
			return common.NewError("unknown object: ", obj)
		}
		if errors.Is(err, ErrNoChanges) {
			return ErrNoChanges
		}
		if err != nil {
			return err
		}
		if obj != "settings" {
			configData := ""
			if obj == "config" {
				configData = string(data)
			}
			rawConfig, configErr := s.getConfig(tx, configData)
			if configErr != nil {
				return configErr
			}
			if configErr = validateRuntimeConfig(*rawConfig); configErr != nil {
				return common.NewErrorf("Sing-Box 配置校验失败: %v", configErr)
			}
		}

		dt := time.Now().Unix()
		change := &model.Changes{
			DateTime: dt,
			Actor:    loginUser,
			Key:      obj,
			Action:   act,
			Obj:      data,
		}
		if err := tx.Create(change).Error; err != nil {
			return err
		}
		changeID = change.Id
		return nil
	})
	if errors.Is(err, ErrNoChanges) {
		return objs, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	actions := make([]postCommitAction, 0, 3)
	if postCommit != nil {
		actions = append(actions, postCommitAction{
			name: "apply post-commit changes",
			run:  postCommit,
		})
	}
	if obj == "clients" && corePtr != nil && corePtr.IsRunning() {
		actions = append(actions, postCommitAction{
			name: "reload user rate limits",
			run: func() error {
				instance := corePtr.GetInstance()
				if instance == nil || instance.RateLimitTracker() == nil {
					return nil
				}
				return instance.RateLimitTracker().Reload()
			},
		})
	}
	if masquePtr != nil && (obj == "endpoints" || obj == "settings") {
		actions = append(actions, postCommitAction{
			name: "sync masque service",
			run:  masquePtr.SyncFromDB,
		})
	}
	if obj != "config" && corePtr != nil && !corePtr.IsRunning() {
		actions = append(actions, postCommitAction{
			name: "start core",
			run:  s.StartCore,
		})
	}

	if err = runPostCommitActions(actions); err != nil {
		rollbackErr := s.compensateFailedSave(snapshot, obj, changeID)
		if rollbackErr != nil {
			return nil, false, errors.Join(err, common.NewErrorf("保存补偿失败: %v", rollbackErr))
		}
		return nil, false, common.NewErrorf("保存失败，配置已自动回滚: %v", err)
	}

	markDataUpdated()

	return objs, true, nil
}

func runPostCommitActions(actions []postCommitAction) error {
	for _, action := range actions {
		if action.run == nil {
			continue
		}
		if err := action.run(); err != nil {
			return common.NewErrorf("%s: %v", action.name, err)
		}
	}
	return nil
}

func (s *ConfigService) CheckChanges(lu string) (bool, error) {
	if lu == "" {
		return true, nil
	}
	intLu, err := strconv.ParseInt(lu, 10, 64)
	if err != nil {
		return false, err
	}
	return CurrentDataVersion() > intLu, nil
}

func (s *ConfigService) GetChanges(actor string, chngKey string, count string) []model.Changes {
	c, _ := strconv.Atoi(count)
	if c < 1 {
		c = 100
	} else if c > 1000 {
		c = 1000
	}
	db := database.GetDB()
	var chngs []model.Changes
	query := db.Model(model.Changes{})
	if len(actor) > 0 {
		query = query.Where("actor = ?", actor)
	}
	if len(chngKey) > 0 {
		query = query.Where("key = ?", chngKey)
	}
	err := query.Order("id desc").Limit(c).Scan(&chngs).Error
	if err != nil {
		logger.Warning(err)
	}
	return chngs
}
