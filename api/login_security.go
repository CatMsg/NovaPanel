package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-gonic/gin"
)

const (
	loginFailureWindow              = 10 * time.Minute
	loginFailureLimit               = 3
	loginFailureMapCap              = 4096
	maxConcurrentLoginVerifications = 4
	loginRequestBodyReadTimeout     = 15 * time.Second
)

type loginVerificationLimiter struct {
	slots chan struct{}
}

func newLoginVerificationLimiter(limit int) *loginVerificationLimiter {
	if limit < 1 {
		panic("login verification limit must be positive")
	}
	return &loginVerificationLimiter{slots: make(chan struct{}, limit)}
}

func (l *loginVerificationLimiter) tryAcquire() (func(), bool) {
	select {
	case l.slots <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() { <-l.slots })
		}, true
	default:
		return nil, false
	}
}

var loginVerificationGate = newLoginVerificationLimiter(maxConcurrentLoginVerifications)

func LoginRequestBodyDeadlineMiddleware() gin.HandlerFunc {
	return loginRequestBodyDeadlineMiddleware(loginRequestBodyReadTimeout)
}

func loginRequestBodyDeadlineMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := strings.TrimRight(c.Request.URL.Path, "/")
		if c.Request.Method != http.MethodPost || !strings.HasSuffix(path, "/api/login") {
			c.Next()
			return
		}

		controller := http.NewResponseController(c.Writer)
		if err := controller.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			logger.Warning("set login request read deadline failed: ", err)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, Msg{Success: false, Msg: "login temporarily unavailable"})
			return
		}
		defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
		c.Next()
	}
}

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
