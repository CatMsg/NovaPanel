package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
)

func (s *FleetService) GetFleetTrafficBudgetHistory(id string) (*TrafficBudgetHistory, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "local" {
		return GetTrafficBudgetService().GetHistory()
	}
	configs, err := s.loadFleetServers()
	if err != nil {
		return nil, err
	}
	for _, config := range configs {
		if config.ID != id {
			continue
		}
		if !config.Enabled {
			return nil, fmt.Errorf("服务器已停用")
		}
		token, err := s.decryptFleetToken(config.TokenEnc)
		if err != nil {
			return nil, fmt.Errorf("令牌解密失败: %w", err)
		}
		response, err := s.fetchFleetAPI(config.URL, token, "traffic-budget-history", "")
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(response.Obj)
		if err != nil {
			return nil, err
		}
		var history TrafficBudgetHistory
		if err := json.Unmarshal(raw, &history); err != nil {
			return nil, err
		}
		if history.Samples == nil {
			history.Samples = make([]model.TrafficBudgetSample, 0)
		}
		return &history, nil
	}
	return nil, fmt.Errorf("服务器不存在: %s", id)
}
