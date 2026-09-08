package cronjob

import (
	"time"

	"github.com/robfig/cron/v3"
)

type CronJob struct {
	cron *cron.Cron
}

func NewCronJob() *CronJob {
	return &CronJob{}
}

func (c *CronJob) Start(loc *time.Location, trafficAge int) error {
	c.cron = cron.New(cron.WithLocation(loc), cron.WithSeconds())
	jobs := []struct {
		spec string
		job  cron.Job
	}{
		{"@every 10s", NewStatsJob(trafficAge > 0)},
		{"@every 1m", NewDepleteJob()},
		{"@every 5s", NewCheckCoreJob()},
		{"@every 10m", NewWALCheckpointJob()},
		{"@every 1m", NewAlertJob()},
	}
	if trafficAge > 0 {
		jobs = append(jobs, struct {
			spec string
			job  cron.Job
		}{"@daily", NewDelStatsJob(trafficAge)})
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
		c.cron.Stop()
	}
}
