package sub

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"

	"github.com/gin-gonic/gin"
)

type SubHandler struct {
	service.SettingService
	SubService
	JsonService
	ClashService
	AggregateService
	UserCenterService
}

const aggregateRequestTimeout = 60 * time.Second

var aggregateRequestSlots = make(chan struct{}, 2)

const userSubscriptionRequestTimeout = 60 * time.Second

var userSubscriptionRequestSlots = make(chan struct{}, 8)

func beginAggregateRequest(c *gin.Context) (context.Context, func(), bool) {
	select {
	case aggregateRequestSlots <- struct{}{}:
		ctx, cancel := context.WithTimeout(c.Request.Context(), aggregateRequestTimeout)
		return ctx, func() {
			cancel()
			<-aggregateRequestSlots
		}, true
	default:
		c.Header("Retry-After", "3")
		c.String(http.StatusServiceUnavailable, "Aggregate subscription is busy; retry shortly.")
		return nil, nil, false
	}
}

func beginUserSubscriptionRequest(c *gin.Context) (context.Context, func(), bool) {
	select {
	case userSubscriptionRequestSlots <- struct{}{}:
		ctx, cancel := context.WithTimeout(c.Request.Context(), userSubscriptionRequestTimeout)
		return ctx, func() {
			cancel()
			<-userSubscriptionRequestSlots
		}, true
	default:
		c.Header("Retry-After", "3")
		c.String(http.StatusServiceUnavailable, "Subscription service is busy; retry shortly.")
		return nil, nil, false
	}
}

func NewSubHandler(g *gin.RouterGroup) {
	a := &SubHandler{}
	a.initRouter(g)
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
	g.Use(func(c *gin.Context) {
		// Subscription responses contain client credentials and must not be
		// retained by shared caches or forwarded as a referrer.
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
	})
	g.GET("/aggregate", s.aggregate)
	g.HEAD("/aggregate", s.aggregateHeaders)
	g.GET("/:subid", s.subs)
	g.HEAD("/:subid", s.subHeaders)
}

func (s *SubHandler) aggregate(c *gin.Context) {
	ctx, release, ok := beginAggregateRequest(c)
	if !ok {
		return
	}
	defer release()

	format, _, negotiated := requestedSubscriptionFormat(c)
	if negotiated {
		c.Header("Vary", "User-Agent")
	}
	result, headers, err := s.AggregateService.GetAggregateContext(ctx, format, requestHost(c))
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil || result == nil {
		logger.Error(err)
		if errors.Is(err, context.DeadlineExceeded) {
			c.Header("Retry-After", "5")
			c.String(http.StatusServiceUnavailable, "Aggregate subscription timed out; retry shortly.")
			return
		}
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.String(200, *result)
}

func (s *SubHandler) aggregateHeaders(c *gin.Context) {
	ctx, release, ok := beginAggregateRequest(c)
	if !ok {
		return
	}
	defer release()

	mode, err := s.SettingService.GetSubMode()
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}
	if mode != "master" {
		c.String(404, "")
		return
	}
	format, _, negotiated := requestedSubscriptionFormat(c)
	if negotiated {
		c.Header("Vary", "User-Agent")
	}
	var usage aggregateUsage
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "clash":
		_, usage, err = s.AggregateService.collectAggregateClashProxiesContext(ctx, requestHost(c))
	case "json":
		_, _, usage, err = s.AggregateService.collectAggregateJSONOutboundsContext(ctx, requestHost(c))
	default:
		_, usage, err = s.AggregateService.collectAggregateLinksContext(ctx, requestHost(c))
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		logger.Error(err)
		if errors.Is(err, context.DeadlineExceeded) {
			c.Header("Retry-After", "5")
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.String(400, "Error!")
		return
	}
	s.addHeaders(c, s.AggregateService.aggregateHeaders(usage))
	c.Status(200)
}

func (s *SubHandler) subs(c *gin.Context) {
	subId := c.Param("subid")
	format, isFormat, negotiated := requestedUserSubscriptionFormat(c)
	if negotiated {
		c.Header("Vary", "User-Agent, Accept")
	}
	if isFormat && (format == "info" || format == "html") {
		s.userCenter(c, subId)
		return
	}
	ctx, release, ok := beginUserSubscriptionRequest(c)
	if !ok {
		return
	}
	defer release()

	var headers []string
	var result *string
	var err error
	if isFormat {
		switch format {
		case "json":
			result, headers, err = s.JsonService.GetJsonContext(ctx, subId, format)
		case "clash":
			result, headers, err = s.ClashService.GetClashContext(ctx, subId, requestHost(c))
		case "raw":
			result, headers, err = s.SubService.GetSubsContext(ctx, subId)
		}
	} else {
		result, headers, err = s.SubService.GetSubsContext(ctx, subId)
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil || result == nil {
		logger.Error(err)
		if errors.Is(err, context.DeadlineExceeded) {
			c.Header("Retry-After", "5")
			c.String(http.StatusServiceUnavailable, "Subscription request timed out; retry shortly.")
			return
		}
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)

	c.String(200, *result)
}

func (s *SubHandler) userCenter(c *gin.Context, subID string) {
	view, headers, err := s.UserCenterService.Build(subID, time.Now())
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	var body bytes.Buffer
	if err := renderUserCenter(&body, view); err != nil {
		logger.Error(err)
		c.String(500, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "text/html; charset=utf-8", body.Bytes())
}

func requestedSubscriptionFormat(c *gin.Context) (format string, selected bool, negotiated bool) {
	if value, exists := c.GetQuery("format"); exists {
		return strings.ToLower(strings.TrimSpace(value)), true, false
	}
	if isClashSubscriptionClient(c.GetHeader("User-Agent")) {
		return "clash", true, true
	}
	return "", false, false
}

func requestedUserSubscriptionFormat(c *gin.Context) (format string, selected bool, negotiated bool) {
	format, selected, negotiated = requestedSubscriptionFormat(c)
	if selected {
		return format, selected, negotiated
	}
	if acceptsHTML(c.GetHeader("Accept")) {
		return "info", true, true
	}
	return format, selected, true
}

func acceptsHTML(accept string) bool {
	for _, mediaRange := range strings.Split(strings.ToLower(accept), ",") {
		parts := strings.Split(mediaRange, ";")
		mediaType := strings.TrimSpace(parts[0])
		if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
			quality := 1.0
			for _, parameter := range parts[1:] {
				keyValue := strings.SplitN(strings.TrimSpace(parameter), "=", 2)
				if len(keyValue) == 2 && strings.TrimSpace(keyValue[0]) == "q" {
					if parsed, err := strconv.ParseFloat(strings.TrimSpace(keyValue[1]), 64); err == nil {
						quality = parsed
					}
				}
			}
			if quality > 0 {
				return true
			}
		}
	}
	return false
}

func isClashSubscriptionClient(userAgent string) bool {
	userAgent = strings.ToLower(strings.TrimSpace(userAgent))
	if userAgent == "" {
		return false
	}
	for _, marker := range []string{"clash", "mihomo", "openclash", "shadowrocket", "stash"} {
		if strings.Contains(userAgent, marker) {
			return true
		}
	}
	return false
}

func (s *SubHandler) subHeaders(c *gin.Context) {
	subId := c.Param("subid")
	client, err := s.SubService.getClientBySubId(subId)
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	headers := s.SubService.getClientHeaders(client)
	s.addHeaders(c, headers)

	c.Status(200)
}

func (s *SubHandler) addHeaders(c *gin.Context, headers []string) {
	keys := []string{"Subscription-Userinfo", "Profile-Update-Interval", "Profile-Title"}
	for index, key := range keys {
		if index < len(headers) && strings.TrimSpace(headers[index]) != "" {
			c.Writer.Header().Set(key, headers[index])
		}
	}
}

func requestHost(c *gin.Context) string {
	host := c.Request.Host
	if strings.Contains(host, ":") {
		host, _, _ = net.SplitHostPort(host)
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return host
}
