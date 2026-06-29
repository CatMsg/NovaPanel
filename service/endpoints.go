package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/gorm"
)

type EndpointService struct {
	WarpService
}

func (o *EndpointService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	endpoints := []*model.Endpoint{}
	err := db.Model(model.Endpoint{}).Scan(&endpoints).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, endpoint := range endpoints {
		epData := map[string]interface{}{
			"id":   endpoint.Id,
			"type": endpoint.Type,
			"tag":  endpoint.Tag,
			"ext":  endpoint.Ext,
		}
		if len(strings.TrimSpace(string(endpoint.Options))) > 0 {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(endpoint.Options, &restFields); err != nil {
				logger.Warning("skip invalid endpoint options while loading endpoint list: ", endpoint.Tag, " err=", err)
			} else {
				for k, v := range restFields {
					epData[k] = v
				}
			}
		}
		data = append(data, epData)
	}
	return &data, nil
}

func (o *EndpointService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var endpointsJson []json.RawMessage
	var endpoints []*model.Endpoint
	err := db.Model(model.Endpoint{}).Scan(&endpoints).Error
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		endpointJson, err := marshalEndpointConfigForCore(endpoint)
		if err != nil {
			return nil, err
		}
		if len(endpointJson) == 0 {
			continue
		}
		endpointsJson = append(endpointsJson, endpointJson)
	}
	return endpointsJson, nil
}

func marshalEndpointConfigForCore(endpoint *model.Endpoint) ([]byte, error) {
	if endpoint == nil {
		return nil, nil
	}
	if endpoint.Type == "masque" {
		return nil, nil
	}

	endpointJson, err := endpoint.MarshalJSON()
	if err != nil {
		return nil, err
	}

	if endpoint.Type != "wireguard" && endpoint.Type != "warp" {
		return endpointJson, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(endpointJson, &payload); err != nil {
		return nil, err
	}

	rawPeers, ok := payload["peers"].([]interface{})
	if !ok || len(rawPeers) == 0 {
		return endpointJson, nil
	}

	filteredPeers := make([]interface{}, 0, len(rawPeers))
	droppedPeers := 0
	for _, rawPeer := range rawPeers {
		peerMap, ok := rawPeer.(map[string]interface{})
		if !ok {
			filteredPeers = append(filteredPeers, rawPeer)
			continue
		}

		address, _ := peerMap["address"].(string)
		if strings.TrimSpace(address) == "" {
			droppedPeers++
			continue
		}

		filteredPeers = append(filteredPeers, rawPeer)
	}

	if droppedPeers == 0 {
		return endpointJson, nil
	}

	payload["peers"] = filteredPeers
	sanitized, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}

	logger.Warning("skip wireguard peers without endpoint while building core config: ", endpoint.Tag, " dropped=", droppedPeers)
	return sanitized, nil
}

func rollbackEndpointCoreState(act string, oldEndpoint, newEndpoint *model.Endpoint) error {
	if corePtr == nil || !corePtr.IsRunning() {
		return nil
	}

	switch act {
	case "new":
		if newEndpoint == nil {
			return nil
		}
		if newEndpoint.Type == "masque" {
			return nil
		}
		if err := corePtr.RemoveEndpoint(newEndpoint.Tag); err != nil && err != os.ErrInvalid {
			return err
		}
	case "edit", "del":
		if oldEndpoint == nil {
			return nil
		}
		if oldEndpoint.Type == "masque" {
			return nil
		}
		configData, err := marshalEndpointConfigForCore(oldEndpoint)
		if err != nil {
			return err
		}
		if len(configData) == 0 {
			return nil
		}
		if err := corePtr.AddEndpoint(configData); err != nil {
			return err
		}
	}

	return nil
}

func rollbackEndpointExternalState(act string, oldEndpoint, newEndpoint *model.Endpoint) error {
	var errs []error

	switch act {
	case "new":
		if err := syncManagedEndpointPortForwarding(newEndpoint, nil); err != nil {
			errs = append(errs, fmt.Errorf("rollback managed endpoint port forwarding failed: %w", err))
		}
	case "edit":
		if err := syncManagedEndpointPortForwarding(newEndpoint, oldEndpoint); err != nil {
			errs = append(errs, fmt.Errorf("rollback managed endpoint port forwarding failed: %w", err))
		}
	case "del":
		if err := syncManagedEndpointPortForwarding(nil, oldEndpoint); err != nil {
			errs = append(errs, fmt.Errorf("rollback managed endpoint port forwarding failed: %w", err))
		}
	}

	if err := rollbackEndpointCoreState(act, oldEndpoint, newEndpoint); err != nil {
		errs = append(errs, fmt.Errorf("rollback endpoint core failed: %w", err))
	}

	return errors.Join(errs...)
}

func (s *EndpointService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	var err error

	switch act {
	case "new", "edit":
		var endpoint model.Endpoint
		err = endpoint.UnmarshalJSON(data)
		if err != nil {
			return err
		}

		var oldEndpoint *model.Endpoint
		if act == "edit" {
			oldEndpoint = &model.Endpoint{}
			err = tx.Model(model.Endpoint{}).Where("id = ?", endpoint.Id).First(oldEndpoint).Error
			if err != nil {
				return err
			}
		}

		if endpoint.Type == "warp" {
			if act == "new" {
				err = s.WarpService.RegisterWarp(&endpoint)
				if err != nil {
					return err
				}
			} else {
				var old_license string
				err = tx.Model(model.Endpoint{}).Select("json_extract(ext, '$.license_key')").Where("id = ?", endpoint.Id).Find(&old_license).Error
				if err != nil {
					return err
				}
				err = s.WarpService.SetWarpLicense(old_license, &endpoint)
				if err != nil {
					return err
				}
			}
		}

		err = s.SyncManagedEndpointPortForwarding(oldEndpoint, &endpoint)
		if err != nil {
			return err
		}

		if corePtr.IsRunning() {
			configData, err := marshalEndpointConfigForCore(&endpoint)
			if err != nil {
				if rollbackErr := syncManagedEndpointPortForwarding(&endpoint, oldEndpoint); rollbackErr != nil {
					return errors.Join(err, rollbackErr)
				}
				return err
			}
			if len(configData) > 0 {
				if act == "edit" {
					if oldEndpoint != nil && oldEndpoint.Type != "masque" {
						err = corePtr.RemoveEndpoint(oldEndpoint.Tag)
						if err != nil && err != os.ErrInvalid {
							if rollbackErr := syncManagedEndpointPortForwarding(&endpoint, oldEndpoint); rollbackErr != nil {
								return errors.Join(err, rollbackErr)
							}
							return err
						}
					}
				}
				err = corePtr.AddEndpoint(configData)
				if err != nil {
					rollbackErr := rollbackEndpointExternalState(act, oldEndpoint, &endpoint)
					if rollbackErr != nil {
						return errors.Join(err, rollbackErr)
					}
					return err
				}
			}
		}

		err = tx.Save(&endpoint).Error
		if err != nil {
			rollbackErr := rollbackEndpointExternalState(act, oldEndpoint, &endpoint)
			if rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return err
		}
		oldEndpoint := &model.Endpoint{}
		err = tx.Model(model.Endpoint{}).Where("tag = ?", tag).First(oldEndpoint).Error
		if err != nil {
			return err
		}
		if corePtr.IsRunning() {
			err = corePtr.RemoveEndpoint(tag)
			if err != nil && err != os.ErrInvalid {
				return err
			}
		}
		err = s.SyncManagedEndpointPortForwarding(oldEndpoint, nil)
		if err != nil {
			rollbackErr := rollbackEndpointExternalState("del", oldEndpoint, nil)
			if rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
		err = tx.Where("tag = ?", tag).Delete(model.Endpoint{}).Error
		if err != nil {
			rollbackErr := rollbackEndpointExternalState("del", oldEndpoint, nil)
			if rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
	default:
		return common.NewErrorf("unknown action: %s", act)
	}
	return nil
}
