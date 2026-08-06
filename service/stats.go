package service

import (
	"errors"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"

	"gorm.io/gorm"
)

type onlines struct {
	Inbound  []string `json:"inbound,omitempty"`
	User     []string `json:"user,omitempty"`
	Outbound []string `json:"outbound,omitempty"`
}

var onlineResources = &onlines{}
var onlineResourcesMu sync.RWMutex

type StatsService struct {
}

func (s *StatsService) SaveStats(enableTraffic bool) error {
	stats := make([]model.Stats, 0)
	if corePtr != nil && corePtr.IsRunning() {
		box := corePtr.GetInstance()
		if box != nil && box.StatsTracker() != nil {
			coreStats := box.StatsTracker().GetStats()
			if coreStats != nil {
				stats = append(stats, (*coreStats)...)
			}
		}
	}

	sampled := onlines{}
	var collectionErr error
	if mieru := GetMieruService(); mieru != nil {
		mieruStats, mieruOnline, err := mieru.CollectStats()
		if err != nil {
			collectionErr = err
		} else {
			stats = append(stats, mieruStats...)
			sampled = mergeOnlines(sampled, mieruOnline)
		}
	}

	if len(stats) == 0 {
		onlineResourcesMu.Lock()
		*onlineResources = sampled
		onlineResourcesMu.Unlock()
		return collectionErr
	}

	err := retryWriteTx(func(tx *gorm.DB) error {
		for _, stat := range stats {
			if stat.Resource == "user" {
				var err error
				if stat.Direction {
					err = tx.Model(model.Client{}).Where("name = ?", stat.Tag).
						UpdateColumn("up", gorm.Expr("up + ?", stat.Traffic)).Error
				} else {
					err = tx.Model(model.Client{}).Where("name = ?", stat.Tag).
						UpdateColumn("down", gorm.Expr("down + ?", stat.Traffic)).Error
				}
				if err != nil {
					return err
				}
			}
			if stat.Direction {
				switch stat.Resource {
				case "inbound":
					sampled.Inbound = append(sampled.Inbound, stat.Tag)
				case "outbound":
					sampled.Outbound = append(sampled.Outbound, stat.Tag)
				case "user":
					sampled.User = append(sampled.User, stat.Tag)
				}
			}
		}

		if !enableTraffic {
			return nil
		}
		return tx.Create(&stats).Error
	})
	if err == nil {
		onlineResourcesMu.Lock()
		*onlineResources = sampled
		onlineResourcesMu.Unlock()
	}
	return errors.Join(err, collectionErr)
}

func (s *StatsService) GetStats(resource string, tag string, limit int) ([]model.Stats, error) {
	var result []model.Stats

	currentTime := time.Now().Unix()
	timeDiff := currentTime - (int64(limit) * 3600)

	db := database.GetDB()
	resources := []string{resource}
	if resource == "endpoint" {
		resources = []string{"inbound", "outbound"}
	}
	query := db.Model(model.Stats{}).Where("resource IN ? AND tag = ? AND date_time > ?", resources, tag, timeDiff)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return nil, err
	}
	if count <= 60 {
		if err := query.Order("date_time ASC, direction ASC").Find(&result).Error; err != nil {
			return nil, err
		}
		return result, nil
	}

	var bounds struct {
		Min int64
		Max int64
	}
	if err := query.Select("MIN(date_time) AS min, MAX(date_time) AS max").Scan(&bounds).Error; err != nil {
		return nil, err
	}
	const bucketCount int64 = 30
	bucketSpan := (bounds.Max - bounds.Min) / bucketCount
	if bucketSpan == 0 {
		bucketSpan = 1
	}

	const bucketSQL = `
		SELECT 0 AS id,
		       ? + bucket * ? AS date_time,
		       MIN(resource) AS resource,
		       ? AS tag,
		       direction,
		       CAST(AVG(traffic) AS INTEGER) AS traffic
		FROM (
			SELECT resource, direction, traffic,
			       MIN(CAST((date_time - ?) / ? AS INTEGER), ?) AS bucket
			FROM stats
			WHERE resource IN ? AND tag = ? AND date_time > ?
		)
		GROUP BY bucket, direction
		ORDER BY date_time ASC, direction ASC`
	if err := db.Raw(
		bucketSQL,
		bounds.Min, bucketSpan, tag,
		bounds.Min, bucketSpan, bucketCount-1,
		resources, tag, timeDiff,
	).Scan(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *StatsService) GetOnlines() (onlines, error) {
	onlineResourcesMu.RLock()
	sampled := onlines{
		Inbound:  append([]string(nil), onlineResources.Inbound...),
		Outbound: append([]string(nil), onlineResources.Outbound...),
		User:     append([]string(nil), onlineResources.User...),
	}
	onlineResourcesMu.RUnlock()
	if corePtr != nil && corePtr.IsRunning() {
		box := corePtr.GetInstance()
		if box != nil && box.ConnTracker() != nil {
			active := box.ConnTracker().Snapshot()
			return mergeOnlines(onlines{
				Inbound:  active.Inbound,
				Outbound: active.Outbound,
				User:     active.User,
			}, sampled), nil
		}
	}
	return sampled, nil
}

func mergeOnlines(left, right onlines) onlines {
	return onlines{
		Inbound:  mergeOnlineTags(left.Inbound, right.Inbound),
		Outbound: mergeOnlineTags(left.Outbound, right.Outbound),
		User:     mergeOnlineTags(left.User, right.User),
	}
}

func mergeOnlineTags(groups ...[]string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, group := range groups {
		for _, tag := range group {
			if tag == "" {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			result = append(result, tag)
		}
	}
	return result
}
func (s *StatsService) DelOldStats(days int) error {
	oldTime := time.Now().AddDate(0, 0, -(days)).Unix()
	return retryWrite(func(db *gorm.DB) error {
		return db.Where("date_time < ?", oldTime).Delete(model.Stats{}).Error
	})
}
