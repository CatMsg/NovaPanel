package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"

	"github.com/shirou/gopsutil/v4/net"
)

func resetSetting() error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	settingService := service.SettingService{}
	err = settingService.ResetSettings()
	if err != nil {
		return fmt.Errorf("reset settings: %w", err)
	}
	fmt.Println("reset setting success")
	return nil
}

func updateSetting(port int, path string, subPort int, subPath string, webCertFile string, webKeyFile string, subCertFile string, subKeyFile string, clearWebTLS bool, clearSubTLS bool) error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	settingService := service.SettingService{}
	oldWebPort, err := settingService.GetPort()
	if err != nil {
		return fmt.Errorf("get current panel port: %w", err)
	}
	oldSubPort, err := settingService.GetSubPort()
	if err != nil {
		return fmt.Errorf("get current subscription port: %w", err)
	}
	newWebPort := oldWebPort
	newSubPort := oldSubPort
	if port > 0 {
		newWebPort = port
	}
	if subPort > 0 {
		newSubPort = subPort
	}
	if newWebPort != oldWebPort || newSubPort != oldSubPort {
		if err := service.ValidateManagedPanelPorts(newWebPort, newSubPort); err != nil {
			return fmt.Errorf("validate panel ports: %w", err)
		}
	}
	set := func(label string, update func() error) error {
		if err := update(); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		fmt.Println(label + " success")
		return nil
	}

	if port > 0 {
		if err := set("set port", func() error { return settingService.SetPort(port) }); err != nil {
			return err
		}
	}
	if path != "" {
		if err := set("set path", func() error { return settingService.SetWebPath(path) }); err != nil {
			return err
		}
	}
	if subPort > 0 {
		if err := set("set sub port", func() error { return settingService.SetSubPort(subPort) }); err != nil {
			return err
		}
	}
	if subPath != "" {
		if err := set("set sub path", func() error { return settingService.SetSubPath(subPath) }); err != nil {
			return err
		}
	}
	if webCertFile != "" {
		if err := set("set web cert file", func() error { return settingService.SetCertFile(webCertFile) }); err != nil {
			return err
		}
	}
	if webKeyFile != "" {
		if err := set("set web key file", func() error { return settingService.SetKeyFile(webKeyFile) }); err != nil {
			return err
		}
	}
	if subCertFile != "" {
		if err := set("set subscription cert file", func() error { return settingService.SetSubCertFile(subCertFile) }); err != nil {
			return err
		}
	}
	if subKeyFile != "" {
		if err := set("set subscription key file", func() error { return settingService.SetSubKeyFile(subKeyFile) }); err != nil {
			return err
		}
	}
	if clearWebTLS {
		if err := set("clear web cert file", func() error { return settingService.SetCertFile("") }); err != nil {
			return err
		}
		if err := set("clear web key file", func() error { return settingService.SetKeyFile("") }); err != nil {
			return err
		}
	}
	if clearSubTLS {
		if err := set("clear subscription cert file", func() error { return settingService.SetSubCertFile("") }); err != nil {
			return err
		}
		if err := set("clear subscription key file", func() error { return settingService.SetSubKeyFile("") }); err != nil {
			return err
		}
	}

	currentWebPort, err := settingService.GetPort()
	if err != nil {
		return fmt.Errorf("get panel port after update: %w", err)
	}
	currentSubPort, err := settingService.GetSubPort()
	if err != nil {
		return fmt.Errorf("get subscription port after update: %w", err)
	}
	if currentWebPort != oldWebPort || currentSubPort != oldSubPort {
		if err := settingService.SyncManagedPanelPortForwarding(oldWebPort, currentWebPort, oldSubPort, currentSubPort); err != nil {
			rollbackErrors := []error{fmt.Errorf("sync panel port forwarding: %w", err)}
			if currentWebPort != oldWebPort {
				if rollbackErr := settingService.SetPort(oldWebPort); rollbackErr != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback panel port: %w", rollbackErr))
				}
			}
			if currentSubPort != oldSubPort {
				if rollbackErr := settingService.SetSubPort(oldSubPort); rollbackErr != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback subscription port: %w", rollbackErr))
				}
			}
			return errors.Join(rollbackErrors...)
		} else {
			fmt.Println("sync panel port forwarding success")
		}
	}
	return nil
}

func showSetting() error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	settingService := service.SettingService{}
	allSetting, err := settingService.GetAllSetting()
	if err != nil {
		return fmt.Errorf("get current settings: %w", err)
	}
	if allSetting == nil {
		return fmt.Errorf("get current settings: no settings returned")
	}
	fmt.Println("Current panel settings:")
	fmt.Println("\tPanel port:\t", (*allSetting)["webPort"])
	fmt.Println("\tPanel path:\t", (*allSetting)["webPath"])
	if (*allSetting)["webListen"] != "" {
		fmt.Println("\tPanel IP:\t", (*allSetting)["webListen"])
	}
	if (*allSetting)["webDomain"] != "" {
		fmt.Println("\tPanel Domain:\t", (*allSetting)["webDomain"])
	}
	if (*allSetting)["webURI"] != "" {
		fmt.Println("\tPanel URI:\t", (*allSetting)["webURI"])
	}
	if (*allSetting)["webCertFile"] != "" {
		fmt.Println("\tPanel Cert:\t", (*allSetting)["webCertFile"])
	}
	if (*allSetting)["webKeyFile"] != "" {
		fmt.Println("\tPanel Key:\t", (*allSetting)["webKeyFile"])
	}
	fmt.Println()
	fmt.Println("Current subscription settings:")
	fmt.Println("\tSub port:\t", (*allSetting)["subPort"])
	fmt.Println("\tSub path:\t", (*allSetting)["subPath"])
	if (*allSetting)["subListen"] != "" {
		fmt.Println("\tSub IP:\t", (*allSetting)["subListen"])
	}
	if (*allSetting)["subDomain"] != "" {
		fmt.Println("\tSub Domain:\t", (*allSetting)["subDomain"])
	}
	if (*allSetting)["subURI"] != "" {
		fmt.Println("\tSub URI:\t", (*allSetting)["subURI"])
	}
	if (*allSetting)["subCertFile"] != "" {
		fmt.Println("\tSub Cert:\t", (*allSetting)["subCertFile"])
	}
	if (*allSetting)["subKeyFile"] != "" {
		fmt.Println("\tSub Key:\t", (*allSetting)["subKeyFile"])
	}
	return nil
}

func getPublicIP() string {
	return (&service.ServerService{}).GetPublicIP()
}

func getPanelURI() error {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	settingService := service.SettingService{}
	Port, err := settingService.GetPort()
	if err != nil {
		return fmt.Errorf("get panel port: %w", err)
	}
	BasePath, err := settingService.GetWebPath()
	if err != nil {
		return fmt.Errorf("get panel path: %w", err)
	}
	Listen, err := settingService.GetListen()
	if err != nil {
		return fmt.Errorf("get panel listen address: %w", err)
	}
	Domain, err := settingService.GetWebDomain()
	if err != nil {
		return fmt.Errorf("get panel domain: %w", err)
	}
	KeyFile, err := settingService.GetKeyFile()
	if err != nil {
		return fmt.Errorf("get panel key file: %w", err)
	}
	CertFile, err := settingService.GetCertFile()
	if err != nil {
		return fmt.Errorf("get panel certificate file: %w", err)
	}
	TLS := false
	if KeyFile != "" && CertFile != "" {
		TLS = true
	}
	Proto := ""
	if TLS {
		Proto = "https://"
	} else {
		Proto = "http://"
	}
	PortText := fmt.Sprintf(":%d", Port)
	if (Port == 443 && TLS) || (Port == 80 && !TLS) {
		PortText = ""
	}
	if len(Domain) > 0 {
		fmt.Println(Proto + Domain + PortText + BasePath)
		return nil
	}
	if len(Listen) > 0 {
		fmt.Println(Proto + Listen + PortText + BasePath)
		return nil
	}
	fmt.Println("Local address:")
	netInterfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("enumerate network interfaces: %w", err)
	}
	for i := 0; i < len(netInterfaces); i++ {
		if interfaceIsUp(netInterfaces[i].Flags) {
			addrs := netInterfaces[i].Addrs
			for _, address := range addrs {
				IP := strings.Split(address.Addr, "/")[0]
				if strings.Contains(address.Addr, ".") {
					fmt.Println(Proto + IP + PortText + BasePath)
				} else if !strings.HasPrefix(address.Addr, "fe80::") {
					fmt.Println(Proto + "[" + IP + "]" + PortText + BasePath)
				}
			}
		}
	}
	pubIP := getPublicIP()
	if pubIP != "" {
		fmt.Printf("\nGlobal address:\n%s%s%s\n", Proto, pubIP, PortText+BasePath)
	}
	return nil
}

func interfaceIsUp(flags []string) bool {
	up, loopback := false, false
	for _, flag := range flags {
		if flag == "up" {
			up = true
		} else if flag == "loopback" {
			loopback = true
		}
	}
	return up && !loopback
}
