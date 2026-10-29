package api

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const requestSchemeContextKey = "novapanel.requestScheme"

func sameOriginAPIMiddleware(getTrustedProxyList ...func() (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			trustedProxyList := ""
			if c.Request.TLS == nil && hasForwardedSchemeHeaders(c) && len(getTrustedProxyList) > 0 {
				if value, err := getTrustedProxyList[0](); err == nil {
					trustedProxyList = value
				}
			}
			requestScheme := requestSchemeFor(c, trustedProxyList)
			if requestScheme == "" {
				requestScheme = "http"
			}
			c.Set(requestSchemeContextKey, requestScheme)
			c.Next()
			return
		}

		trustedProxyList := ""
		if c.Request.TLS == nil && hasForwardedSchemeHeaders(c) && len(getTrustedProxyList) > 0 {
			if value, err := getTrustedProxyList[0](); err == nil {
				trustedProxyList = value
			}
		}
		requestScheme := requestSchemeFor(c, trustedProxyList)
		c.Set(requestSchemeContextKey, requestScheme)

		if !requestHasSameOrigin(c, trustedProxyList) {
			c.AbortWithStatusJSON(http.StatusForbidden, Msg{Success: false, Msg: "cross-origin request rejected"})
			return
		}
		c.Next()
	}
}

func requestHasSameOrigin(c *gin.Context, trustedProxyLists ...string) bool {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin == "" || strings.EqualFold(origin, "null") {
		return false
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.User != nil || parsedOrigin.RawQuery != "" || parsedOrigin.Fragment != "" || (parsedOrigin.Path != "" && parsedOrigin.Path != "/") {
		return false
	}

	trustedProxyList := ""
	if len(trustedProxyLists) > 0 {
		trustedProxyList = trustedProxyLists[0]
	}
	requestScheme := requestSchemeFor(c, trustedProxyList)
	if requestScheme == "" {
		return false
	}

	originAuthority, originOK := normalizeOriginAuthority(parsedOrigin.Scheme, parsedOrigin.Host)
	requestAuthority, requestOK := normalizeOriginAuthority(requestScheme, c.Request.Host)
	return originOK && requestOK && strings.EqualFold(parsedOrigin.Scheme, requestScheme) && originAuthority == requestAuthority
}

func hasForwardedSchemeHeaders(c *gin.Context) bool {
	return strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")) != "" || strings.TrimSpace(c.GetHeader("X-Forwarded-SSL")) != ""
}

func requestSchemeFor(c *gin.Context, trustedProxyList string) string {
	if c.Request.TLS != nil {
		return "https"
	}
	peer := parseRemoteAddress(c.Request.RemoteAddr)
	if peer == nil || !networkListContains(trustedProxyNetworks(trustedProxyList), peer) {
		return "http"
	}
	if forwarded := strings.TrimSpace(strings.SplitN(c.GetHeader("X-Forwarded-Proto"), ",", 2)[0]); forwarded != "" {
		forwarded = strings.ToLower(forwarded)
		if forwarded == "http" || forwarded == "https" {
			return forwarded
		}
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-SSL")), "on") {
		return "https"
	}
	return "http"
}

func normalizeOriginAuthority(scheme, authority string) (string, bool) {
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" || authority == "" {
		return "", false
	}
	parsed, err := url.Parse(scheme + "://" + authority)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	host := parsed.Hostname()
	if host == "" {
		return "", false
	}

	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", false
	}
	return net.JoinHostPort(strings.ToLower(host), strconv.Itoa(portNumber)), true
}
