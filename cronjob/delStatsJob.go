package cronjob

import (
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
)

type DelStatsJob struct {
	service.StatsService
	retentionDays int
}

func NewDelStatsJob(retentionDays int) *DelStatsJob {
	return &DelStatsJob{
		retentionDays: retentionDays,
	}
}

func (s *DelStatsJob) Run() {
	err := s.StatsService.DelOldStats(s.retentionDays)
	if err != nil {
		logger.Warning("Deleting old statistics failed: ", err)
		return
	}
	logger.Debug("Stats older than ", s.retentionDays, " days were deleted")
}
