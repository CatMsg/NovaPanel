package service

import (
	"errors"
	"sort"
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
	var err error
	var result []model.Stats

	currentTime := time.Now().Unix()
	timeDiff := currentTime - (int64(limit) * 3600)

	db := database.GetDB()
	resources := []string{resource}
	if resource == "endpoint" {
		resources = []string{"inbound", "outbound"}
	}
	err = db.Model(model.Stats{}).Where("resource in ? AND tag = ? AND date_time > ?", resources, tag, timeDiff).Scan(&result).Error
	if err != nil {
		return nil, err
	}

	result = s.downsampleStats(result, 60) // 60 rows for 30 buckets
	return result, nil
}

// downsampleStats reduces stats to maxRows rows.
// Each bucket outputs two rows (direction false and true) with average Traffic.
func (s *StatsService) downsampleStats(stats []model.Stats, maxRows int) []model.Stats {
	if len(stats) <= maxRows {
		return stats
	}
	numBuckets := int(maxRows / 2)
	sort.Slice(stats, func(i, j int) bool { return stats[i].DateTime < stats[j].DateTime })
	timeMin, timeMax := stats[0].DateTime, stats[len(stats)-1].DateTime
	bucketSpan := (timeMax - timeMin) / int64(numBuckets)
	if bucketSpan == 0 {
		bucketSpan = 1
	}
	downsampled := make([]model.Stats, 0, maxRows)
	for i := 0; i < numBuckets; i++ {
		bucketStart := timeMin + int64(i)*bucketSpan
		bucketEnd := timeMin + int64(i+1)*bucketSpan
		if i == numBuckets-1 {
			bucketEnd = timeMax + 1
		}
		for _, dir := range []bool{false, true} {
			var sum int64
			var count int
			for _, r := range stats {
				if r.DateTime >= bucketStart && r.DateTime < bucketEnd && r.Direction == dir {
					sum += r.Traffic
					count++
				}
			}
			avg := int64(0)
			if count > 0 {
				avg = sum / int64(count)
			}
			downsampled = append(downsampled, model.Stats{
				DateTime:  bucketStart,
				Resource:  stats[0].Resource,
				Tag:       stats[0].Tag,
				Direction: dir,
				Traffic:   avg,
			})
		}
	}
	return downsampled
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
