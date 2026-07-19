package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"

	"github.com/sagernet/sing-box/common/tls"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type ServerService struct{}

func (s *ServerService) GetStatus(request string) *map[string]interface{} {
	status := make(map[string]interface{}, 0)
	requests := strings.Split(request, ",")
	for _, req := range requests {
		switch req {
		case "cpu":
			status["cpu"] = getCachedStatusValue("cpu", func() interface{} { return s.GetCpuPercent() })
		case "mem":
			status["mem"] = getCachedStatusValue("mem", func() interface{} { return s.GetMemInfo() })
		case "dsk":
			status["dsk"] = getCachedStatusValue("dsk", func() interface{} { return s.GetDiskInfo() })
		case "dio":
			status["dio"] = getCachedStatusValue("dio", func() interface{} { return s.GetDiskIO() })
		case "swp":
			status["swp"] = getCachedStatusValue("swp", func() interface{} { return s.GetSwapInfo() })
		case "net":
			status["net"] = getCachedStatusValue("net", func() interface{} { return s.GetNetInfo() })
		case "sys":
			status["sys"] = getCachedStatusValue("sys", func() interface{} { return s.GetSystemInfo() })
		case "sbd":
			status["sbd"] = getCachedStatusValue("sbd", func() interface{} { return s.GetSingboxInfo() })
		case "db":
			status["db"] = getCachedStatusValue("db", func() interface{} { return s.GetDatabaseInfo() })
		}
	}
	return &status
}

func (s *ServerService) GetPublicIP() string {
	now := time.Now()
	serverStatusCache.mu.RLock()
	entry, ok := serverStatusCache.entries["publicip"]
	serverStatusCache.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		if ip, ok := entry.value.(string); ok {
			return ip
		}
	}

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
			ch <- result{strings.TrimSpace(string(body)), nil}
		}(api)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for res := range ch {
		if res.err == nil && isIPv4Literal(res.ip) {
			serverStatusCache.mu.Lock()
			serverStatusCache.entries["publicip"] = statusCacheEntry{
				expiresAt: time.Now().Add(statusCacheTTL["publicip"]),
				value:     res.ip,
			}
			serverStatusCache.mu.Unlock()
			return res.ip
		}
	}
	return ""
}

func isIPv4Literal(value string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	return err == nil && addr.Is4()
}

func (s *ServerService) GetCpuPercent() float64 {
	percents, err := cpu.Percent(0, false)
	if err != nil {
		logger.Warning("get cpu percent failed:", err)
		return 0
	} else {
		return percents[0]
	}
}

func (s *ServerService) GetMemInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		logger.Warning("get virtual memory failed:", err)
	} else {
		info["current"] = memInfo.Used
		info["total"] = memInfo.Total
	}
	return info
}

func (s *ServerService) GetDiskInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	diskInfo, err := disk.Usage("/")
	if err != nil {
		logger.Warning("get disk usage failed:", err)
	} else {
		info["current"] = diskInfo.Used
		info["total"] = diskInfo.Total
	}
	return info
}

func (s *ServerService) GetDiskIO() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	ioStats, err := disk.IOCounters()
	if err != nil {
		logger.Warning("get disk io counters failed:", err)
	} else if len(ioStats) > 0 {
		infoR, infoW := uint64(0), uint64(0)
		for _, ioStat := range ioStats {
			infoR += ioStat.ReadBytes
			infoW += ioStat.WriteBytes
		}
		info["read"] = infoR
		info["write"] = infoW
	} else {
		logger.Warning("can not find disk io counters")
	}
	return info
}

func (s *ServerService) GetSwapInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	swapInfo, err := mem.SwapMemory()
	if err != nil {
		logger.Warning("get swap memory failed:", err)
	} else {
		info["current"] = swapInfo.Used
		info["total"] = swapInfo.Total
	}
	return info
}

func (s *ServerService) GetNetInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	ioStats, err := net.IOCounters(false)
	if err != nil {
		logger.Warning("get io counters failed:", err)
	} else if len(ioStats) > 0 {
		ioStat := ioStats[0]
		info["sent"] = ioStat.BytesSent
		info["recv"] = ioStat.BytesRecv
		info["psent"] = ioStat.PacketsSent
		info["precv"] = ioStat.PacketsRecv
	} else {
		logger.Warning("can not find io counters")
	}
	return info
}

func (s *ServerService) GetSingboxInfo() map[string]interface{} {
	var rtm runtime.MemStats
	runtime.ReadMemStats(&rtm)
	isRunning := corePtr.IsRunning()
	uptime := uint32(0)
	if isRunning {
		uptime = corePtr.GetInstance().Uptime()
	}
	return map[string]interface{}{
		"running": isRunning,
		"stats": map[string]interface{}{
			"NumGoroutine": uint32(runtime.NumGoroutine()),
			"Alloc":        rtm.Alloc,
			"Uptime":       uptime,
		},
	}
}

func (s *ServerService) GetSystemInfo() map[string]interface{} {
	info := make(map[string]interface{}, 0)
	var rtm runtime.MemStats
	runtime.ReadMemStats(&rtm)

	info["appMem"] = rtm.Sys
	info["appThreads"] = uint32(runtime.NumGoroutine())
	cpuInfo, err := cpu.Info()
	if err == nil {
		info["cpuType"] = cpuInfo[0].ModelName
	}
	info["cpuCount"] = runtime.NumCPU()
	info["hostName"], _ = os.Hostname()
	info["appVersion"] = config.GetVersion()
	info["firewallBackend"] = detectFirewallBackend()
	ipv4 := make([]string, 0)
	ipv6 := make([]string, 0)
	// get ip address
	netInterfaces, _ := net.Interfaces()
	for i := 0; i < len(netInterfaces); i++ {
		if len(netInterfaces[i].Flags) > 2 && netInterfaces[i].Flags[0] == "up" && netInterfaces[i].Flags[1] != "loopback" {
			addrs := netInterfaces[i].Addrs

			for _, address := range addrs {
				if strings.Contains(address.Addr, ".") {
					ipv4 = append(ipv4, address.Addr)
				} else if address.Addr[0:6] != "fe80::" {
					ipv6 = append(ipv6, address.Addr)
				}
			}
		}
	}
	info["ipv4"] = ipv4
	info["ipv6"] = ipv6
	info["bootTime"], _ = host.BootTime()

	return info
}

func (s *ServerService) GetLogs(count string, level string) []string {
	c, err := strconv.Atoi(count)
	if err != nil {
		c = 10
	}
	return logger.GetLogs(c, level)
}

func (s *ServerService) GenKeypair(keyType string, options string) []string {
	if len(keyType) == 0 {
		return []string{"No keypair to generate"}
	}

	switch keyType {
	case "ech":
		return s.generateECHKeyPair(options)
	case "tls":
		return s.generateTLSKeyPair(options)
	case "reality":
		return s.generateRealityKeyPair()
	case "wireguard":
		return s.generateWireGuardKey(options)
	case "masque":
		return s.generateMasqueKey(options)
	}

	return []string{"Failed to generate keypair"}
}

func (s *ServerService) generateECHKeyPair(serverName string) []string {
	configPem, keyPem, err := tls.ECHKeygenDefault(serverName)
	if err != nil {
		return []string{"Failed to generate ECH keypair: ", err.Error()}
	}
	return append(strings.Split(configPem, "\n"), strings.Split(keyPem, "\n")...)
}

func (s *ServerService) generateTLSKeyPair(serverName string) []string {
	privateKeyPem, publicKeyPem, err := tls.GenerateCertificate(nil, nil, time.Now, serverName, time.Now().AddDate(0, 12, 0))
	if err != nil {
		return []string{"Failed to generate TLS keypair: ", err.Error()}
	}
	return append(strings.Split(string(privateKeyPem), "\n"), strings.Split(string(publicKeyPem), "\n")...)
}

func (s *ServerService) generateRealityKeyPair() []string {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return []string{"Failed to generate Reality keypair: ", err.Error()}
	}
	publicKey := privateKey.PublicKey()
	return []string{"PrivateKey: " + base64.RawURLEncoding.EncodeToString(privateKey[:]), "PublicKey: " + base64.RawURLEncoding.EncodeToString(publicKey[:])}
}

func (s *ServerService) generateWireGuardKey(pk string) []string {
	if len(pk) > 0 {
		key, _ := wgtypes.ParseKey(pk)
		return []string{key.PublicKey().String()}
	}
	wgKeys, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return []string{"Failed to generate wireguard keypair: ", err.Error()}
	}
	return []string{"PrivateKey: " + wgKeys.String(), "PublicKey: " + wgKeys.PublicKey().String()}
}

func (s *ServerService) generateMasqueKey(pk string) []string {
	if len(strings.TrimSpace(pk)) > 0 {
		pub, err := masquePublicKeyFromPrivate(pk)
		if err != nil {
			return []string{"Failed to generate masque keypair: ", err.Error()}
		}
		return []string{"PublicKey: " + pub}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return []string{"Failed to generate masque keypair: ", err.Error()}
	}
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return []string{"Failed to generate masque keypair: ", err.Error()}
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return []string{"Failed to generate masque keypair: ", err.Error()}
	}

	return []string{
		"PrivateKey: " + base64.StdEncoding.EncodeToString(privDER),
		"PublicKey: " + base64.StdEncoding.EncodeToString(pubDER),
	}
}

func masquePublicKeyFromPrivate(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty private key")
	}

	var data []byte
	if block, _ := pem.Decode([]byte(trimmed)); block != nil {
		data = block.Bytes
	} else {
		data, _ = base64.StdEncoding.DecodeString(trimmed)
		if len(data) == 0 {
			data, _ = base64.RawStdEncoding.DecodeString(trimmed)
		}
		if len(data) == 0 {
			return "", errors.New("invalid base64 ECDSA private key")
		}
	}

	priv, err := x509.ParseECPrivateKey(data)
	if err != nil {
		return "", err
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pubDER), nil
}

func (s *ServerService) GetDatabaseInfo() map[string]int64 {
	if cached, ok := databaseInfoCache.get("database", 0); ok {
		if info, ok := cached.(map[string]int64); ok {
			return cloneDatabaseInfo(info)
		}
	}

	info := make(map[string]int64, 0)
	db := database.GetDB()
	if db == nil {
		return nil
	}

	var clientsCount, inboundsCount, outboundsCount, servicesCount, endpointsCount, clientUp, clientDown int64
	queries := []struct {
		name  string
		query func() error
	}{
		{name: "clients", query: func() error { return db.Model(&model.Client{}).Count(&clientsCount).Error }},
		{name: "inbounds", query: func() error { return db.Model(&model.Inbound{}).Count(&inboundsCount).Error }},
		{name: "outbounds", query: func() error { return db.Model(&model.Outbound{}).Count(&outboundsCount).Error }},
		{name: "services", query: func() error { return db.Model(&model.Service{}).Count(&servicesCount).Error }},
		{name: "endpoints", query: func() error { return db.Model(&model.Endpoint{}).Count(&endpointsCount).Error }},
		{name: "client up", query: func() error {
			return db.Model(&model.Client{}).Select("COALESCE(SUM(up+total_up),0)").Scan(&clientUp).Error
		}},
		{name: "client down", query: func() error {
			return db.Model(&model.Client{}).Select("COALESCE(SUM(down+total_down),0)").Scan(&clientDown).Error
		}},
	}
	for _, item := range queries {
		if err := item.query(); err != nil {
			logger.Warning("load database info failed for ", item.name, ": ", err)
			return nil
		}
	}

	info["clients"] = clientsCount
	info["inbounds"] = inboundsCount
	info["outbounds"] = outboundsCount
	info["services"] = servicesCount
	info["endpoints"] = endpointsCount
	info["clientUp"] = clientUp
	info["clientDown"] = clientDown

	databaseInfoCache.set("database", 0, databaseInfoCacheTTL, cloneDatabaseInfo(info))
	return info
}
