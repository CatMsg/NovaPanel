package sub

import (
	"bytes"
	"net"
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

func NewSubHandler(g *gin.RouterGroup) {
	a := &SubHandler{}
	a.initRouter(g)
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
	g.GET("/aggregate", s.aggregate)
	g.HEAD("/aggregate", s.aggregateHeaders)
	g.GET("/endpoints/aggregate", s.endpointAggregate)
	g.HEAD("/endpoints/aggregate", s.endpointAggregateHeaders)
	g.GET("/endpoints", s.endpointSource)
	g.HEAD("/endpoints", s.endpointSourceHeaders)
	g.GET("/:subid", s.subs)
	g.HEAD("/:subid", s.subHeaders)
}

func (s *SubHandler) aggregate(c *gin.Context) {
	format, _, negotiated := requestedSubscriptionFormat(c)
	if negotiated {
		c.Header("Vary", "User-Agent")
	}
	result, headers, err := s.AggregateService.GetAggregate(format, requestHost(c))
	if err != nil || result == nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.String(200, *result)
}

func (s *SubHandler) aggregateHeaders(c *gin.Context) {
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
	if format == "clash" {
		_, usage, err = s.AggregateService.collectAggregateClashProxies(requestHost(c))
	} else {
		_, usage, err = s.AggregateService.collectAggregateLinks(requestHost(c))
	}
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}
	s.addHeaders(c, s.AggregateService.aggregateHeaders(usage))
	c.Status(200)
}

func (s *SubHandler) endpointAggregate(c *gin.Context) {
	format := c.Query("format")
	result, headers, err := s.AggregateService.GetEndpointAggregate(format, requestHost(c))
	if err != nil || result == nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.String(200, *result)
}

func (s *SubHandler) endpointAggregateHeaders(c *gin.Context) {
	mode, err := s.SettingService.GetEndpointMode()
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}
	if mode != "master" {
		c.String(404, "")
		return
	}
	s.addHeaders(c, s.AggregateService.endpointAggregateHeaders())
	c.Status(200)
}

func (s *SubHandler) endpointSource(c *gin.Context) {
	format := c.Query("format")
	result, headers, err := s.AggregateService.GetEndpointSource(format, requestHost(c))
	if err != nil || result == nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.String(200, *result)
}

func (s *SubHandler) endpointSourceHeaders(c *gin.Context) {
	s.addHeaders(c, s.AggregateService.endpointSourceHeaders())
	c.Status(200)
}

func (s *SubHandler) subs(c *gin.Context) {
	var headers []string
	var result *string
	var err error
	subId := c.Param("subid")
	format, isFormat, negotiated := requestedUserSubscriptionFormat(c)
	if negotiated {
		c.Header("Vary", "User-Agent, Accept")
	}
	if isFormat {
		switch format {
		case "json":
			result, headers, err = s.JsonService.GetJson(subId, format)
		case "clash":
			result, headers, err = s.ClashService.GetClash(subId, requestHost(c))
		case "raw":
			result, headers, err = s.SubService.GetSubs(subId)
		case "info", "html":
			s.userCenter(c, subId)
			return
		}
		if err != nil || result == nil {
			logger.Error(err)
			c.String(400, "Error!")
			return
		}
	} else {
		result, headers, err = s.SubService.GetSubs(subId)
		if err != nil || result == nil {
			logger.Error(err)
			c.String(400, "Error!")
			return
		}
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
