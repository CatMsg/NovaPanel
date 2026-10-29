package api

import (
	"encoding/gob"
	"net/http"
	"strconv"

	"github.com/CatMsg/NovaPanel/database/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	loginUser           = "LOGIN_USER"
	loginSessionEpoch   = "LOGIN_SESSION_EPOCH"
	initialSessionEpoch = uint64(1)
)

func init() {
	gob.Register(model.User{})
}

func sessionOptions(c *gin.Context) sessions.Options {
	secure := c.Request.TLS != nil
	if scheme, ok := c.Get(requestSchemeContextKey); ok {
		secure = scheme == "https"
	}

	return sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func SetLoginUser(c *gin.Context, userName string, maxAge int, epoch uint64) error {
	options := sessionOptions(c)
	if maxAge > 0 {
		options.MaxAge = maxAge * 60
	}

	s := sessions.Default(c)
	s.Set(loginUser, userName)
	s.Set(loginSessionEpoch, strconv.FormatUint(epoch, 10))
	s.Options(options)

	return s.Save()
}

func getLoginSessionEpoch(c *gin.Context) (uint64, bool) {
	value := sessions.Default(c).Get(loginSessionEpoch)
	if value == nil {
		return initialSessionEpoch, true
	}
	encoded, ok := value.(string)
	if !ok {
		return 0, false
	}
	epoch, err := strconv.ParseUint(encoded, 10, 64)
	return epoch, err == nil && epoch > 0
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

func ClearSession(c *gin.Context) error {
	s := sessions.Default(c)
	s.Clear()
	options := sessionOptions(c)
	options.MaxAge = -1
	s.Options(options)
	return s.Save()
}
