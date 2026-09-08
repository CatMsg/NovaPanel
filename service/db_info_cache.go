package service

import "time"

const databaseInfoCacheTTL = 3 * time.Second

var databaseInfoCache = newTimedCache()

func cloneDatabaseInfo(src map[string]int64) map[string]int64 {
	if src == nil {
		return nil
	}
	cloned := make(map[string]int64, len(src))
	for k, v := range src {
		cloned[k] = v
	}
	return cloned
}
