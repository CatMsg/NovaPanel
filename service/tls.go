package service

import (
	"encoding/json"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/gorm"
)

type TlsService struct {
	InboundService
	ServicesService
}

func (s *TlsService) GetAll() ([]model.Tls, error) {
	db := database.GetDB()
	tlsConfig := []model.Tls{}
	err := db.Model(model.Tls{}).Scan(&tlsConfig).Error
	if err != nil {
		return nil, err
	}

	return tlsConfig, nil
}

func (s *TlsService) Save(tx *gorm.DB, action string, data json.RawMessage, hostname string) (func() error, error) {
	var err error

	switch action {
	case "new", "edit":
		var tls model.Tls
		err = json.Unmarshal(data, &tls)
		if err != nil {
			return nil, err
		}
		if action == "edit" {
			current := &model.Tls{}
			if err := tx.Model(model.Tls{}).Where("id = ?", tls.Id).First(current).Error; err != nil {
				return nil, err
			}
			if current.Name == tls.Name &&
				equalJSONBytes(current.Server, tls.Server) &&
				equalJSONBytes(current.Client, tls.Client) {
				return nil, ErrNoChanges
			}
		}
		err = tx.Save(&tls).Error
		if err != nil {
			return nil, err
		}
		if action == "edit" {
			var inbounds []model.Inbound
			err = tx.Model(model.Inbound{}).Preload("Tls").Where("tls_id = ?", tls.Id).Find(&inbounds).Error
			if err != nil {
				return nil, err
			}
			var inboundRestartAction func() error
			if len(inbounds) > 0 {
				err = s.ClientService.UpdateLinksByInboundChange(tx, &inbounds, hostname, "")
				if err != nil {
					return nil, err
				}
				var inboundIds []uint
				for _, inbound := range inbounds {
					inboundIds = append(inboundIds, inbound.Id)
				}
				err = s.InboundService.UpdateOutJsons(tx, inboundIds, hostname)
				if err != nil {
					return nil, common.NewError("unable to update out_json of inbounds: ", err.Error())
				}
				inboundRestartAction, err = s.InboundService.BuildRestartInboundsAction(tx, inboundIds)
				if err != nil {
					return nil, err
				}
			}
			var serviceIds []uint
			err = tx.Model(model.Service{}).Where("tls_id = ?", tls.Id).Scan(&serviceIds).Error
			if err != nil {
				return nil, err
			}
			var serviceRestartAction func() error
			if len(serviceIds) > 0 {
				serviceRestartAction, err = s.ServicesService.BuildRestartServicesAction(tx, serviceIds)
				if err != nil {
					return nil, err
				}
			}
			return combinePostCommitActions(inboundRestartAction, serviceRestartAction), nil
		}
	case "del":
		var id uint
		err = json.Unmarshal(data, &id)
		if err != nil {
			return nil, err
		}
		var inboundCount int64
		err = tx.Model(model.Inbound{}).Where("tls_id = ?", id).Count(&inboundCount).Error
		if err != nil {
			return nil, err
		}
		var serviceCount int64
		err = tx.Model(model.Service{}).Where("tls_id = ?", id).Count(&serviceCount).Error
		if err != nil {
			return nil, err
		}
		if inboundCount > 0 || serviceCount > 0 {
			return nil, common.NewError("tls in use")
		}
		err = tx.Where("id = ?", id).Delete(model.Tls{}).Error
		if err != nil {
			return nil, err
		}
	}

	return nil, nil
}
