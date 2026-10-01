package cronjob

import (
	"time"

	"github.com/robfig/cron/v3"
)

type CronJob struct {
	cron *cron.Cron
}

// Keep chart history bounded without exposing its retention as a setting.
const trafficStatsRetentionDays = 30

func NewCronJob() *CronJob {
	return &CronJob{}
}

func (c *CronJob) Start(loc *time.Location) error {
	c.cron = cron.New(cron.WithLocation(loc), cron.WithSeconds())
	jobs := []struct {
		spec string
		job  cron.Job
	}{
		{"@every 10s", NewStatsJob(true)},
		{"@every 10s", NewTrafficBudgetJob()},
		{"@every 1m", NewDepleteJob()},
		{"@every 5s", NewCheckCoreJob()},
		{"@every 10m", NewWALCheckpointJob()},
		{"@every 1m", NewAlertJob()},
		{"@daily", NewDelStatsJob(trafficStatsRetentionDays)},
	}
	for _, scheduled := range jobs {
		if _, err := c.cron.AddJob(scheduled.spec, scheduled.job); err != nil {
			return err
		}
	}
	c.cron.Start()

	return nil
}

func (c *CronJob) Stop() {
	if c.cron != nil {
		ctx := c.cron.Stop()
		<-ctx.Done()
	}
}
