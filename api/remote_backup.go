package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-gonic/gin"
)

type remoteBackupAPI interface {
	Status() (service.RemoteBackupStatus, error)
	Save(service.RemoteBackupSettings) error
	Run(context.Context, string, string) (*service.RemoteBackupResult, error)
}

var remoteBackupServiceForAPI = func() remoteBackupAPI { return service.GetRemoteBackupService() }

func remoteBackupHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}

func (a *ApiService) GetRemoteBackupStatus(c *gin.Context) {
	remoteBackupHeaders(c)
	status, err := remoteBackupServiceForAPI().Status()
	jsonObj(c, status, err)
}

func decodeBackupRequest(c *gin.Context, out interface{}) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("invalid remote backup request")
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		return errors.New("invalid remote backup request")
	}
	return nil
}

func (a *ApiService) SaveRemoteBackupSettings(c *gin.Context) {
	remoteBackupHeaders(c)
	var settings service.RemoteBackupSettings
	if err := decodeBackupRequest(c, &settings); err != nil {
		jsonObj(c, nil, err)
		return
	}
	if err := remoteBackupServiceForAPI().Save(settings); err != nil {
		jsonObj(c, nil, err)
		return
	}
	status, err := remoteBackupServiceForAPI().Status()
	jsonObj(c, status, err)
}

func (a *ApiService) RunRemoteBackup(c *gin.Context) {
	remoteBackupHeaders(c)
	var request struct {
		Action   string `json:"action"`
		Artifact string `json:"artifact"`
	}
	if err := decodeBackupRequest(c, &request); err != nil {
		jsonObj(c, nil, err)
		return
	}
	result, err := remoteBackupServiceForAPI().Run(c.Request.Context(), request.Action, request.Artifact)
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
		c.FileAttachment(result.RecoveryPath, result.Artifact.Artifact)
		return
	}
	jsonObj(c, result, nil)
}
