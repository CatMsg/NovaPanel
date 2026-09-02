package cronjob

import (
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
)

type CheckCoreJob struct {
	service.ConfigService
}

func NewCheckCoreJob() *CheckCoreJob {
	return &CheckCoreJob{}
}

func (s *CheckCoreJob) Run() {
	if err := s.ConfigService.StartCore(); err != nil {
		logger.Warning("start core from scheduled check failed: ", err)
	}
}
