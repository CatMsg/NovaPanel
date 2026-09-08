package service

import (
	"encoding/json"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/gorm"
)

type OutboundService struct{}

func (o *OutboundService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	outbounds := []*model.Outbound{}
	err := db.Model(model.Outbound{}).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, outbound := range outbounds {
		outData := map[string]interface{}{
			"id":   outbound.Id,
			"type": outbound.Type,
			"tag":  outbound.Tag,
		}
		if outbound.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(outbound.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				outData[k] = v
			}
		}
		data = append(data, outData)
	}
	return &data, nil
}

func (o *OutboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var outboundsJson []json.RawMessage
	var outbounds []*model.Outbound
	err := db.Model(model.Outbound{}).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	for _, outbound := range outbounds {
		outboundJson, err := outbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		outboundsJson = append(outboundsJson, outboundJson)
	}
	return outboundsJson, nil
}

func (s *OutboundService) Save(tx *gorm.DB, act string, data json.RawMessage) (func() error, error) {
	var err error

	switch act {
	case "new", "edit":
		var outbound model.Outbound
		err = outbound.UnmarshalJSON(data)
		if err != nil {
			return nil, err
		}

		var oldOutbound *model.Outbound
		if act == "edit" {
			oldOutbound = &model.Outbound{}
			if err = tx.Model(model.Outbound{}).Where("id = ?", outbound.Id).First(oldOutbound).Error; err != nil {
				return nil, err
			}
			if oldOutbound.Type == outbound.Type &&
				oldOutbound.Tag == outbound.Tag &&
				equalJSONBytes(oldOutbound.Options, outbound.Options) {
				return nil, ErrNoChanges
			}
		}
		if outbound.Type == "" || outbound.Tag == "" {
			return nil, common.NewError("出站类型和标签不能为空")
		}

		err = tx.Save(&outbound).Error
		if err != nil {
			return nil, err
		}
		return nil, nil
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return nil, err
		}
		result := tx.Where("tag = ?", tag).Delete(model.Outbound{})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			err = common.NewError("出站不存在: ", tag)
		}
		if err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
}
