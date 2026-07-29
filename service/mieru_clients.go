package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
)

func normalizeMieruClientConfig(client *model.Client) error {
	if client == nil {
		return nil
	}
	configs := make(map[string]interface{})
	if len(client.Config) > 0 {
		if err := json.Unmarshal(client.Config, &configs); err != nil {
			return fmt.Errorf("parse client %s config: %w", client.Name, err)
		}
	}
	mieru, _ := configs["mieru"].(map[string]interface{})
	if mieru == nil {
		mieru = make(map[string]interface{})
	}
	mieru["name"] = strings.TrimSpace(client.Name)
	if strings.TrimSpace(fmt.Sprint(mieru["password"])) == "" || fmt.Sprint(mieru["password"]) == "<nil>" {
		password, err := newMieruPassword()
		if err != nil {
			return err
		}
		mieru["password"] = password
	}
	configs["mieru"] = mieru

	payload, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return err
	}
	client.Config = payload
	return nil
}

func newMieruPassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate Mieru password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
