package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/gorm"
)

type ClientService struct{}

func decodeClientInboundIDs(raw json.RawMessage) ([]uint, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var inboundIDs []uint
	if err := json.Unmarshal(raw, &inboundIDs); err != nil {
		return nil, err
	}
	return inboundIDs, nil
}

func decodeClientLinks(raw json.RawMessage) ([]map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var links []map[string]string
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func encodeClientInboundIDs(inboundIDs []uint) (json.RawMessage, error) {
	if len(inboundIDs) == 0 {
		return json.RawMessage("[]"), nil
	}
	data, err := json.MarshalIndent(inboundIDs, "", "  ")
	if err != nil {
		return nil, err
	}
	return data, nil
}

func encodeClientLinks(links []map[string]string) (json.RawMessage, error) {
	if len(links) == 0 {
		return json.RawMessage("[]"), nil
	}
	data, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return nil, err
	}
	return data, nil
}

func filterClientLinks(links []map[string]string, keep func(map[string]string) bool) []map[string]string {
	if len(links) == 0 {
		return nil
	}
	filtered := make([]map[string]string, 0, len(links))
	for _, link := range links {
		if keep(link) {
			filtered = append(filtered, link)
		}
	}
	return filtered
}

func (s *ClientService) collectClientIDsByInboundID(tx *gorm.DB, inboundID uint) ([]uint, error) {
	var clientIDs []uint
	err := tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ?", inboundID).Scan(&clientIDs).Error
	if err != nil {
		return nil, err
	}
	return clientIDs, nil
}

func (s *ClientService) Get(id string) (*[]model.Client, error) {
	if id == "" {
		return s.GetAll()
	}
	return s.getById(id)
}

func (s *ClientService) getById(id string) (*[]model.Client, error) {
	db := database.GetDB()
	var client []model.Client
	err := db.Model(model.Client{}).Where("id in ?", strings.Split(id, ",")).Scan(&client).Error
	if err != nil {
		return nil, err
	}

	return &client, nil
}

func (s *ClientService) GetAll() (*[]model.Client, error) {
	db := database.GetDB()
	var clients []model.Client
	err := db.Model(model.Client{}).
		Select("`id`, `enable`, `name`, `desc`, `group`, `inbounds`, `up`, `down`, `volume`, `expiry`").
		Scan(&clients).Error
	if err != nil {
		return nil, err
	}
	return &clients, nil
}

func (s *ClientService) GetAllEnabledWithLinks() ([]model.Client, error) {
	db := database.GetDB()
	var clients []model.Client
	err := db.Model(model.Client{}).
		Where("enable = true").
		Find(&clients).Error
	if err != nil {
		return nil, err
	}
	return clients, nil
}

func (s *ClientService) Save(tx *gorm.DB, act string, data json.RawMessage, hostname string) ([]uint, error) {
	var err error
	var inboundIds []uint

	switch act {
	case "new", "edit":
		var client model.Client
		err = json.Unmarshal(data, &client)
		if err != nil {
			return nil, err
		}
		if act == "edit" {
			err = s.preserveClientHistory(tx, &client)
			if err != nil {
				return nil, err
			}
		}
		err = s.updateLinksWithFixedInbounds(tx, []*model.Client{&client}, hostname)
		if err != nil {
			return nil, err
		}
		if act == "edit" {
			// Find changed inbounds
			inboundIds, err = s.findInboundsChanges(tx, &client, false)
			if err != nil {
				return nil, err
			}
		} else {
			err = json.Unmarshal(client.Inbounds, &inboundIds)
			if err != nil {
				return nil, err
			}
		}
		err = tx.Save(&client).Error
		if err != nil {
			return nil, err
		}
	case "addbulk":
		var clients []*model.Client
		err = json.Unmarshal(data, &clients)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(clients[0].Inbounds, &inboundIds)
		if err != nil {
			return nil, err
		}
		err = s.updateLinksWithFixedInbounds(tx, clients, hostname)
		if err != nil {
			return nil, err
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, err
		}
	case "editbulk":
		var clients []*model.Client
		err = json.Unmarshal(data, &clients)
		if err != nil {
			return nil, err
		}
		for _, client := range clients {
			err = s.preserveClientHistory(tx, client)
			if err != nil {
				return nil, err
			}
			changedInboundIds, err := s.findInboundsChanges(tx, client, true)
			if err != nil {
				return nil, err
			}
			if len(changedInboundIds) > 0 {
				inboundIds = common.UnionUintArray(inboundIds, changedInboundIds)
			}
		}
		if len(inboundIds) > 0 {
			err = s.updateLinksWithFixedInbounds(tx, clients, hostname)
			if err != nil {
				return nil, err
			}
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, err
		}
	case "delbulk":
		var ids []uint
		err = json.Unmarshal(data, &ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			var client model.Client
			err = tx.Where("id = ?", id).First(&client).Error
			if err != nil {
				return nil, err
			}
			var clientInbounds []uint
			err = json.Unmarshal(client.Inbounds, &clientInbounds)
			if err != nil {
				return nil, err
			}
			inboundIds = common.UnionUintArray(inboundIds, clientInbounds)
		}
		err = tx.Where("id in ?", ids).Delete(model.Client{}).Error
		if err != nil {
			return nil, err
		}
	case "del":
		var id uint
		err = json.Unmarshal(data, &id)
		if err != nil {
			return nil, err
		}
		var client model.Client
		err = tx.Where("id = ?", id).First(&client).Error
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(client.Inbounds, &inboundIds)
		if err != nil {
			return nil, err
		}
		err = tx.Where("id = ?", id).Delete(model.Client{}).Error
		if err != nil {
			return nil, err
		}
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}

	return inboundIds, nil
}

func (s *ClientService) updateLinksWithFixedInbounds(tx *gorm.DB, clients []*model.Client, hostname string) error {
	if len(clients) == 0 {
		return nil
	}

	inboundIDSet := make(map[uint]struct{})
	clientInboundIDs := make([][]uint, len(clients))
	for index, client := range clients {
		inboundIDs, err := decodeClientInboundIDs(client.Inbounds)
		if err != nil {
			return err
		}
		clientInboundIDs[index] = inboundIDs
		for _, inboundID := range inboundIDs {
			inboundIDSet[inboundID] = struct{}{}
		}
	}

	inboundIDs := make([]uint, 0, len(inboundIDSet))
	for inboundID := range inboundIDSet {
		inboundIDs = append(inboundIDs, inboundID)
	}

	inboundIndex := make(map[uint]*model.Inbound, len(inboundIDs))
	if len(inboundIDs) > 0 {
		var inbounds []model.Inbound
		if err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ? and type in ?", inboundIDs, util.InboundTypeWithLink).Find(&inbounds).Error; err != nil {
			return err
		}
		for index := range inbounds {
			inbound := inbounds[index]
			inboundIndex[inbound.Id] = &inbound
		}
	}

	for index, client := range clients {
		clientLinks, err := decodeClientLinks(client.Links)
		if err != nil {
			return err
		}

		newClientLinks := make([]map[string]string, 0)
		for _, inboundID := range clientInboundIDs[index] {
			inbound := inboundIndex[inboundID]
			if inbound == nil {
				continue
			}
			newLinks := util.LinkGenerator(client.Config, inbound, hostname)
			for _, newLink := range newLinks {
				newClientLinks = append(newClientLinks, map[string]string{
					"remark": inbound.Tag,
					"type":   "local",
					"uri":    newLink,
				})
			}
		}

		for _, clientLink := range clientLinks {
			if clientLink["type"] != "local" {
				newClientLinks = append(newClientLinks, clientLink)
			}
		}

		clients[index].Links, err = encodeClientLinks(newClientLinks)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateClientsOnInboundAdd(tx *gorm.DB, initIds string, inboundId uint, hostname string) error {
	clientIds := strings.Split(initIds, ",")
	var clients []model.Client
	err := tx.Model(model.Client{}).Where("id in ?", clientIds).Find(&clients).Error
	if err != nil {
		return err
	}
	for index := range clients {
		clientInbounds, err := decodeClientInboundIDs(clients[index].Inbounds)
		if err != nil {
			return err
		}
		clientInbounds = append(clientInbounds, inboundId)
		clients[index].Inbounds, err = encodeClientInboundIDs(clientInbounds)
		if err != nil {
			return err
		}
	}
	if err := s.updateLinksWithFixedInbounds(tx, sliceClientPointers(clients), hostname); err != nil {
		return err
	}
	for index := range clients {
		err = tx.Save(&clients[index]).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error {
	clientIds, err := s.collectClientIDsByInboundID(tx, id)
	if err != nil {
		return err
	}
	if len(clientIds) == 0 {
		return nil
	}
	var clients []model.Client
	err = tx.Model(model.Client{}).Where("id IN ?", clientIds).Find(&clients).Error
	if err != nil {
		return err
	}
	for index := range clients {
		clientInbounds, err := decodeClientInboundIDs(clients[index].Inbounds)
		if err != nil {
			return err
		}
		newClientInbounds := make([]uint, 0, len(clientInbounds))
		for _, clientInbound := range clientInbounds {
			if clientInbound != id {
				newClientInbounds = append(newClientInbounds, clientInbound)
			}
		}
		clients[index].Inbounds, err = encodeClientInboundIDs(newClientInbounds)
		if err != nil {
			return err
		}
	}
	for index := range clients {
		clientLinks, err := decodeClientLinks(clients[index].Links)
		if err != nil {
			return err
		}
		clients[index].Links, err = encodeClientLinks(filterClientLinks(clientLinks, func(link map[string]string) bool {
			return link["type"] != "local" || link["remark"] != tag
		}))
		if err != nil {
			return err
		}
	}
	for index := range clients {
		err = tx.Save(&clients[index]).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error {
	for _, inbound := range *inbounds {
		clientIds, err := s.collectClientIDsByInboundID(tx, inbound.Id)
		if err != nil {
			return err
		}
		if len(clientIds) == 0 {
			continue
		}
		var clients []model.Client
		err = tx.Model(model.Client{}).Where("id IN ?", clientIds).Find(&clients).Error
		if err != nil {
			return err
		}
		if err := s.updateLinksWithFixedInbounds(tx, sliceClientPointers(clients), hostname); err != nil {
			return err
		}
		for index := range clients {
			err = tx.Save(&clients[index]).Error
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func sliceClientPointers(clients []model.Client) []*model.Client {
	if len(clients) == 0 {
		return nil
	}
	items := make([]*model.Client, 0, len(clients))
	for index := range clients {
		items = append(items, &clients[index])
	}
	return items
}

func (s *ClientService) DepleteClients() ([]uint, error) {
	var clients []model.Client
	var changes []model.Changes
	var users []string
	var inboundIds []uint

	dt := time.Now().Unix()
	err := retryWriteTx(func(tx *gorm.DB) error {
		var err error

		// Reset clients
		inboundIds, err = s.ResetClients(tx, dt)
		if err != nil {
			return err
		}

		// Deplete clients
		err = tx.Model(model.Client{}).Where("enable = true AND ((volume >0 AND up+down > volume) OR (expiry > 0 AND expiry < ?))", dt).Scan(&clients).Error
		if err != nil {
			return err
		}

		for _, client := range clients {
			logger.Debug("Client ", client.Name, " is going to be disabled")
			users = append(users, client.Name)
			var userInbounds []uint
			json.Unmarshal(client.Inbounds, &userInbounds)
			inboundIds = common.UnionUintArray(inboundIds, userInbounds)
			changes = append(changes, model.Changes{
				DateTime: dt,
				Actor:    "DepleteJob",
				Key:      "clients",
				Action:   "disable",
				Obj:      json.RawMessage("\"" + client.Name + "\""),
			})
		}

		if len(changes) > 0 {
			err = tx.Model(model.Client{}).Where("enable = true AND ((volume >0 AND up+down > volume) OR (expiry > 0 AND expiry < ?))", dt).Update("enable", false).Error
			if err != nil {
				return err
			}
			err = tx.Model(model.Changes{}).Create(&changes).Error
			if err != nil {
				return err
			}
			LastUpdate = dt
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := retryWrite(func(db *gorm.DB) error {
		return db.Exec("PRAGMA wal_checkpoint(FULL)").Error
	}); err != nil {
		logger.Error("Error checkpointing WAL: ", err.Error())
	}

	return inboundIds, nil
}

func (s *ClientService) ResetClients(tx *gorm.DB, dt int64) ([]uint, error) {
	var err error
	var resetClients, allClients []*model.Client
	var changes []model.Changes
	var inboundIds []uint
	// Set delay start without periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = false AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.Expiry = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set delay start with periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = true AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set periodic reset
	err = tx.Model(model.Client{}).
		Where("delay_start = false AND auto_reset = true AND next_reset < ?", dt).Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.TotalUp += client.Up
		client.TotalDown += client.Down
		client.Up = 0
		client.Down = 0
		if !client.Enable {
			client.Enable = true
			var clientInboundIds []uint
			json.Unmarshal(client.Inbounds, &clientInboundIds)
			inboundIds = common.UnionUintArray(inboundIds, clientInboundIds)
		}
	}
	allClients = append(allClients, resetClients...)

	// Save clients
	if len(allClients) > 0 {
		err = tx.Save(allClients).Error
		if err != nil {
			return nil, err
		}
	}

	// Save changes
	if len(changes) > 0 {
		err = tx.Model(model.Changes{}).Create(&changes).Error
		if err != nil {
			return nil, err
		}
		LastUpdate = dt
	}
	return inboundIds, nil
}

func (s *ClientService) findInboundsChanges(tx *gorm.DB, client *model.Client, fillOmitted bool) ([]uint, error) {
	var err error
	var oldClient model.Client
	var oldInboundIds, newInboundIds []uint
	err = tx.Model(model.Client{}).Where("id = ?", client.Id).First(&oldClient).Error
	if err != nil {
		return nil, err
	}
	if fillOmitted {
		client.Links = oldClient.Links
		client.Config = oldClient.Config
	}
	err = json.Unmarshal(oldClient.Inbounds, &oldInboundIds)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(client.Inbounds, &newInboundIds)
	if err != nil {
		return nil, err
	}

	// Check client.Config changes
	if !bytes.Equal(oldClient.Config, client.Config) ||
		oldClient.Name != client.Name ||
		oldClient.Enable != client.Enable {
		return common.UnionUintArray(oldInboundIds, newInboundIds), nil
	}

	// Check client.Inbounds changes
	diffInbounds := common.DiffUintArray(oldInboundIds, newInboundIds)

	return diffInbounds, nil
}

func (s *ClientService) preserveClientHistory(tx *gorm.DB, client *model.Client) error {
	if client == nil || client.Id == 0 {
		return nil
	}

	var oldClient model.Client
	err := tx.Model(model.Client{}).Select("history").Where("id = ?", client.Id).First(&oldClient).Error
	if err != nil {
		return err
	}
	client.History = oldClient.History
	return nil
}
