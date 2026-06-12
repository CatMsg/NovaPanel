package api

import (
	"encoding/gob"
	"net/http"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	loginUser = "LOGIN_USER"
)

func init() {
	gob.Register(model.User{})
}

func sessionOptions(c *gin.Context) sessions.Options {
	secure := c.Request.TLS != nil
	if !secure {
		proto := strings.ToLower(c.GetHeader("X-Forwarded-Proto"))
		secure = proto == "https" || strings.EqualFold(c.GetHeader("X-Forwarded-SSL"), "on")
	}

	return sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func SetLoginUser(c *gin.Context, userName string, maxAge int) error {
	options := sessionOptions(c)
	if maxAge > 0 {
		options.MaxAge = maxAge * 60
	}

	s := sessions.Default(c)
	s.Set(loginUser, userName)
	s.Options(options)

	return s.Save()
}

func SetMaxAge(c *gin.Context) error {
	s := sessions.Default(c)
	s.Options(sessionOptions(c))
	return s.Save()
}

func GetLoginUser(c *gin.Context) string {
	s := sessions.Default(c)
	obj := s.Get(loginUser)
	if obj == nil {
		return ""
	}
	objStr, ok := obj.(string)
	if !ok {
		return ""
	}
	return objStr
}

func IsLogin(c *gin.Context) bool {
	return GetLoginUser(c) != ""
}

func ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	options := sessionOptions(c)
	options.MaxAge = -1
	s.Options(options)
	s.Save()
}
