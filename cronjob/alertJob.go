package cronjob

import (
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
)

type AlertJob struct{ service.AlertService }

func NewAlertJob() *AlertJob { return &AlertJob{} }

func (s *AlertJob) Run() {
	if err := s.EvaluateAndNotify(); err != nil {
		logger.Warning("alert notification failed:", err)
	}
}
