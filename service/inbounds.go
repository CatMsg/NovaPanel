package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/gorm"
)

type InboundService struct {
	ClientService
}

func (s *InboundService) rollbackInboundCoreState(act string, oldInbound, newInbound *model.Inbound) error {
	if corePtr == nil || !corePtr.IsRunning() {
		return nil
	}
	switch act {
	case "new":
		if newInbound == nil {
			return nil
		}
		if newInbound.Type == "masque" {
			return nil
		}
		if err := corePtr.RemoveInbound(newInbound.Tag); err != nil && !errors.Is(err, os.ErrInvalid) {
			return err
		}
	case "edit", "del":
		if oldInbound == nil {
			return nil
		}
		if oldInbound.Type == "masque" {
			return nil
		}
		var configData []byte
		var err error
		if oldInbound.Type == "mieru" {
			configData, err = buildMieruBridgeInboundFromDB(database.GetDB(), oldInbound)
		} else {
			configData, err = oldInbound.MarshalJSON()
			if err == nil {
				configData, err = s.addUsers(database.GetDB(), configData, oldInbound.Id, oldInbound.Type)
			}
		}
		if err != nil {
			return err
		}
		if err := corePtr.AddInbound(configData); err != nil {
			return err
		}
	}
	return nil
}

func (s *InboundService) Get(ids string) (*[]map[string]interface{}, error) {
	if ids == "" {
		return s.GetAll()
	}
	return s.getById(ids)
}

func (s *InboundService) getById(ids string) (*[]map[string]interface{}, error) {
	var inbound []model.Inbound
	var result []map[string]interface{}
	db := database.GetDB()
	err := db.Model(model.Inbound{}).Where("id in ?", strings.Split(ids, ",")).Scan(&inbound).Error
	if err != nil {
		return nil, err
	}
	for _, inb := range inbound {
		inbData, err := inb.MarshalFull()
		if err != nil {
			return nil, err
		}
		result = append(result, *inbData)
	}
	return &result, nil
}

func (s *InboundService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	inbounds := []model.Inbound{}
	err := db.Model(model.Inbound{}).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	inboundUsers, err := loadInboundUsersMap(db)
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, inbound := range inbounds {
		var shadowtls_version uint
		ss_managed := false
		inbData := map[string]interface{}{
			"id":     inbound.Id,
			"type":   inbound.Type,
			"tag":    inbound.Tag,
			"tls_id": inbound.TlsId,
		}
		if inbound.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(inbound.Options, &restFields); err != nil {
				return nil, err
			}
			inbData["listen"] = restFields["listen"]
			inbData["listen_port"] = restFields["listen_port"]
			if inbound.Type == "mieru" {
				inbData["port_range"] = restFields["port_range"]
				inbData["transport"] = restFields["transport"]
			}
			if inbound.Type == "shadowtls" {
				if raw, ok := restFields["version"]; ok && len(raw) > 0 {
					if err := json.Unmarshal(raw, &shadowtls_version); err != nil {
						return nil, err
					}
				}
			}
			if inbound.Type == "shadowsocks" {
				if raw, ok := restFields["managed"]; ok && len(raw) > 0 {
					if err := json.Unmarshal(raw, &ss_managed); err != nil {
						return nil, err
					}
				}
			}
		}
		if s.hasUser(inbound.Type) &&
			!(inbound.Type == "shadowtls" && shadowtls_version < 3) &&
			!(inbound.Type == "shadowsocks" && ss_managed) {
			inbData["users"] = append([]string(nil), inboundUsers[inbound.Id]...)
		}

		data = append(data, inbData)
	}
	return &data, nil
}

type inboundUserRef struct {
	Name      string `gorm:"column:name"`
	InboundID uint   `gorm:"column:inbound_id"`
}

func loadInboundUsersMap(db *gorm.DB) (map[uint][]string, error) {
	rows := make([]inboundUserRef, 0)
	err := db.Raw(`
		SELECT clients.name AS name, CAST(je.value AS INTEGER) AS inbound_id
		FROM clients, json_each(clients.inbounds) AS je
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	usersByInbound := make(map[uint][]string)
	for _, row := range rows {
		usersByInbound[row.InboundID] = append(usersByInbound[row.InboundID], row.Name)
	}
	return usersByInbound, nil
}

func (s *InboundService) FromIds(ids []uint) ([]*model.Inbound, error) {
	db := database.GetDB()
	inbounds := []*model.Inbound{}
	err := db.Model(model.Inbound{}).Where("id in ?", ids).Scan(&inbounds).Error
	if err != nil {
		return nil, err
	}
	return inbounds, nil
}

func (s *InboundService) Save(tx *gorm.DB, act string, data json.RawMessage, initUserIds string, hostname string) (func() error, error) {
	var err error
	var postCommit func() error

	switch act {
	case "new", "edit":
		var inbound model.Inbound
		err = inbound.UnmarshalJSON(data)
		if err != nil {
			return nil, err
		}
		if inbound.TlsId > 0 {
			err = tx.Model(model.Tls{}).Where("id = ?", inbound.TlsId).Find(&inbound.Tls).Error
			if err != nil {
				return nil, err
			}
		}
		var oldInbound *model.Inbound
		if act == "edit" {
			oldInbound = &model.Inbound{}
			err = tx.Model(model.Inbound{}).Where("id = ?", inbound.Id).First(oldInbound).Error
			if err != nil {
				return nil, err
			}
		}
		if inbound.Type == "masque" {
			if err := prepareMasqueInbound(tx, &inbound, oldInbound, hostname); err != nil {
				return nil, err
			}
		}
		if inbound.Type == "mieru" {
			if _, err := parseMieruInbound(&inbound); err != nil {
				return nil, err
			}
			var count int64
			if err := tx.Model(&model.Inbound{}).
				Where("type = ? AND id <> ?", "mieru", inbound.Id).
				Count(&count).Error; err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, common.NewError("每台服务器只能创建一个 Mieru 入站")
			}
		}

		if _, ports, err := collectInboundForwardPorts(&inbound); err == nil {
			if err := validateInboundPortsAgainstSSH(&inbound, ports); err != nil {
				return nil, err
			}
			if err := validateManagedPortConflicts(tx, "入站", inbound.Tag, inbound.Id, 0, ports); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}

		err = util.FillOutJson(&inbound, hostname)
		if err != nil {
			return nil, err
		}
		if act == "edit" && oldInbound != nil &&
			oldInbound.Type == inbound.Type &&
			oldInbound.Tag == inbound.Tag &&
			oldInbound.TlsId == inbound.TlsId &&
			equalJSONBytes(oldInbound.Addrs, inbound.Addrs) &&
			equalJSONBytes(oldInbound.OutJson, inbound.OutJson) &&
			equalJSONBytes(oldInbound.Options, inbound.Options) {
			return nil, ErrNoChanges
		}

		err = tx.Save(&inbound).Error
		if err != nil {
			return nil, err
		}
		if err := syncManagedPortEntriesForInboundTx(tx, &inbound); err != nil {
			return nil, err
		}
		switch act {
		case "new":
			err = s.ClientService.UpdateClientsOnInboundAdd(tx, initUserIds, inbound.Id, hostname)
		case "edit":
			err = s.ClientService.UpdateLinksByInboundChange(tx, &[]model.Inbound{inbound}, hostname, oldInbound.Tag)
		}
		if err != nil {
			return nil, err
		}

		var inboundConfig []byte
		if corePtr.IsRunning() && inbound.Type != "masque" {
			if inbound.Type == "mieru" {
				inboundConfig, err = buildMieruBridgeInboundFromDB(tx, &inbound)
			} else {
				inboundConfig, err = inbound.MarshalJSON()
				if err == nil {
					if act == "edit" {
						inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
					} else {
						inboundConfig, err = s.initUsers(tx, inboundConfig, initUserIds, inbound.Type)
					}
				}
			}
			if err != nil {
				return nil, err
			}
		}

		inboundSnapshot := inbound
		oldSnapshot := oldInbound
		inboundConfigSnapshot := append([]byte(nil), inboundConfig...)
		postCommit = func() error {
			coreChanged := false
			if corePtr.IsRunning() {
				if act == "edit" && oldSnapshot != nil && oldSnapshot.Type != "masque" {
					if err := corePtr.RemoveInbound(oldSnapshot.Tag); err != nil && err != os.ErrInvalid {
						return err
					}
					coreChanged = true
				}

				if len(inboundConfigSnapshot) > 0 {
					if err := corePtr.AddInbound(inboundConfigSnapshot); err != nil {
						rollbackErr := s.rollbackInboundCoreState(act, oldSnapshot, &inboundSnapshot)
						return errors.Join(err, rollbackErr)
					}
					coreChanged = true
				}
			}
			if err := s.syncInboundPortForwarding(oldSnapshot, &inboundSnapshot); err != nil {
				if coreChanged {
					return errors.Join(err, s.rollbackInboundCoreState(act, oldSnapshot, &inboundSnapshot))
				}
				return err
			}
			if mieruPtr != nil && (inboundSnapshot.Type == "mieru" || (oldSnapshot != nil && oldSnapshot.Type == "mieru")) {
				if err := mieruPtr.SyncFromDB(); err != nil {
					return err
				}
			}
			if masquePtr != nil && (inboundSnapshot.Type == "masque" || (oldSnapshot != nil && oldSnapshot.Type == "masque")) {
				if err := masquePtr.SyncFromDB(); err != nil {
					return err
				}
			}
			return nil
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return nil, err
		}
		oldInbound := &model.Inbound{}
		err = tx.Model(model.Inbound{}).Where("tag = ?", tag).First(oldInbound).Error
		if err != nil {
			return nil, err
		}
		var id uint
		err = tx.Model(model.Inbound{}).Select("id").Where("tag = ?", tag).Scan(&id).Error
		if err != nil {
			return nil, err
		}
		err = s.ClientService.UpdateClientsOnInboundDelete(tx, id, tag)
		if err != nil {
			return nil, err
		}
		err = tx.Where("tag = ?", tag).Delete(model.Inbound{}).Error
		if err != nil {
			return nil, err
		}
		if err := deleteManagedPortEntriesForInboundTx(tx, id); err != nil {
			return nil, err
		}
		oldSnapshot := oldInbound
		postCommit = func() error {
			coreChanged := false
			if corePtr.IsRunning() && oldSnapshot.Type != "masque" {
				if err := corePtr.RemoveInbound(tag); err != nil && err != os.ErrInvalid {
					return err
				}
				coreChanged = true
			}
			if err := s.syncInboundPortForwarding(oldSnapshot, nil); err != nil {
				if coreChanged {
					return errors.Join(err, s.rollbackInboundCoreState(act, oldSnapshot, nil))
				}
				return err
			}
			if mieruPtr != nil && oldSnapshot.Type == "mieru" {
				if err := mieruPtr.SyncFromDB(); err != nil {
					return err
				}
			}
			if masquePtr != nil && oldSnapshot.Type == "masque" {
				if err := masquePtr.SyncFromDB(); err != nil {
					return err
				}
			}
			return nil
		}
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
	return postCommit, nil
}

func (s *InboundService) removeInboundByTag(tag string) error {
	return retryWriteTx(func(tx *gorm.DB) error {
		oldInbound := &model.Inbound{}
		err := tx.Model(model.Inbound{}).Select("id", "tag", "type").Where("tag = ?", tag).First(oldInbound).Error
		if err != nil {
			return err
		}

		if corePtr.IsRunning() && oldInbound.Type != "masque" {
			err = corePtr.RemoveInbound(tag)
			if err != nil && err != os.ErrInvalid {
				return err
			}
		}

		if err := s.ClientService.UpdateClientsOnInboundDelete(tx, oldInbound.Id, tag); err != nil {
			return err
		}
		if err := deleteManagedPortEntriesForInboundTx(tx, oldInbound.Id); err != nil {
			return err
		}

		if err := tx.Where("tag = ?", tag).Delete(model.Inbound{}).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *InboundService) UpdateOutJsons(tx *gorm.DB, inboundIds []uint, hostname string) error {
	var inbounds []model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", inboundIds).Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = util.FillOutJson(&inbound, hostname)
		if err != nil {
			return err
		}
		err = tx.Model(model.Inbound{}).Where("tag = ?", inbound.Tag).Update("out_json", inbound.OutJson).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *InboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var inboundsJson []json.RawMessage
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("Tls").Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if inbound.Type == "masque" {
			continue
		}
		if inbound.Type == "mieru" {
			inboundJson, err := buildMieruBridgeInboundFromDB(db, inbound)
			if err != nil {
				return nil, err
			}
			inboundsJson = append(inboundsJson, inboundJson)
			continue
		}
		inboundJson, err := inbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		inboundJson, err = s.addUsers(db, inboundJson, inbound.Id, inbound.Type)
		if err != nil {
			return nil, err
		}
		inboundsJson = append(inboundsJson, inboundJson)
	}
	return inboundsJson, nil
}

func (s *InboundService) hasUser(inboundType string) bool {
	switch inboundType {
	case "mixed", "socks", "http", "shadowsocks", "vmess", "trojan", "naive", "hysteria", "shadowtls", "tuic", "hysteria2", "vless", "anytls", "mieru", "masque":
		return true
	}
	return false
}

func (s *InboundService) fetchUsers(db *gorm.DB, inboundType string, condition string, args []interface{}, inbound map[string]interface{}) ([]json.RawMessage, error) {
	if inboundType == "shadowtls" {
		version, _ := inbound["version"].(float64)
		if int(version) < 3 {
			return nil, nil
		}
	}
	if inboundType == "shadowsocks" {
		method, _ := inbound["method"].(string)
		if method == "2022-blake3-aes-128-gcm" {
			inboundType = "shadowsocks16"
		}
	}

	var users []string

	err := db.Raw(
		fmt.Sprintf(`SELECT json_extract(clients.config, "$.%s")
			FROM clients WHERE enable = true AND %s`,
			inboundType, condition), args...).Scan(&users).Error
	if err != nil {
		return nil, err
	}
	var usersJson []json.RawMessage
	for _, user := range users {
		if inboundType == "vless" && inbound["tls"] == nil {
			user = strings.Replace(user, "xtls-rprx-vision", "", -1)
		}
		usersJson = append(usersJson, json.RawMessage(user))
	}
	return usersJson, nil
}

func (s *InboundService) addUsers(db *gorm.DB, inboundJson []byte, inboundId uint, inboundType string) ([]byte, error) {
	if !s.hasUser(inboundType) {
		return inboundJson, nil
	}

	var inbound map[string]interface{}
	err := json.Unmarshal(inboundJson, &inbound)
	if err != nil {
		return nil, err
	}

	condition := "? IN (SELECT json_each.value FROM json_each(clients.inbounds))"
	inbound["users"], err = s.fetchUsers(db, inboundType, condition, []interface{}{inboundId}, inbound)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

func (s *InboundService) initUsers(db *gorm.DB, inboundJson []byte, clientIds string, inboundType string) ([]byte, error) {
	clientIDList, err := parseClientIDs(clientIds)
	if err != nil {
		return nil, err
	}
	if len(clientIDList) == 0 {
		return inboundJson, nil
	}

	if !s.hasUser(inboundType) {
		return inboundJson, nil
	}

	var inbound map[string]interface{}
	err = json.Unmarshal(inboundJson, &inbound)
	if err != nil {
		return nil, err
	}

	condition := "id IN ?"
	inbound["users"], err = s.fetchUsers(db, inboundType, condition, []interface{}{clientIDList}, inbound)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

func parseClientIDs(raw string) ([]uint, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]uint, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil || value == 0 {
			return nil, common.NewErrorf("invalid client id: %q", strings.TrimSpace(part))
		}
		ids = append(ids, uint(value))
	}
	return ids, nil
}

func (s *InboundService) BuildRestartInboundsAction(tx *gorm.DB, ids []uint) (func() error, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var inbounds []*model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", ids).Find(&inbounds).Error
	if err != nil {
		return nil, err
	}

	restartConfigs := make([]taggedConfig, 0, len(inbounds))
	mieruChanged := false
	masqueChanged := false
	for _, inbound := range inbounds {
		if inbound.Type == "mieru" {
			mieruChanged = true
			continue
		}
		if inbound.Type == "masque" {
			masqueChanged = true
			continue
		}
		if corePtr == nil || !corePtr.IsRunning() {
			continue
		}
		inboundConfig, err := inbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
		if err != nil {
			return nil, err
		}
		restartConfigs = append(restartConfigs, taggedConfig{
			tag:    inbound.Tag,
			config: inboundConfig,
		})
	}
	var coreAction func() error
	if len(restartConfigs) > 0 {
		snapshots := buildCoreReplaceSnapshots(restartConfigs, func(currentTag string) error {
			corePtr.GetInstance().ConnTracker().CloseConnByInbound(currentTag)
			return nil
		})
		coreAction = buildCoreReplaceAction(snapshots, corePtr.RemoveInbound, corePtr.AddInbound)
	}
	if coreAction == nil && !mieruChanged && !masqueChanged {
		return nil, nil
	}
	return func() error {
		if coreAction != nil {
			if err := coreAction(); err != nil {
				return err
			}
		}
		if mieruChanged && mieruPtr != nil {
			if err := mieruPtr.SyncFromDB(); err != nil {
				return err
			}
		}
		if masqueChanged && masquePtr != nil {
			if err := masquePtr.SyncFromDB(); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

func (s *InboundService) RestartInbounds(tx *gorm.DB, ids []uint) error {
	action, err := s.BuildRestartInboundsAction(tx, ids)
	if err != nil {
		return err
	}
	if action == nil {
		return nil
	}
	return action()
}
