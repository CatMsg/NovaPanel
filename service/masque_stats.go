package service

import (
	"math"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
)

func masqueTrafficValue(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func (s *MasqueService) CollectStats() ([]model.Stats, onlines, error) {
	if s == nil {
		return nil, onlines{}, nil
	}
	s.mu.Lock()
	runtimes := make([]*masqueRuntime, 0, len(s.runtimes))
	for _, runtime := range s.runtimes {
		runtimes = append(runtimes, runtime)
	}
	s.mu.Unlock()

	now := time.Now()
	current := make(map[string]masqueTrafficSnapshot)
	online := onlines{}
	for _, runtime := range runtimes {
		if runtime == nil || !runtime.running.Load() {
			continue
		}
		runtime.sessionMu.Lock()
		if len(runtime.sessions) > 0 {
			online.Inbound = append(online.Inbound, runtime.tag)
		}
		for username := range runtime.userSessions {
			online.User = append(online.User, username)
		}
		runtime.sessionMu.Unlock()
		for username, traffic := range runtime.traffic {
			if traffic == nil {
				continue
			}
			current[runtime.tag+"\x00"+username] = masqueTrafficSnapshot{
				Upload: traffic.Upload.Load(), Download: traffic.Download.Load(),
			}
		}
	}

	stats := make([]model.Stats, 0, len(current)*4)
	inboundDeltas := make(map[string]masqueTrafficSnapshot)
	s.mu.Lock()
	if s.trafficBaseline == nil {
		s.trafficBaseline = make(map[string]masqueTrafficSnapshot)
	}
	for key, counter := range current {
		previous := s.trafficBaseline[key]
		upload := counter.Upload
		if counter.Upload >= previous.Upload {
			upload = counter.Upload - previous.Upload
		}
		download := counter.Download
		if counter.Download >= previous.Download {
			download = counter.Download - previous.Download
		}
		s.trafficBaseline[key] = counter
		separator := len(key)
		for index := range key {
			if key[index] == 0 {
				separator = index
				break
			}
		}
		if separator == len(key) {
			continue
		}
		inboundTag, username := key[:separator], key[separator+1:]
		if upload > 0 {
			stats = append(stats, model.Stats{DateTime: now.Unix(), Resource: "user", Tag: username, Direction: true, Traffic: masqueTrafficValue(upload)})
		}
		if download > 0 {
			stats = append(stats, model.Stats{DateTime: now.Unix(), Resource: "user", Tag: username, Direction: false, Traffic: masqueTrafficValue(download)})
		}
		total := inboundDeltas[inboundTag]
		total.Upload += upload
		total.Download += download
		inboundDeltas[inboundTag] = total
	}
	for key := range s.trafficBaseline {
		if _, exists := current[key]; !exists {
			delete(s.trafficBaseline, key)
		}
	}
	s.mu.Unlock()
	for tag, delta := range inboundDeltas {
		if delta.Upload > 0 {
			stats = append(stats, model.Stats{DateTime: now.Unix(), Resource: "inbound", Tag: tag, Direction: true, Traffic: masqueTrafficValue(delta.Upload)})
		}
		if delta.Download > 0 {
			stats = append(stats, model.Stats{DateTime: now.Unix(), Resource: "inbound", Tag: tag, Direction: false, Traffic: masqueTrafficValue(delta.Download)})
		}
	}
	return stats, online, nil
}
