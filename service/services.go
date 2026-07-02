package service

import (
	"encoding/json"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/gorm"
)

type ServicesService struct{}

func (s *ServicesService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	services := []model.Service{}
	err := db.Model(model.Service{}).Scan(&services).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, srv := range services {
		srvData := map[string]interface{}{
			"id":     srv.Id,
			"type":   srv.Type,
			"tag":    srv.Tag,
			"tls_id": srv.TlsId,
		}
		if srv.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(srv.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				srvData[k] = v
			}
		}

		data = append(data, srvData)
	}
	return &data, nil
}

func (s *ServicesService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var servicesJson []json.RawMessage
	var services []*model.Service
	err := db.Model(model.Service{}).Preload("Tls").Find(&services).Error
	if err != nil {
		return nil, err
	}
	for _, srv := range services {
		srvJson, err := srv.MarshalJSON()
		if err != nil {
			return nil, err
		}
		servicesJson = append(servicesJson, srvJson)
	}
	return servicesJson, nil
}

func (s *ServicesService) Save(tx *gorm.DB, act string, data json.RawMessage) (func() error, error) {
	var err error

	switch act {
	case "new", "edit":
		var srv model.Service
		err = srv.UnmarshalJSON(data)
		if err != nil {
			return nil, err
		}

		if srv.TlsId > 0 {
			err = tx.Model(model.Tls{}).Where("id = ?", srv.TlsId).Find(&srv.Tls).Error
			if err != nil {
				return nil, err
			}
		}

		var oldTag string
		var configData []byte
		if corePtr.IsRunning() {
			configData, err = srv.MarshalJSON()
			if err != nil {
				return nil, err
			}
			if act == "edit" {
				err = tx.Model(model.Service{}).Select("tag").Where("id = ?", srv.Id).Find(&oldTag).Error
				if err != nil {
					return nil, err
				}
			}
		}

		err = tx.Save(&srv).Error
		if err != nil {
			return nil, err
		}
		if !corePtr.IsRunning() {
			return nil, nil
		}
		removeTag := ""
		if act == "edit" {
			removeTag = oldTag
		}
		return buildCoreReplaceAction([]coreReplaceSnapshot{{
			removeTag: removeTag,
			config:    configData,
		}}, corePtr.RemoveService, corePtr.AddService), nil
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return nil, err
		}
		err = tx.Where("tag = ?", tag).Delete(model.Service{}).Error
		if err != nil {
			return nil, err
		}
		if !corePtr.IsRunning() {
			return nil, nil
		}
		return buildCoreReplaceAction([]coreReplaceSnapshot{{
			removeTag: tag,
		}}, corePtr.RemoveService, corePtr.AddService), nil
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
	return nil, nil
}

func (s *ServicesService) BuildRestartServicesAction(tx *gorm.DB, ids []uint) (func() error, error) {
	if !corePtr.IsRunning() || len(ids) == 0 {
		return nil, nil
	}
	var services []*model.Service
	err := tx.Model(model.Service{}).Preload("Tls").Where("id in ?", ids).Find(&services).Error
	if err != nil {
		return nil, err
	}

	type serviceRestartConfig struct {
		tag    string
		config []byte
	}
	restartConfigs := make([]serviceRestartConfig, 0, len(services))
	for _, srv := range services {
		srvConfig, err := srv.MarshalJSON()
		if err != nil {
			return nil, err
		}
		restartConfigs = append(restartConfigs, serviceRestartConfig{tag: srv.Tag, config: srvConfig})
	}
	snapshots := make([]coreReplaceSnapshot, 0, len(restartConfigs))
	for _, srv := range restartConfigs {
		snapshots = append(snapshots, coreReplaceSnapshot{
			removeTag: srv.tag,
			config:    srv.config,
		})
	}
	return buildCoreReplaceAction(snapshots, corePtr.RemoveService, corePtr.AddService), nil
}

func (s *ServicesService) RestartServices(tx *gorm.DB, ids []uint) error {
	action, err := s.BuildRestartServicesAction(tx, ids)
	if err != nil {
		return err
	}
	if action == nil {
		return nil
	}
	return action()
}
