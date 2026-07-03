package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/gorm"
)

var defaultConfig = `{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [],
    "rules": []
  },
  "route": {
    "rules": [
		  {
        "action": "sniff"
      },
      {
        "protocol": [
          "dns"
        ],
        "action": "hijack-dns"
      }
    ]
  },
  "experimental": {}
}`

var defaultValueMap = map[string]string{
	"webListen":        "",
	"webDomain":        "",
	"webPort":          "2095",
	"secret":           common.Random(32),
	"webCertFile":      "",
	"webKeyFile":       "",
	"webPath":          "/app/",
	"webURI":           "",
	"sessionMaxAge":    "0",
	"trafficAge":       "30",
	"timeLocation":     "Asia/Shanghai",
	"subListen":        "",
	"subPort":          "2096",
	"subPath":          "/sub/",
	"subDomain":        "",
	"subCertFile":      "",
	"subKeyFile":       "",
	"subUpdates":       "12",
	"subEncode":        "true",
	"subShowInfo":      "true",
	"subURI":           "",
	"subMode":          "slave",
	"subMasterSources": "",
	"subJsonExt":       "",
	"subClashExt":      "",
	"config":           defaultConfig,
	"version":          config.GetVersion(),
}

type SettingService struct {
}

type settingPortChange struct {
	oldWebPort     int
	oldSubPort     int
	newWebPort     int
	newSubPort     int
	webPortChanged bool
	subPortChanged bool
}

func (s *SettingService) GetAllSetting() (*map[string]string, error) {
	db := database.GetDB()
	settings := make([]*model.Setting, 0)
	err := db.Model(model.Setting{}).Find(&settings).Error
	if err != nil {
		return nil, err
	}
	allSetting := map[string]string{}

	for _, setting := range settings {
		allSetting[setting.Key] = setting.Value
	}

	missingDefaults := make(map[string]string)
	for key, defaultValue := range defaultValueMap {
		if _, exists := allSetting[key]; !exists {
			missingDefaults[key] = defaultValue
			allSetting[key] = defaultValue
		}
	}
	if len(missingDefaults) > 0 {
		if err := retryWriteTx(func(tx *gorm.DB) error {
			for key, value := range missingDefaults {
				if err := s.saveSettingTx(tx, key, value); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}

	s.fillSubCertFiles(allSetting)

	// Due to security principles
	delete(allSetting, "secret")
	delete(allSetting, "config")
	delete(allSetting, "version")

	return &allSetting, nil
}

func (s *SettingService) fillSubCertFiles(allSetting map[string]string) {
	if allSetting == nil {
		return
	}
	if allSetting["subCertFile"] != "" && allSetting["subKeyFile"] != "" {
		return
	}

	seen := map[string]struct{}{}
	tryDomains := func(domain string) bool {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			return false
		}
		if _, ok := seen[domain]; ok {
			return false
		}
		seen[domain] = struct{}{}

		certFile, keyFile, ok := resolveAcmeCertFiles(domain)
		if !ok {
			return false
		}

		if allSetting["subCertFile"] == "" {
			allSetting["subCertFile"] = certFile
		}
		if allSetting["subKeyFile"] == "" {
			allSetting["subKeyFile"] = keyFile
		}
		return true
	}

	if tryDomains(allSetting["subDomain"]) {
		return
	}
	tryDomains(allSetting["webDomain"])
}

func (s *SettingService) ResetSettings() error {
	return retryWrite(func(db *gorm.DB) error {
		return db.Where("1 = 1").Delete(model.Setting{}).Error
	})
}

func (s *SettingService) getSetting(key string) (*model.Setting, error) {
	db := database.GetDB()
	setting := &model.Setting{}
	err := db.Model(model.Setting{}).Where("key = ?", key).First(setting).Error
	if err != nil {
		return nil, err
	}
	return setting, nil
}

func (s *SettingService) getString(key string) (string, error) {
	setting, err := s.getSetting(key)
	if database.IsNotFound(err) {
		value, ok := defaultValueMap[key]
		if !ok {
			return "", common.NewErrorf("key <%v> not in defaultValueMap", key)
		}
		return value, nil
	} else if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (s *SettingService) saveSetting(key string, value string) error {
	return retryWriteTx(func(tx *gorm.DB) error {
		return s.saveSettingTx(tx, key, value)
	})
}

func (s *SettingService) setString(key string, value string) error {
	return s.saveSetting(key, value)
}

func (s *SettingService) saveSettingTx(tx *gorm.DB, key string, value string) error {
	setting := &model.Setting{}
	err := tx.Model(model.Setting{}).Where("key = ?", key).First(setting).Error
	if database.IsNotFound(err) {
		return tx.Create(&model.Setting{
			Key:   key,
			Value: value,
		}).Error
	} else if err != nil {
		return err
	}
	setting.Key = key
	setting.Value = value
	return tx.Save(setting).Error
}

func (s *SettingService) getBool(key string) (bool, error) {
	str, err := s.getString(key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(str)
}

// func (s *SettingService) setBool(key string, value bool) error {
// 	return s.setString(key, strconv.FormatBool(value))
// }

func (s *SettingService) getInt(key string) (int, error) {
	str, err := s.getString(key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(str)
}

func (s *SettingService) setInt(key string, value int) error {
	return s.setString(key, strconv.Itoa(value))
}
func (s *SettingService) GetListen() (string, error) {
	return s.getString("webListen")
}

func (s *SettingService) GetWebDomain() (string, error) {
	return s.getString("webDomain")
}

func (s *SettingService) GetPort() (int, error) {
	return s.getInt("webPort")
}

func (s *SettingService) SetPort(port int) error {
	return s.setInt("webPort", port)
}

func (s *SettingService) GetCertFile() (string, error) {
	return s.getString("webCertFile")
}

func (s *SettingService) SetCertFile(certFile string) error {
	if certFile != "" {
		if err := s.fileExists(certFile); err != nil {
			return common.NewError(" -> ", certFile, " is not exists")
		}
	}
	return s.setString("webCertFile", certFile)
}

func (s *SettingService) GetKeyFile() (string, error) {
	return s.getString("webKeyFile")
}

func (s *SettingService) SetKeyFile(keyFile string) error {
	if keyFile != "" {
		if err := s.fileExists(keyFile); err != nil {
			return common.NewError(" -> ", keyFile, " is not exists")
		}
	}
	return s.setString("webKeyFile", keyFile)
}

func (s *SettingService) GetWebPath() (string, error) {
	webPath, err := s.getString("webPath")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(webPath, "/") {
		webPath = "/" + webPath
	}
	if !strings.HasSuffix(webPath, "/") {
		webPath += "/"
	}
	return webPath, nil
}

func (s *SettingService) SetWebPath(webPath string) error {
	if !strings.HasPrefix(webPath, "/") {
		webPath = "/" + webPath
	}
	if !strings.HasSuffix(webPath, "/") {
		webPath += "/"
	}
	return s.setString("webPath", webPath)
}

func (s *SettingService) GetSecret() ([]byte, error) {
	secret, err := s.getString("secret")
	if secret == defaultValueMap["secret"] {
		err := s.saveSetting("secret", secret)
		if err != nil {
			logger.Warning("save secret failed:", err)
		}
	}
	return []byte(secret), err
}

func (s *SettingService) GetSessionMaxAge() (int, error) {
	return s.getInt("sessionMaxAge")
}

func (s *SettingService) GetTrafficAge() (int, error) {
	return s.getInt("trafficAge")
}

func (s *SettingService) GetTimeLocation() (*time.Location, error) {
	l, err := s.getString("timeLocation")
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		l = "Local"
	}
	location, err := time.LoadLocation(l)
	if err != nil {
		defaultLocation := defaultValueMap["timeLocation"]
		logger.Errorf("location <%v> not exist, using default location: %v", l, defaultLocation)
		return time.LoadLocation(defaultLocation)
	}
	return location, nil
}

func (s *SettingService) GetSubListen() (string, error) {
	return s.getString("subListen")
}

func (s *SettingService) GetSubPort() (int, error) {
	return s.getInt("subPort")
}

func (s *SettingService) SetSubPort(subPort int) error {
	return s.setInt("subPort", subPort)
}

func (s *SettingService) GetSubPath() (string, error) {
	subPath, err := s.getString("subPath")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(subPath, "/") {
		subPath = "/" + subPath
	}
	if !strings.HasSuffix(subPath, "/") {
		subPath += "/"
	}
	return subPath, nil
}

func (s *SettingService) SetSubPath(subPath string) error {
	if !strings.HasPrefix(subPath, "/") {
		subPath = "/" + subPath
	}
	if !strings.HasSuffix(subPath, "/") {
		subPath += "/"
	}
	return s.setString("subPath", subPath)
}

func (s *SettingService) GetSubDomain() (string, error) {
	return s.getString("subDomain")
}

func (s *SettingService) GetSubCertFile() (string, error) {
	return s.getString("subCertFile")
}

func (s *SettingService) SetSubCertFile(certFile string) error {
	if certFile != "" {
		if err := s.fileExists(certFile); err != nil {
			return common.NewError(" -> ", certFile, " is not exists")
		}
	}
	return s.setString("subCertFile", certFile)
}

func (s *SettingService) GetSubKeyFile() (string, error) {
	return s.getString("subKeyFile")
}

func (s *SettingService) SetSubKeyFile(keyFile string) error {
	if keyFile != "" {
		if err := s.fileExists(keyFile); err != nil {
			return common.NewError(" -> ", keyFile, " is not exists")
		}
	}
	return s.setString("subKeyFile", keyFile)
}

func (s *SettingService) GetSubUpdates() (int, error) {
	return s.getInt("subUpdates")
}

func (s *SettingService) GetSubEncode() (bool, error) {
	return s.getBool("subEncode")
}

func (s *SettingService) GetSubShowInfo() (bool, error) {
	return s.getBool("subShowInfo")
}

func (s *SettingService) GetSubURI() (string, error) {
	return s.getString("subURI")
}

func (s *SettingService) GetSubMode() (string, error) {
	subMode, err := s.getString("subMode")
	if err != nil {
		return "", err
	}
	switch subMode {
	case "master", "slave":
		return subMode, nil
	default:
		return "slave", nil
	}
}

func (s *SettingService) GetSubMasterSources() ([]string, error) {
	rawSources, err := s.getString("subMasterSources")
	if err != nil {
		return nil, err
	}
	rawSources = strings.ReplaceAll(rawSources, "\r\n", "\n")
	rawSources = strings.ReplaceAll(rawSources, "\r", "\n")

	sources := make([]string, 0)
	for _, line := range strings.Split(rawSources, "\n") {
		source := strings.TrimSpace(line)
		if source == "" || strings.HasPrefix(source, "#") {
			continue
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func (s *SettingService) GetFinalSubURI(host string) (string, error) {
	allSetting, err := s.GetAllSetting()
	if err != nil {
		return "", err
	}
	SubURI := (*allSetting)["subURI"]
	if SubURI != "" {
		return SubURI, nil
	}
	protocol := "http"
	if (*allSetting)["subKeyFile"] != "" && (*allSetting)["subCertFile"] != "" {
		protocol = "https"
	}
	if (*allSetting)["subDomain"] != "" {
		host = (*allSetting)["subDomain"]
	}
	port := ":" + (*allSetting)["subPort"]
	if (port == "80" && protocol == "http") || (port == "443" && protocol == "https") {
		port = ""
	}
	return protocol + "://" + host + port + (*allSetting)["subPath"], nil
}

func (s *SettingService) GetConfig() (string, error) {
	return s.getString("config")
}

func (s *SettingService) SetConfig(config string) error {
	return s.setString("config", config)
}

func (s *SettingService) SaveConfig(tx *gorm.DB, config json.RawMessage) error {
	configs, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return tx.Model(model.Setting{}).Where("key = ?", "config").Update("value", string(configs)).Error
}

func (s *SettingService) preparePortChange(tx *gorm.DB, settings map[string]string) (settingPortChange, error) {
	change := settingPortChange{}

	oldWebPort, err := s.GetPort()
	if err != nil {
		return change, err
	}
	oldSubPort, err := s.GetSubPort()
	if err != nil {
		return change, err
	}

	change.oldWebPort = oldWebPort
	change.oldSubPort = oldSubPort
	change.newWebPort = oldWebPort
	change.newSubPort = oldSubPort

	if rawWebPort, ok := settings["webPort"]; ok && rawWebPort != "" {
		change.newWebPort, err = strconv.Atoi(rawWebPort)
		if err != nil {
			return change, err
		}
		change.webPortChanged = change.newWebPort != change.oldWebPort
	}
	if rawSubPort, ok := settings["subPort"]; ok && rawSubPort != "" {
		change.newSubPort, err = strconv.Atoi(rawSubPort)
		if err != nil {
			return change, err
		}
		change.subPortChanged = change.newSubPort != change.oldSubPort
	}
	if change.webPortChanged || change.subPortChanged {
		if err := ValidateManagedPanelPortsWithConflicts(tx, change.newWebPort, change.newSubPort); err != nil {
			return change, err
		}
	}

	return change, nil
}

func (s *SettingService) normalizeSettingValue(key, value string) string {
	switch key {
	case "webPath", "subPath":
		if !strings.HasPrefix(value, "/") {
			value = "/" + value
		}
		if !strings.HasSuffix(value, "/") {
			value += "/"
		}
	}
	return value
}

func (s *SettingService) validateSettingValue(key, value string) error {
	if value == "" {
		return nil
	}
	switch key {
	case "webCertFile", "webKeyFile", "subCertFile", "subKeyFile":
		if err := s.fileExists(value); err != nil {
			return common.NewError(" -> ", value, " is not exists")
		}
	}
	return nil
}

func (s *SettingService) applySettingSideEffects(tx *gorm.DB, key, value string) error {
	if key == "trafficAge" && value == "0" {
		return tx.Where("id > 0").Delete(model.Stats{}).Error
	}
	return nil
}

func (s *SettingService) buildSavePostCommit(change settingPortChange) func() error {
	if !change.webPortChanged && !change.subPortChanged {
		return nil
	}
	return func() error {
		return s.SyncManagedPanelPortForwarding(change.oldWebPort, change.newWebPort, change.oldSubPort, change.newSubPort)
	}
}

func (s *SettingService) Save(tx *gorm.DB, data json.RawMessage) (func() error, error) {
	var settings map[string]string
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	portChange, err := s.preparePortChange(tx, settings)
	if err != nil {
		return nil, err
	}

	for key, value := range settings {
		value = s.normalizeSettingValue(key, value)
		if err := s.validateSettingValue(key, value); err != nil {
			return nil, err
		}
		if err := s.applySettingSideEffects(tx, key, value); err != nil {
			return nil, err
		}
		if err := s.saveSettingTx(tx, key, value); err != nil {
			return nil, err
		}
	}

	return s.buildSavePostCommit(portChange), nil
}

func (s *SettingService) GetSubJsonExt() (string, error) {
	return s.getString("subJsonExt")
}

func (s *SettingService) GetSubClashExt() (string, error) {
	return s.getString("subClashExt")
}

func (s *SettingService) fileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return common.NewErrorf("%s is a directory, not a file", path)
	}
	if info.Size() <= 0 {
		return common.NewErrorf("%s is empty", path)
	}
	return nil
}

func resolveAcmeCertFiles(domain string) (string, string, bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return "", "", false
	}
	return resolveAcmeCertFilesFromHome(homeDir, domain)
}

func resolveAcmeCertFilesFromHome(homeDir, domain string) (string, string, bool) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", "", false
	}

	candidates := []string{
		filepath.Join(homeDir, ".acme.sh", domain+"_ecc"),
		filepath.Join(homeDir, ".acme.sh", domain),
	}

	for _, certDir := range candidates {
		certFile := filepath.Join(certDir, "fullchain.cer")
		keyFile := filepath.Join(certDir, domain+".key")
		if err := fileMustExist(certFile); err != nil {
			continue
		}
		if err := fileMustExist(keyFile); err != nil {
			continue
		}
		return certFile, keyFile, true
	}

	return "", "", false
}

func fileMustExist(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Size() <= 0 {
		return common.NewErrorf("%s is invalid", path)
	}
	return nil
}
