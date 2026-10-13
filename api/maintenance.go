package api

import (
	"errors"

	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-gonic/gin"
)

type maintenanceRequest struct {
	Enabled *bool `json:"enabled"`
}

func (a *ApiService) GetMaintenanceStatus(c *gin.Context) {
	jsonObj(c, service.GetMaintenanceService().Status(), nil)
}

func (a *ApiService) SetMaintenance(c *gin.Context) {
	var input maintenanceRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		jsonMsg(c, "maintenance", err)
		return
	}
	if input.Enabled == nil {
		jsonMsg(c, "maintenance", errors.New("enabled must be specified"))
		return
	}

	err := service.GetMaintenanceService().SetEnabled(*input.Enabled)
	jsonMsgObj(c, "maintenance", service.GetMaintenanceService().Status(), err)
}
