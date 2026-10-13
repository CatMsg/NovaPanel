package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/sagernet/sing/common/auth"
	"gorm.io/gorm"
)

type masqueClientCredential struct {
	Username string                            `json:"username"`
	Password string                            `json:"password"`
	Mihomo   map[string]masqueMihomoCredential `json:"mihomo,omitempty"`
}

func decodeMasqueCredential(raw json.RawMessage) (masqueClientCredential, map[string]json.RawMessage, error) {
	root := make(map[string]json.RawMessage)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &root); err != nil {
			return masqueClientCredential{}, nil, err
		}
	}
	if root == nil {
		root = make(map[string]json.RawMessage)
	}
	var credential masqueClientCredential
	if value := root["masque"]; len(value) > 0 {
		if err := json.Unmarshal(value, &credential); err != nil {
			return credential, nil, err
		}
	}
	return credential, root, nil
}

func normalizeMasqueClientConfigs(tx *gorm.DB, clients []*model.Client) error {
	for _, client := range clients {
		ids, err := decodeClientInboundIDs(client.Inbounds)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.Inbound{}).Where("id IN ? AND type = ?", ids, "masque").Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		credential, root, err := decodeMasqueCredential(client.Config)
		if err != nil {
			return err
		}
		if strings.TrimSpace(client.Name) == "" {
			return fmt.Errorf("MASQUE client name is required")
		}
		credential.Username = client.Name
		if credential.Password == "" {
			var secret [32]byte
			if _, err := rand.Read(secret[:]); err != nil {
				return err
			}
			credential.Password = base64.RawURLEncoding.EncodeToString(secret[:])
		}
		root["masque"], err = json.Marshal(credential)
		if err != nil {
			return err
		}
		client.Config, err = json.MarshalIndent(root, "", "  ")
		if err != nil {
			return err
		}
	}
	return normalizeMasqueMihomo(tx, clients)
}

func loadMasqueUsers(tx *gorm.DB, inbound *model.Inbound) ([]auth.User, error) {
	var clients []model.Client
	if err := tx.Where("enable = true AND ? IN (SELECT json_each.value FROM json_each(clients.inbounds))", inbound.Id).Order("id").Find(&clients).Error; err != nil {
		return nil, err
	}
	users := make([]auth.User, 0, len(clients))
	now := time.Now().Unix()
	for _, client := range clients {
		if (client.Volume > 0 && client.Up+client.Down >= client.Volume) || (client.Expiry > 0 && client.Expiry <= now) {
			continue
		}
		credential, _, err := decodeMasqueCredential(client.Config)
		if err != nil {
			return nil, err
		}
		if credential.Username != client.Name || credential.Password == "" {
			return nil, fmt.Errorf("MASQUE credentials missing or mismatched for client %s", client.Name)
		}
		users = append(users, auth.User{Username: client.Name, Password: credential.Password})
	}
	return users, nil
}
