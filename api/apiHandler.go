package api

import (
	"strings"

	"github.com/CatMsg/NovaPanel/util/common"

	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	ApiService
	apiv2 *APIv2Handler
}

func NewAPIHandler(g *gin.RouterGroup, a2 *APIv2Handler) {
	a := &APIHandler{
		apiv2: a2,
	}
	a.initRouter(g)
}

func (a *APIHandler) initRouter(g *gin.RouterGroup) {
	g.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasSuffix(path, "login") && !strings.HasSuffix(path, "logout") {
			checkLogin(c)
		}
	})
	g.POST("/:postAction", a.postHandler)
	g.GET("/:getAction", a.getHandler)
}

func (a *APIHandler) postHandler(c *gin.Context) {
	loginUser := GetLoginUser(c)
	action := c.Param("postAction")

	switch action {
	case "login":
		a.ApiService.Login(c)
	case "changePass":
		a.ApiService.ChangePass(c)
	case "save":
		a.ApiService.Save(c, loginUser)
	case "preflight":
		a.ApiService.PreflightSave(c)
	case "restartApp":
		a.ApiService.RestartApp(c)
	case "restartSb":
		a.ApiService.RestartSb(c)
	case "linkConvert":
		a.ApiService.LinkConvert(c)
	case "subConvert":
		a.ApiService.SubConvert(c)
	case "importdb":
		a.ApiService.ImportDb(c)
	case "validateBackup":
		a.ApiService.ValidateDb(c)
	case "reconcilePorts":
		a.ApiService.ReconcilePorts(c)
	case "repairPortIssue":
		a.ApiService.RepairPortIssue(c)
	case "addToken":
		a.ApiService.AddToken(c)
		a.apiv2.ReloadTokens()
	case "deleteToken":
		a.ApiService.DeleteToken(c)
		a.apiv2.ReloadTokens()
	case "fleetSave":
		a.ApiService.SaveFleet(c)
	case "fleetAction":
		a.ApiService.FleetAction(c)
	case "fleetRefresh":
		a.ApiService.FleetRefresh(c)
	case "alertSave":
		a.ApiService.SaveAlertSettings(c)
	case "alertTest":
		a.ApiService.TestAlert(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIHandler) getHandler(c *gin.Context) {
	action := c.Param("getAction")

	switch action {
	case "logout":
		a.ApiService.Logout(c)
	case "load":
		a.ApiService.LoadData(c)
	case "inbounds", "outbounds", "endpoints", "services", "tls", "clients", "config":
		err := a.ApiService.LoadPartialData(c, []string{action})
		if err != nil {
			jsonMsg(c, action, err)
		}
		return
	case "users":
		a.ApiService.GetUsers(c)
	case "settings":
		a.ApiService.GetSettings(c)
	case "stats":
		a.ApiService.GetStats(c)
	case "status":
		a.ApiService.GetStatus(c)
	case "public-ip":
		a.ApiService.GetPublicIP(c)
	case "health":
		a.ApiService.GetHealth(c)
	case "alert-settings":
		a.ApiService.GetAlertSettings(c)
	case "ports":
		a.ApiService.GetPorts(c)
	case "masque-status":
		a.ApiService.GetMasqueStatus(c)
	case "mieru-status":
		a.ApiService.GetMieruStatus(c)
	case "onlines":
		a.ApiService.GetOnlines(c)
	case "logs":
		a.ApiService.GetLogs(c)
	case "changes":
		a.ApiService.CheckChanges(c)
	case "checkLogin":
		jsonMsg(c, "", nil)
	case "keypairs":
		a.ApiService.GetKeypairs(c)
	case "getdb":
		a.ApiService.GetDb(c)
	case "tokens":
		a.ApiService.GetTokens(c)
	case "singbox-config":
		a.ApiService.GetSingboxConfig(c)
	case "checkOutbound":
		a.ApiService.GetCheckOutbound(c)
	case "fleet":
		a.ApiService.GetFleet(c)
	case "update-status":
		a.ApiService.GetUpdateStatus(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}
