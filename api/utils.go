package api

import (
	"net"
	"net/http"
	"strings"

	"github.com/CatMsg/NovaPanel/logger"

	"github.com/gin-gonic/gin"
)

type Msg struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

func getRemoteIPWithTrustedProxies(c *gin.Context, trustedProxyList string) string {
	return resolveRemoteIP(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"), trustedProxyList)
}

func resolveRemoteIP(remoteAddr, forwardedFor, trustedProxyList string) string {
	peer := parseRemoteAddress(remoteAddr)
	if peer == nil {
		return "unknown"
	}
	trusted := trustedProxyNetworks(trustedProxyList)
	if !networkListContains(trusted, peer) || strings.TrimSpace(forwardedFor) == "" {
		return peer.String()
	}

	forwarded := strings.Split(forwardedFor, ",")
	hops := make([]net.IP, 0, len(forwarded)+1)
	for _, raw := range forwarded {
		value := strings.TrimSpace(raw)
		if value == "" {
			return peer.String()
		}
		ip := net.ParseIP(value)
		if ip == nil {
			return peer.String()
		}
		hops = append(hops, ip)
	}
	hops = append(hops, peer)
	for i := len(hops) - 1; i >= 0; i-- {
		if !networkListContains(trusted, hops[i]) {
			return hops[i].String()
		}
	}
	return hops[0].String()
}

func parseRemoteAddress(value string) net.IP {
	if host, _, err := net.SplitHostPort(strings.TrimSpace(value)); err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(strings.TrimSpace(strings.Trim(value, "[]")))
}

func trustedProxyNetworks(raw string) []*net.IPNet {
	values := append([]string{"127.0.0.0/8", "::1/128"}, strings.Fields(raw)...)
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			networks = append(networks, network)
		}
	}
	return networks
}

func networkListContains(networks []*net.IPNet, ip net.IP) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func getHostname(c *gin.Context) string {
	host := c.Request.Host
	if strings.Contains(host, ":") {
		host, _, _ = net.SplitHostPort(c.Request.Host)
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return host
}

func jsonMsg(c *gin.Context, msg string, err error) {
	jsonMsgObj(c, msg, nil, err)
}

func jsonObj(c *gin.Context, obj interface{}, err error) {
	jsonMsgObj(c, "", obj, err)
}

func jsonMsgObj(c *gin.Context, msg string, obj interface{}, err error) {
	m := Msg{
		Obj: obj,
	}
	if err == nil {
		m.Success = true
		if msg != "" {
			m.Msg = msg
		}
	} else {
		m.Success = false
		m.Msg = msg + ": " + err.Error()
		logger.Warning("failed :", err)
	}
	c.JSON(http.StatusOK, m)
}

func pureJsonMsg(c *gin.Context, success bool, msg string) {
	if success {
		c.JSON(http.StatusOK, Msg{
			Success: true,
			Msg:     msg,
		})
	} else {
		c.JSON(http.StatusOK, Msg{
			Success: false,
			Msg:     msg,
		})
	}
}

func checkLogin(c *gin.Context) {
	if !IsLogin(c) {
		if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
			pureJsonMsg(c, false, "Invalid login")
		} else {
			c.Redirect(http.StatusTemporaryRedirect, "/login")
		}
		c.Abort()
	} else {
		c.Next()
	}
}
