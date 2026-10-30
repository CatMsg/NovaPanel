package api

import (
	"net/http"
	"strings"

	"github.com/CatMsg/NovaPanel/logger"
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
	g.Use(noStoreAPIResponses())
	g.Use(sameOriginAPIMiddleware(a.SettingService.GetLoginTrustedProxies))
	g.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasSuffix(path, "login") && !strings.HasSuffix(path, "logout") {
			a.checkLogin(c)
		}
	})
	g.POST("/:postAction", a.postHandler)
	g.GET("/:getAction", a.getHandler)
}

func (a *APIHandler) checkLogin(c *gin.Context) {
	reject := func() {
		if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
			pureJsonMsg(c, false, "Invalid login")
		} else {
			c.Redirect(http.StatusTemporaryRedirect, "/login")
		}
		c.Abort()
	}

	if !IsLogin(c) {
		reject()
		return
	}
	currentEpoch, err := a.UserService.GetAuthSessionEpoch()
	if err != nil {
		logger.Warning("validate login session epoch failed: ", err)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, Msg{Success: false, Msg: "unable to validate login session"})
		return
	}
	sessionEpoch, valid := getLoginSessionEpoch(c)
	if !valid || sessionEpoch != currentEpoch {
		if err := ClearSession(c); err != nil {
			logger.Warning("clear stale login session failed: ", err)
		}
		reject()
		return
	}
	c.Next()
}

func (a *APIHandler) postHandler(c *gin.Context) {
	loginUser := GetLoginUser(c)
	action := c.Param("postAction")

	switch action {
	case "logout":
		a.ApiService.Logout(c)
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
	case "remoteBackupSave":
		a.ApiService.SaveRemoteBackupSettings(c)
	case "remoteBackupRun":
		a.ApiService.RunRemoteBackup(c)
	case "maintenance":
		a.ApiService.SetMaintenance(c)
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
	case "fleetInboundPreview":
		a.ApiService.PreviewFleetInboundCreate(c)
	case "fleetInboundCreate":
		a.ApiService.CreateFleetInbound(c)
	case "fleetInboundDelete":
		a.ApiService.DeleteFleetInbound(c)
	case "sessionClose":
		a.ApiService.CloseSession(c)
	case "alertSave":
		a.ApiService.SaveAlertSettings(c)
	case "alertTest":
		a.ApiService.TestAlert(c)
	case "loginUnban":
		a.ApiService.UnbanLoginIP(c)
	case "mieruDebug":
		a.ApiService.EnableMieruDebug(c)
	case "routeExplain":
		a.ApiService.ExplainRoute(c)
	case "failoverSave":
		a.ApiService.SaveFailoverPolicy(c)
	case "failoverDelete":
		a.ApiService.DeleteFailoverPolicy(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}

func (a *APIHandler) getHandler(c *gin.Context) {
	action := c.Param("getAction")

	switch action {
	case "logout":
		c.Header("Allow", "POST")
		c.AbortWithStatusJSON(http.StatusMethodNotAllowed, Msg{Success: false, Msg: "logout requires POST"})
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
	case "remote-backup-status":
		a.ApiService.GetRemoteBackupStatus(c)
	case "maintenance-status":
		a.ApiService.GetMaintenanceStatus(c)
	case "stats":
		a.ApiService.GetStats(c)
	case "status":
		a.ApiService.GetStatus(c)
	case "public-ip":
		a.ApiService.GetPublicIP(c)
	case "health":
		a.ApiService.GetHealth(c)
	case "traffic-budget":
		a.ApiService.GetTrafficBudget(c)
	case "traffic-budget-history":
		a.ApiService.GetTrafficBudgetHistory(c)
	case "fleetTrafficHistory":
		a.ApiService.GetFleetTrafficBudgetHistory(c)
	case "alert-settings":
		a.ApiService.GetAlertSettings(c)
	case "login-security":
		a.ApiService.GetLoginProtection(c)
	case "rule-catalog":
		a.ApiService.SearchRuleCatalog(c)
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
	case "fleetInboundTargets":
		a.ApiService.GetFleetInboundTargets(c)
	case "sessions":
		a.ApiService.GetFleetSessions(c)
	case "update-status":
		a.ApiService.GetUpdateStatus(c)
	case "ruleset-health":
		a.ApiService.GetRuleSetHealth(c)
	case "failover-status":
		a.ApiService.GetFailoverStatus(c)
	case "outbound-health":
		a.ApiService.GetOutboundHealth(c)
	default:
		jsonMsg(c, "failed", common.NewError("unknown action: ", action))
	}
}
