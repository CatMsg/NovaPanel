package cmd

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"

	"github.com/shirou/gopsutil/v4/net"
)

func resetSetting() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	settingService := service.SettingService{}
	err = settingService.ResetSettings()
	if err != nil {
		fmt.Println("reset setting failed:", err)
	} else {
		fmt.Println("reset setting success")
	}
}

func updateSetting(port int, path string, subPort int, subPath string, webCertFile string, webKeyFile string, subCertFile string, subKeyFile string, clearWebTLS bool, clearSubTLS bool) {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	settingService := service.SettingService{}

	if port > 0 {
		err := settingService.SetPort(port)
		if err != nil {
			fmt.Println("set port failed:", err)
		} else {
			fmt.Println("set port success")
		}
	}
	if path != "" {
		err := settingService.SetWebPath(path)
		if err != nil {
			fmt.Println("set path failed:", err)
		} else {
			fmt.Println("set path success")
		}
	}
	if subPort > 0 {
		err := settingService.SetSubPort(subPort)
		if err != nil {
			fmt.Println("set sub port failed:", err)
		} else {
			fmt.Println("set sub port success")
		}
	}
	if subPath != "" {
		err := settingService.SetSubPath(subPath)
		if err != nil {
			fmt.Println("set sub path failed:", err)
		} else {
			fmt.Println("set sub path success")
		}
	}
	if webCertFile != "" {
		err := settingService.SetCertFile(webCertFile)
		if err != nil {
			fmt.Println("set web cert file failed:", err)
		} else {
			fmt.Println("set web cert file success")
		}
	}
	if webKeyFile != "" {
		err := settingService.SetKeyFile(webKeyFile)
		if err != nil {
			fmt.Println("set web key file failed:", err)
		} else {
			fmt.Println("set web key file success")
		}
	}
	if subCertFile != "" {
		err := settingService.SetSubCertFile(subCertFile)
		if err != nil {
			fmt.Println("set sub cert file failed:", err)
		} else {
			fmt.Println("set sub cert file success")
		}
	}
	if subKeyFile != "" {
		err := settingService.SetSubKeyFile(subKeyFile)
		if err != nil {
			fmt.Println("set sub key file failed:", err)
		} else {
			fmt.Println("set sub key file success")
		}
	}
	if clearWebTLS {
		err := settingService.SetCertFile("")
		if err != nil {
			fmt.Println("clear web cert file failed:", err)
		} else {
			fmt.Println("clear web cert file success")
		}
		err = settingService.SetKeyFile("")
		if err != nil {
			fmt.Println("clear web key file failed:", err)
		} else {
			fmt.Println("clear web key file success")
		}
	}
	if clearSubTLS {
		err := settingService.SetSubCertFile("")
		if err != nil {
			fmt.Println("clear sub cert file failed:", err)
		} else {
			fmt.Println("clear sub cert file success")
		}
		err = settingService.SetSubKeyFile("")
		if err != nil {
			fmt.Println("clear sub key file failed:", err)
		} else {
			fmt.Println("clear sub key file success")
		}
	}
}

func showSetting() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}
	settingService := service.SettingService{}
	allSetting, err := settingService.GetAllSetting()
	if err != nil {
		fmt.Println("get current port failed,error info:", err)
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
}

func getPublicIP() string {
	apis := []string{
		"https://api64.ipify.org",
		"https://ip.sb",
		"https://icanhazip.com",
		"https://ipinfo.io/ip",
		"https://checkip.amazonaws.com",
	}
	type result struct {
		ip  string
		err error
	}
	ch := make(chan result, len(apis))
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 3 * time.Second}

	for _, api := range apis {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			resp, err := client.Get(url)
			if err != nil {
				ch <- result{"", err}
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				ch <- result{"", err}
				return
			}
			ch <- result{string(body), nil}
		}(api)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for res := range ch {
		if res.err == nil && res.ip != "" {
			return strings.TrimSpace(res.ip)
		}
	}
	return ""
}

func getPanelURI() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}
	settingService := service.SettingService{}
	Port, _ := settingService.GetPort()
	BasePath, _ := settingService.GetWebPath()
	Listen, _ := settingService.GetListen()
	Domain, _ := settingService.GetWebDomain()
	KeyFile, _ := settingService.GetKeyFile()
	CertFile, _ := settingService.GetCertFile()
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
		return
	}
	if len(Listen) > 0 {
		fmt.Println(Proto + Listen + PortText + BasePath)
		return
	}
	fmt.Println("Local address:")
	netInterfaces, _ := net.Interfaces()
	for i := 0; i < len(netInterfaces); i++ {
		if len(netInterfaces[i].Flags) > 2 && netInterfaces[i].Flags[0] == "up" && netInterfaces[i].Flags[1] != "loopback" {
			addrs := netInterfaces[i].Addrs
			for _, address := range addrs {
				IP := strings.Split(address.Addr, "/")[0]
				if strings.Contains(address.Addr, ".") {
					fmt.Println(Proto + IP + PortText + BasePath)
				} else if address.Addr[0:6] != "fe80::" {
					fmt.Println(Proto + "[" + IP + "]" + PortText + BasePath)
				}
			}
		}
	}
	pubIP := getPublicIP()
	if pubIP != "" {
		fmt.Printf("\nGlobal address:\n%s%s%s\n", Proto, pubIP, PortText+BasePath)
	}
}
