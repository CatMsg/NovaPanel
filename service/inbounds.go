package service

import (
	"encoding/json"
	"fmt"
	"os"
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
			if inbound.Type == "shadowtls" {
				json.Unmarshal(restFields["version"], &shadowtls_version)
			}
			if inbound.Type == "shadowsocks" {
				json.Unmarshal(restFields["managed"], &ss_managed)
			}
		}
		if s.hasUser(inbound.Type) &&
			!(inbound.Type == "shadowtls" && shadowtls_version < 3) &&
			!(inbound.Type == "shadowsocks" && ss_managed) {
			users := []string{}
			err = db.Raw("SELECT clients.name FROM clients, json_each(clients.inbounds) as je WHERE je.value = ?", inbound.Id).Scan(&users).Error
			if err != nil {
				return nil, err
			}
			inbData["users"] = users
		}

		data = append(data, inbData)
	}
	return &data, nil
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

		err = tx.Save(&inbound).Error
		if err != nil {
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
		if corePtr.IsRunning() {
			inboundConfig, err = inbound.MarshalJSON()
			if err != nil {
				return nil, err
			}

			if act == "edit" {
				inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
			} else {
				inboundConfig, err = s.initUsers(tx, inboundConfig, initUserIds, inbound.Type)
			}
			if err != nil {
				return nil, err
			}
		}

		inboundSnapshot := inbound
		oldSnapshot := oldInbound
		inboundConfigSnapshot := append([]byte(nil), inboundConfig...)
		postCommit = func() error {
			if corePtr.IsRunning() {
				if act == "edit" {
					if err := corePtr.RemoveInbound(oldSnapshot.Tag); err != nil && err != os.ErrInvalid {
						return err
					}
				}

				if len(inboundConfigSnapshot) > 0 {
					if err := corePtr.AddInbound(inboundConfigSnapshot); err != nil {
						return err
					}
				}
			}
			return s.syncInboundPortForwarding(oldSnapshot, &inboundSnapshot)
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
		oldSnapshot := oldInbound
		postCommit = func() error {
			if corePtr.IsRunning() {
				if err := corePtr.RemoveInbound(tag); err != nil && err != os.ErrInvalid {
					return err
				}
			}
			return s.syncInboundPortForwarding(oldSnapshot, nil)
		}
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
	return postCommit, nil
}

func (s *InboundService) removeInboundByTag(tag string) error {
	return retryWriteTx(func(tx *gorm.DB) error {
		oldInbound := &model.Inbound{}
		err := tx.Model(model.Inbound{}).Select("id", "tag").Where("tag = ?", tag).First(oldInbound).Error
		if err != nil {
			return err
		}

		if corePtr.IsRunning() {
			err = corePtr.RemoveInbound(tag)
			if err != nil && err != os.ErrInvalid {
				return err
			}
		}

		if err := s.ClientService.UpdateClientsOnInboundDelete(tx, oldInbound.Id, tag); err != nil {
			return err
		}

		return tx.Where("tag = ?", tag).Delete(model.Inbound{}).Error
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
	case "mixed", "socks", "http", "shadowsocks", "vmess", "trojan", "naive", "hysteria", "shadowtls", "tuic", "hysteria2", "vless", "anytls":
		return true
	}
	return false
}

func (s *InboundService) fetchUsers(db *gorm.DB, inboundType string, condition string, inbound map[string]interface{}) ([]json.RawMessage, error) {
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
			inboundType, condition)).Scan(&users).Error
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

	condition := fmt.Sprintf("%d IN (SELECT json_each.value FROM json_each(clients.inbounds))", inboundId)
	inbound["users"], err = s.fetchUsers(db, inboundType, condition, inbound)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

func (s *InboundService) initUsers(db *gorm.DB, inboundJson []byte, clientIds string, inboundType string) ([]byte, error) {
	ClientIds := strings.Split(clientIds, ",")
	if len(ClientIds) == 0 {
		return inboundJson, nil
	}

	if !s.hasUser(inboundType) {
		return inboundJson, nil
	}

	var inbound map[string]interface{}
	err := json.Unmarshal(inboundJson, &inbound)
	if err != nil {
		return nil, err
	}

	condition := fmt.Sprintf("id IN (%s)", strings.Join(ClientIds, ","))
	inbound["users"], err = s.fetchUsers(db, inboundType, condition, inbound)
	if err != nil {
		return nil, err
	}

	return json.Marshal(inbound)
}

func (s *InboundService) BuildRestartInboundsAction(tx *gorm.DB, ids []uint) (func() error, error) {
	if !corePtr.IsRunning() || len(ids) == 0 {
		return nil, nil
	}
	var inbounds []*model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", ids).Find(&inbounds).Error
	if err != nil {
		return nil, err
	}

	type inboundRestartConfig struct {
		tag    string
		config []byte
	}
	restartConfigs := make([]inboundRestartConfig, 0, len(inbounds))
	for _, inbound := range inbounds {
		inboundConfig, err := inbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		inboundConfig, err = s.addUsers(tx, inboundConfig, inbound.Id, inbound.Type)
		if err != nil {
			return nil, err
		}
		restartConfigs = append(restartConfigs, inboundRestartConfig{
			tag:    inbound.Tag,
			config: inboundConfig,
		})
	}
	snapshots := make([]coreReplaceSnapshot, 0, len(restartConfigs))
	for _, inbound := range restartConfigs {
		tag := inbound.tag
		snapshots = append(snapshots, coreReplaceSnapshot{
			removeTag: tag,
			config:    inbound.config,
			beforeAdd: func(currentTag string) error {
				corePtr.GetInstance().ConnTracker().CloseConnByInbound(currentTag)
				return nil
			},
		})
	}
	return buildCoreReplaceAction(snapshots, corePtr.RemoveInbound, corePtr.AddInbound), nil
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
