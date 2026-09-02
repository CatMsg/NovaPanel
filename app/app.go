package app

import (
	"log"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/cronjob"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/CatMsg/NovaPanel/sub"
	"github.com/CatMsg/NovaPanel/web"

	"github.com/op/go-logging"
)

type APP struct {
	service.SettingService
	configService *service.ConfigService
	masqueService *service.MasqueService
	mieruService  *service.MieruService
	webServer     *web.Server
	subServer     *sub.Server
	cronJob       *cronjob.CronJob
	core          *core.Core
}

func NewApp() *APP {
	return &APP{}
}

func (a *APP) Init() error {
	log.Printf("%v %v", config.GetName(), config.GetVersion())

	a.initLog()

	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return err
	}

	if err := service.InitSSHListenPorts(); err != nil {
		logger.Warning("init ssh listen ports failed:", err)
	}

	// Persist missing defaults before services read settings during startup.
	if _, err := a.SettingService.GetAllSetting(); err != nil {
		return err
	}

	if err := service.RebuildManagedPortEntries(); err != nil {
		return err
	}

	a.core = core.NewCore()
	a.masqueService = service.NewMasqueService()
	service.SetMasqueService(a.masqueService)
	a.mieruService = service.NewMieruService()
	service.SetMieruService(a.mieruService)

	a.cronJob = cronjob.NewCronJob()
	a.webServer = web.NewServer()
	a.subServer = sub.NewServer()

	a.configService = service.NewConfigService(a.core)
	service.SetSubServerRestartFunc(a.subServer.Restart)

	return nil
}

func (a *APP) Start() error {
	loc, err := a.SettingService.GetTimeLocation()
	if err != nil {
		return err
	}

	trafficAge, err := a.SettingService.GetTrafficAge()
	if err != nil {
		return err
	}

	err = a.cronJob.Start(loc, trafficAge)
	if err != nil {
		return err
	}

	err = a.webServer.Start()
	if err != nil {
		return err
	}

	err = a.subServer.Start()
	if err != nil {
		return err
	}

	err = a.configService.StartCore()
	if err != nil {
		logger.Error(err)
	}

	go a.runDeferredStartupTasks()

	return nil
}

func (a *APP) runDeferredStartupTasks() {
	if a.masqueService != nil {
		if err := a.masqueService.SyncFromDB(); err != nil {
			logger.Warning("rebuild masque service failed:", err)
		}
	}
	if a.mieruService != nil {
		if err := a.mieruService.SyncFromDB(); err != nil {
			logger.Warning("rebuild mieru service failed:", err)
		}
		a.mieruService.StartWatchdog()
	}

	if err := a.SettingService.RebuildAllManagedPortForwarding(&a.configService.InboundService, &a.configService.EndpointService); err != nil {
		logger.Warning("rebuild all managed port forwarding failed:", err)
	}
}

func (a *APP) Stop() {
	a.cronJob.Stop()
	err := a.subServer.Stop()
	if err != nil {
		logger.Warning("stop Sub Server err:", err)
	}
	err = a.webServer.Stop()
	if err != nil {
		logger.Warning("stop Web Server err:", err)
	}
	err = a.configService.StopCore()
	if err != nil {
		logger.Warning("stop Core err:", err)
	}
	if a.masqueService != nil {
		err = a.masqueService.Stop()
		if err != nil {
			logger.Warning("stop Masque Service err:", err)
		}
	}
	if a.mieruService != nil {
		err = a.mieruService.Stop()
		if err != nil {
			logger.Warning("stop Mieru Service err:", err)
		}
	}
}

func (a *APP) initLog() {
	switch config.GetLogLevel() {
	case config.Debug:
		logger.InitLogger(logging.DEBUG)
	case config.Info:
		logger.InitLogger(logging.INFO)
	case config.Warn:
		logger.InitLogger(logging.WARNING)
	case config.Error:
		logger.InitLogger(logging.ERROR)
	default:
		log.Fatal("unknown log level:", config.GetLogLevel())
	}
}

func (a *APP) RestartApp() error {
	a.Stop()
	return a.Start()
}

func (a *APP) GetCore() *core.Core {
	return a.core
}
