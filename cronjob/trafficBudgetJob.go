package cronjob

import (
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
)

type TrafficBudgetJob struct{}

func NewTrafficBudgetJob() *TrafficBudgetJob { return &TrafficBudgetJob{} }

func (s *TrafficBudgetJob) Run() {
	if err := service.GetTrafficBudgetService().CheckAndEnforce(); err != nil {
		logger.Warning("VPS traffic budget check failed: ", err)
	}
}
