package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func DomainValidator(domain string) gin.HandlerFunc {
	allowedHost := normalizeDomainHost(domain)
	return func(c *gin.Context) {
		host, ok := requestHostname(c.Request.Host)
		if !ok || allowedHost == "" || normalizeDomainHost(host) != allowedHost {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
	}
}

func requestHostname(authority string) (string, bool) {
	if strings.HasPrefix(authority, "[") {
		if host, port, err := net.SplitHostPort(authority); err == nil {
			return host, validDomainPort(port)
		}
		if strings.HasSuffix(authority, "]") {
			host := strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]")
			return host, net.ParseIP(host) != nil
		}
		return "", false
	}
	if strings.Count(authority, ":") > 1 {
		return "", false
	}
	if strings.Contains(authority, ":") {
		host, port, err := net.SplitHostPort(authority)
		if err != nil || !validDomainPort(port) {
			return "", false
		}
		return host, true
	}
	return authority, authority != ""
}

func validDomainPort(port string) bool {
	if port == "" {
		return false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	value, err := strconv.Atoi(port)
	return err == nil && value > 0 && value <= 65535
}

func normalizeDomainHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}
