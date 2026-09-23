package service

import (
	"sync/atomic"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

const (
	trafficBudgetSampleInterval = 5 * time.Minute
	trafficBudgetHistoryDays    = 90
	trafficBudgetHistoryPoints  = 300
)

var trafficBudgetHistoryPruneDay atomic.Int64

type TrafficBudgetHistory struct {
	PeriodStart string                      `json:"periodStart,omitempty"`
	PeriodEnd   string                      `json:"periodEnd,omitempty"`
	Samples     []model.TrafficBudgetSample `json:"samples"`
}

func (s *TrafficBudgetService) recordTrafficBudgetSample(status TrafficBudgetStatus, periodStart time.Time, sampledAt time.Time) error {
	if !status.Enabled || !status.Supported || periodStart.IsZero() {
		return nil
	}
	bucket := sampledAt.Truncate(trafficBudgetSampleInterval).Unix()
	sample := model.TrafficBudgetSample{
		DateTime:        bucket,
		PeriodStart:     periodStart.Unix(),
		MeteredRxBytes:  status.MeteredRxBytes,
		MeteredTxBytes:  status.MeteredTxBytes,
		UsedBytes:       status.UsedBytes,
		ClientPoolBytes: status.ClientPoolBytes,
		Level:           status.Level,
	}
	if err := database.WithRetryTx(3, 50*time.Millisecond, func(tx *gorm.DB) error {
		return tx.Where("period_start = ? AND date_time = ?", sample.PeriodStart, sample.DateTime).
			Assign(sample).
			FirstOrCreate(&model.TrafficBudgetSample{}).Error
	}); err != nil {
		return err
	}

	day := sampledAt.Unix() / int64(24*time.Hour/time.Second)
	previousDay := trafficBudgetHistoryPruneDay.Load()
	if previousDay == day || !trafficBudgetHistoryPruneDay.CompareAndSwap(previousDay, day) {
		return nil
	}
	cutoff := sampledAt.AddDate(0, 0, -trafficBudgetHistoryDays).Unix()
	return database.GetDB().Where("date_time < ?", cutoff).Delete(&model.TrafficBudgetSample{}).Error
}

func (s *TrafficBudgetService) GetHistory() (*TrafficBudgetHistory, error) {
	status := s.GetStatus()
	history := &TrafficBudgetHistory{
		PeriodStart: status.PeriodStart,
		PeriodEnd:   status.PeriodEnd,
		Samples:     make([]model.TrafficBudgetSample, 0),
	}
	if !status.Enabled || !status.Supported || status.PeriodStart == "" {
		return history, nil
	}
	periodStart, err := time.Parse(time.RFC3339, status.PeriodStart)
	if err != nil {
		return nil, err
	}
	var samples []model.TrafficBudgetSample
	if err := database.GetDB().
		Where("period_start = ?", periodStart.Unix()).
		Order("date_time ASC").
		Find(&samples).Error; err != nil {
		return nil, err
	}
	history.Samples = downsampleTrafficBudgetSamples(samples, trafficBudgetHistoryPoints)
	return history, nil
}

func downsampleTrafficBudgetSamples(samples []model.TrafficBudgetSample, maxPoints int) []model.TrafficBudgetSample {
	if maxPoints < 1 || len(samples) <= maxPoints {
		return samples
	}
	result := make([]model.TrafficBudgetSample, 0, maxPoints)
	for i := 0; i < maxPoints; i++ {
		start := i * len(samples) / maxPoints
		end := (i + 1) * len(samples) / maxPoints
		if end <= start {
			end = start + 1
		}
		result = append(result, samples[end-1])
	}
	return result
}
