package sub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func clientInboundIDs(client *model.Client) ([]uint, error) {
	if client == nil {
		return nil, fmt.Errorf("subscription client is missing")
	}
	raw := bytes.TrimSpace(client.Inbounds)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var ids []uint
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("decode subscription inbound IDs: %w", err)
	}
	return ids, nil
}

func visibleSubscriptionInbounds(client *model.Client) ([]*model.Inbound, error) {
	ids, err := clientInboundIDs(client)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	var inbounds []*model.Inbound
	if err := database.GetDB().Model(model.Inbound{}).Preload("Tls").Where("id in ?", ids).Find(&inbounds).Error; err != nil {
		return nil, err
	}
	visible := make([]*model.Inbound, 0, len(inbounds))
	for _, inbound := range inbounds {
		if inbound != nil && !inbound.ExcludeFromSub {
			visible = append(visible, inbound)
		}
	}
	return visible, nil
}

func subscriptionVisibilityCacheKey(client *model.Client) (string, error) {
	ids, err := clientInboundIDs(client)
	if err != nil || len(ids) == 0 {
		return "", err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var inbounds []model.Inbound
	if err := database.GetDB().Model(model.Inbound{}).
		Select("id", "exclude_from_sub").Where("id in ?", ids).Order("id").Find(&inbounds).Error; err != nil {
		return "", err
	}
	var key strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&key, "%d,", id)
	}
	key.WriteByte(':')
	for _, inbound := range inbounds {
		fmt.Fprintf(&key, "%d=%t;", inbound.Id, inbound.ExcludeFromSub)
	}
	return key.String(), nil
}

func filterExcludedInboundLinks(client *model.Client) error {
	ids, err := clientInboundIDs(client)
	if err != nil || len(ids) == 0 || len(client.Links) == 0 {
		return err
	}
	var assigned []model.Inbound
	if err := database.GetDB().Model(model.Inbound{}).
		Select("tag", "exclude_from_sub").Where("id in ?", ids).Find(&assigned).Error; err != nil {
		return err
	}
	hiddenTags := make(map[string]struct{})
	assignedTags := make(map[string]struct{}, len(assigned))
	for _, inbound := range assigned {
		assignedTags[inbound.Tag] = struct{}{}
		if inbound.ExcludeFromSub {
			hiddenTags[inbound.Tag] = struct{}{}
		}
	}
	if len(hiddenTags) == 0 {
		return nil
	}

	var links []Link
	if err := json.Unmarshal(client.Links, &links); err != nil {
		return fmt.Errorf("decode subscription links: %w", err)
	}
	visible := make([]Link, 0, len(links))
	for _, link := range links {
		if link.Type == "local" {
			if _, excluded := hiddenTags[link.Remark]; excluded {
				continue
			}
			if _, known := assignedTags[link.Remark]; !known {
				continue
			}
		}
		visible = append(visible, link)
	}
	client.Links, err = json.Marshal(visible)
	return err
}
