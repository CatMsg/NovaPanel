package api

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
)

const (
	loginFailureWindow = 10 * time.Minute
	loginFailureLimit  = 10
	loginFailureMapCap = 4096
)

type loginFailureState struct {
	attempts []time.Time
	notified bool
}

var loginFailures = struct {
	sync.Mutex
	entries map[string]loginFailureState
}{entries: make(map[string]loginFailureState)}

func recordLoginFailure(remoteIP string, alerts *service.AlertService) {
	now := time.Now()
	cutoff := now.Add(-loginFailureWindow)
	loginFailures.Lock()
	for ip, state := range loginFailures.entries {
		firstCurrent := 0
		for firstCurrent < len(state.attempts) && state.attempts[firstCurrent].Before(cutoff) {
			firstCurrent++
		}
		if firstCurrent == len(state.attempts) {
			delete(loginFailures.entries, ip)
			continue
		}
		if firstCurrent > 0 {
			state.attempts = append([]time.Time(nil), state.attempts[firstCurrent:]...)
			loginFailures.entries[ip] = state
		}
	}
	if len(loginFailures.entries) >= loginFailureMapCap {
		for ip := range loginFailures.entries {
			delete(loginFailures.entries, ip)
			break
		}
	}
	state := loginFailures.entries[remoteIP]
	state.attempts = append(state.attempts, now)
	if len(state.attempts) > loginFailureLimit {
		state.attempts = state.attempts[len(state.attempts)-loginFailureLimit:]
	}
	shouldNotify := len(state.attempts) >= loginFailureLimit && !state.notified
	if shouldNotify {
		state.notified = true
	}
	loginFailures.entries[remoteIP] = state
	loginFailures.Unlock()

	logger.AuditWarningf("NOVAS_LOGIN_FAILED remote_ip=%s", remoteIP)
	if shouldNotify && alerts != nil {
		go func() {
			if err := alerts.NotifyLoginBan(remoteIP); err != nil {
				logger.Warning("login protection alert failed: ", err)
			}
		}()
	}
}

func clearLoginFailures(remoteIP string) {
	remoteIP = strings.TrimSpace(remoteIP)
	if ip := net.ParseIP(remoteIP); ip != nil {
		remoteIP = ip.String()
	}
	loginFailures.Lock()
	delete(loginFailures.entries, remoteIP)
	loginFailures.Unlock()
}
