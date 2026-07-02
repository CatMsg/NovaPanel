package sub

import (
	"net"
	"strings"

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
}

func NewSubHandler(g *gin.RouterGroup) {
	a := &SubHandler{}
	a.initRouter(g)
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
	g.GET("/aggregate", s.aggregate)
	g.HEAD("/aggregate", s.aggregateHeaders)
	g.GET("/:subid/aggregate", s.clientAggregate)
	g.HEAD("/:subid/aggregate", s.clientAggregateHeaders)
	g.GET("/:subid", s.subs)
	g.HEAD("/:subid", s.subHeaders)
}

func (s *SubHandler) aggregate(c *gin.Context) {
	format := c.Query("format")
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
	_, usage, err := s.AggregateService.collectAggregateLinks(requestHost(c))
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}
	s.addHeaders(c, s.AggregateService.aggregateHeaders(usage))
	c.Status(200)
}

func (s *SubHandler) clientAggregate(c *gin.Context) {
	subId := c.Param("subid")
	format := c.Query("format")
	result, headers, err := s.AggregateService.GetClientAggregate(subId, format, requestHost(c))
	if err != nil || result == nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.String(200, *result)
}

func (s *SubHandler) clientAggregateHeaders(c *gin.Context) {
	subId := c.Param("subid")
	result, headers, err := s.AggregateService.GetClientAggregate(subId, "clash", requestHost(c))
	if err != nil || result == nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)
	c.Status(200)
}

func (s *SubHandler) subs(c *gin.Context) {
	var headers []string
	var result *string
	var err error
	subId := c.Param("subid")
	format, isFormat := c.GetQuery("format")
	if isFormat {
		switch format {
		case "json":
			result, headers, err = s.JsonService.GetJson(subId, format)
		case "clash":
			result, headers, err = s.ClashService.GetClash(subId)
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
	c.Writer.Header().Set("Subscription-Userinfo", headers[0])
	c.Writer.Header().Set("Profile-Update-Interval", headers[1])
	c.Writer.Header().Set("Profile-Title", headers[2])
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
