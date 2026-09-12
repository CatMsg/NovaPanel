package service

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

const (
	TrafficBudgetModeTX   = "tx"
	TrafficBudgetModeBoth = "rx_tx"
	TrafficBudgetModeMax  = "max"
)

var (
	trafficBudgetMu               sync.Mutex
	trafficBudgetBlocked          atomic.Bool
	trafficBudgetStatusMu         sync.RWMutex
	trafficBudgetLastStatus       TrafficBudgetStatus
	trafficBudgetRuntimeSupported = func() bool { return runtime.GOOS == "linux" }
	trafficBudgetCounterReader    = readLinuxInterfaceCounters
	trafficBudgetStopDataPlane    = stopProxyDataPlaneForTrafficBudget
	trafficBudgetRestoreDataPlane = restoreProxyDataPlaneAfterTrafficBudget
	trafficBudgetRuntimeState     atomic.Int32
)

type TrafficBudgetService struct{ SettingService }

type trafficBudgetConfig struct {
	Enabled         bool
	LimitBytes      uint64
	ReserveBytes    uint64
	OffsetBytes     uint64
	AccountingMode  string
	Interface       string
	CycleDay        int
	CycleHour       int
	WarningPercent  float64
	CriticalPercent float64
	Location        *time.Location
}

type trafficBudgetState struct {
	PeriodStart   int64
	AccumulatedRx uint64
	AccumulatedTx uint64
	LastRx        uint64
	LastTx        uint64
	LastInterface string
}

type TrafficBudgetStatus struct {
	Enabled                bool    `json:"enabled"`
	Supported              bool    `json:"supported"`
	Interface              string  `json:"interface"`
	AccountingMode         string  `json:"accountingMode"`
	LimitBytes             uint64  `json:"limitBytes"`
	ReserveBytes           uint64  `json:"reserveBytes"`
	ClientPoolBytes        uint64  `json:"clientPoolBytes"`
	OffsetBytes            uint64  `json:"offsetBytes"`
	MeteredRxBytes         uint64  `json:"meteredRxBytes"`
	MeteredTxBytes         uint64  `json:"meteredTxBytes"`
	UsedBytes              uint64  `json:"usedBytes"`
	ProviderRemainingBytes uint64  `json:"providerRemainingBytes"`
	PoolRemainingBytes     uint64  `json:"poolRemainingBytes"`
	UsedPercent            float64 `json:"usedPercent"`
	WarningPercent         float64 `json:"warningPercent"`
	CriticalPercent        float64 `json:"criticalPercent"`
	Level                  string  `json:"level"`
	Blocked                bool    `json:"blocked"`
	PeriodStart            string  `json:"periodStart,omitempty"`
	PeriodEnd              string  `json:"periodEnd,omitempty"`
	SampledAt              string  `json:"sampledAt,omitempty"`
	Error                  string  `json:"error,omitempty"`
}

func GetTrafficBudgetService() *TrafficBudgetService { return &TrafficBudgetService{} }
func IsTrafficBudgetBlocked() bool                   { return trafficBudgetBlocked.Load() }

func (s *TrafficBudgetService) Initialize() error { return s.CheckAndEnforce() }
func (s *TrafficBudgetService) Reconcile() error  { return s.CheckAndEnforce() }

func (s *TrafficBudgetService) GetStatus() TrafficBudgetStatus {
	if err := s.CheckAndEnforce(); err != nil {
		trafficBudgetStatusMu.Lock()
		status := trafficBudgetLastStatus
		if status.Level == "" {
			status.Level = "error"
		}
		status.Error = err.Error()
		trafficBudgetLastStatus = status
		trafficBudgetStatusMu.Unlock()
	}
	trafficBudgetStatusMu.RLock()
	defer trafficBudgetStatusMu.RUnlock()
	return trafficBudgetLastStatus
}

func (s *TrafficBudgetService) CheckAndEnforce() error {
	trafficBudgetMu.Lock()
	cfg, err := s.loadTrafficBudgetConfig(database.GetDB())
	if err != nil {
		trafficBudgetMu.Unlock()
		if trafficBudgetRuntimeSupported() {
			status := TrafficBudgetStatus{Enabled: true, Supported: true, Level: "error", SampledAt: time.Now().Format(time.RFC3339)}
			return s.failTrafficBudgetClosed(status, err)
		}
		return s.recordTrafficBudgetError(err)
	}
	now := time.Now()
	status := newTrafficBudgetStatus(cfg, now)
	if !cfg.Enabled {
		status.Level = "disabled"
		status.Supported = trafficBudgetRuntimeSupported()
		trafficBudgetMu.Unlock()
		return s.finishTrafficBudgetStatus(status, false)
	}
	if !trafficBudgetRuntimeSupported() {
		status.Level = "unsupported"
		status.Supported = false
		status.Error = "服务器总流量保护仅在 Linux 上执行；配置会保留"
		trafficBudgetMu.Unlock()
		return s.finishTrafficBudgetStatus(status, false)
	}

	iface, rx, tx, err := trafficBudgetCounterReader(cfg.Interface)
	if err != nil {
		trafficBudgetMu.Unlock()
		return s.failTrafficBudgetClosed(status, err)
	}
	status.Supported = true
	status.Interface = iface
	periodStart, periodEnd := trafficBudgetPeriod(now, cfg.CycleDay, cfg.CycleHour, cfg.Location)
	status.PeriodStart, status.PeriodEnd = periodStart.Format(time.RFC3339), periodEnd.Format(time.RFC3339)
	state, err := s.loadTrafficBudgetState(database.GetDB())
	if err != nil {
		trafficBudgetMu.Unlock()
		return s.failTrafficBudgetClosed(status, err)
	}

	offset := cfg.OffsetBytes
	if state.PeriodStart == 0 {
		state = trafficBudgetState{PeriodStart: periodStart.Unix(), LastRx: rx, LastTx: tx, LastInterface: iface}
	} else if state.PeriodStart != periodStart.Unix() {
		state = trafficBudgetState{PeriodStart: periodStart.Unix(), LastRx: rx, LastTx: tx, LastInterface: iface}
		offset = 0
	} else if state.LastInterface != iface {
		state.LastRx, state.LastTx, state.LastInterface = rx, tx, iface
	} else {
		state.AccumulatedRx = saturatingAdd(state.AccumulatedRx, counterDelta(state.LastRx, rx))
		state.AccumulatedTx = saturatingAdd(state.AccumulatedTx, counterDelta(state.LastTx, tx))
		state.LastRx, state.LastTx = rx, tx
	}
	if err := s.persistTrafficBudgetState(state, offset, offset != cfg.OffsetBytes); err != nil {
		trafficBudgetMu.Unlock()
		return s.failTrafficBudgetClosed(status, err)
	}

	status.OffsetBytes = offset
	status.MeteredRxBytes = state.AccumulatedRx
	status.MeteredTxBytes = state.AccumulatedTx
	metered := trafficBudgetUsage(cfg.AccountingMode, state.AccumulatedRx, state.AccumulatedTx)
	status.UsedBytes = saturatingAdd(offset, metered)
	status.ProviderRemainingBytes = saturatingSub(cfg.LimitBytes, status.UsedBytes)
	status.PoolRemainingBytes = saturatingSub(status.ClientPoolBytes, status.UsedBytes)
	if status.ClientPoolBytes > 0 {
		status.UsedPercent = math.Min(999.9, float64(status.UsedBytes)*100/float64(status.ClientPoolBytes))
	}
	blocked := status.ClientPoolBytes == 0 || status.UsedBytes >= status.ClientPoolBytes
	switch {
	case blocked:
		status.Level = "blocked"
	case status.UsedPercent >= cfg.CriticalPercent:
		status.Level = "critical"
	case status.UsedPercent >= cfg.WarningPercent:
		status.Level = "warning"
	default:
		status.Level = "normal"
	}
	status.SampledAt = now.Format(time.RFC3339)
	trafficBudgetMu.Unlock()
	return s.finishTrafficBudgetStatus(status, blocked)
}

func newTrafficBudgetStatus(cfg trafficBudgetConfig, now time.Time) TrafficBudgetStatus {
	pool := saturatingSub(cfg.LimitBytes, cfg.ReserveBytes)
	return TrafficBudgetStatus{
		Enabled: cfg.Enabled, Supported: true, Interface: cfg.Interface,
		AccountingMode: cfg.AccountingMode, LimitBytes: cfg.LimitBytes,
		ReserveBytes: cfg.ReserveBytes, ClientPoolBytes: pool, OffsetBytes: cfg.OffsetBytes,
		WarningPercent: cfg.WarningPercent, CriticalPercent: cfg.CriticalPercent,
		Level: "normal", Blocked: IsTrafficBudgetBlocked(), SampledAt: now.Format(time.RFC3339),
	}
}

func (s *TrafficBudgetService) finishTrafficBudgetStatus(status TrafficBudgetStatus, blocked bool) error {
	trafficBudgetBlocked.Store(blocked)
	status.Blocked = blocked
	s.storeTrafficBudgetStatus(status)

	// runtimeState: 0 = normal, 1 = blocked successfully, -1 = transition partially failed.
	// A failed transition stays retryable on every budget check.
	applied := trafficBudgetRuntimeState.Load()
	if blocked {
		if applied == 1 {
			return nil
		}
		logger.Warning("VPS traffic budget reached; stopping proxy data plane")
		if err := trafficBudgetStopDataPlane(); err != nil {
			trafficBudgetRuntimeState.Store(-1)
			return err
		}
		trafficBudgetRuntimeState.Store(1)
		return nil
	}
	if applied == 0 {
		return nil
	}
	logger.Info("VPS traffic budget protection cleared; restoring proxy data plane")
	if err := trafficBudgetRestoreDataPlane(); err != nil {
		trafficBudgetRuntimeState.Store(-1)
		return err
	}
	trafficBudgetRuntimeState.Store(0)
	return nil
}

func (s *TrafficBudgetService) storeTrafficBudgetStatus(status TrafficBudgetStatus) {
	trafficBudgetStatusMu.Lock()
	trafficBudgetLastStatus = status
	trafficBudgetStatusMu.Unlock()
}

func (s *TrafficBudgetService) failTrafficBudgetClosed(status TrafficBudgetStatus, cause error) error {
	status.Level = "error"
	status.Error = cause.Error()
	status.SampledAt = time.Now().Format(time.RFC3339)
	enforceErr := s.finishTrafficBudgetStatus(status, true)
	return errors.Join(cause, enforceErr)
}

func (s *TrafficBudgetService) recordTrafficBudgetError(err error) error {
	trafficBudgetStatusMu.Lock()
	status := trafficBudgetLastStatus
	status.Level, status.Error, status.SampledAt = "error", err.Error(), time.Now().Format(time.RFC3339)
	trafficBudgetLastStatus = status
	trafficBudgetStatusMu.Unlock()
	return err
}

func (s *TrafficBudgetService) loadTrafficBudgetConfig(db *gorm.DB) (trafficBudgetConfig, error) {
	get := func(key string) (string, error) { return s.getStringTx(db, key) }
	enabledRaw, err := get("trafficBudgetEnabled")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	enabled, err := strconv.ParseBool(enabledRaw)
	if err != nil {
		return trafficBudgetConfig{}, fmt.Errorf("invalid traffic budget enabled value: %w", err)
	}
	limit, err := parseUintSetting(get, "trafficBudgetLimitBytes")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	reserve, err := parseUintSetting(get, "trafficBudgetReserveBytes")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	offset, err := parseUintSetting(get, "trafficBudgetOffsetBytes")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	mode, err := get("trafficBudgetAccountingMode")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	iface, err := get("trafficBudgetInterface")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	day, err := parseIntSetting(get, "trafficBudgetCycleDay")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	hour, err := parseIntSetting(get, "trafficBudgetCycleHour")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	warning, err := parseFloatSetting(get, "trafficBudgetWarningPercent")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	critical, err := parseFloatSetting(get, "trafficBudgetCriticalPercent")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	locationName, err := get("timeLocation")
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	loc, err := time.LoadLocation(locationName)
	if err != nil {
		return trafficBudgetConfig{}, err
	}
	cfg := trafficBudgetConfig{Enabled: enabled, LimitBytes: limit, ReserveBytes: reserve, OffsetBytes: offset,
		AccountingMode: strings.TrimSpace(mode), Interface: strings.TrimSpace(iface), CycleDay: day, CycleHour: hour,
		WarningPercent: warning, CriticalPercent: critical, Location: loc}
	if err := validateTrafficBudgetConfig(cfg); err != nil {
		return trafficBudgetConfig{}, err
	}
	return cfg, nil
}

func validateTrafficBudgetSettingsTx(tx *gorm.DB) error {
	_, err := (&TrafficBudgetService{}).loadTrafficBudgetConfig(tx)
	return err
}

func validateTrafficBudgetConfig(cfg trafficBudgetConfig) error {
	if cfg.AccountingMode != TrafficBudgetModeTX && cfg.AccountingMode != TrafficBudgetModeBoth && cfg.AccountingMode != TrafficBudgetModeMax {
		return common.NewError("流量计费方式必须是 tx、rx_tx 或 max")
	}
	if cfg.CycleDay < 1 || cfg.CycleDay > 31 {
		return common.NewError("流量重置日必须在 1 到 31 之间")
	}
	if cfg.CycleHour < 0 || cfg.CycleHour > 23 {
		return common.NewError("流量重置小时必须在 0 到 23 之间")
	}
	if cfg.WarningPercent <= 0 || cfg.WarningPercent >= 100 {
		return common.NewError("流量预警阈值必须大于 0 且小于 100")
	}
	if cfg.CriticalPercent <= cfg.WarningPercent || cfg.CriticalPercent >= 100 {
		return common.NewError("流量严重阈值必须大于预警阈值且小于 100")
	}
	if strings.Contains(cfg.Interface, "/") || strings.Contains(cfg.Interface, "..") || len(cfg.Interface) > 64 {
		return common.NewError("网卡名称无效")
	}
	if cfg.Enabled {
		if cfg.LimitBytes == 0 {
			return common.NewError("启用 VPS 总流量保护前必须设置套餐总流量")
		}
		if cfg.ReserveBytes >= cfg.LimitBytes {
			return common.NewError("安全预留必须小于套餐总流量")
		}
		if cfg.OffsetBytes > cfg.LimitBytes {
			return common.NewError("本周期已用基线不能大于套餐总流量")
		}
	}
	return nil
}

func (s *TrafficBudgetService) loadTrafficBudgetState(db *gorm.DB) (trafficBudgetState, error) {
	get := func(key string) (string, error) { return s.getStringTx(db, key) }
	period, err := parseInt64Setting(get, "trafficBudgetPeriodStart")
	if err != nil {
		return trafficBudgetState{}, err
	}
	rx, err := parseUintSetting(get, "trafficBudgetAccumulatedRx")
	if err != nil {
		return trafficBudgetState{}, err
	}
	tx, err := parseUintSetting(get, "trafficBudgetAccumulatedTx")
	if err != nil {
		return trafficBudgetState{}, err
	}
	lastRx, err := parseUintSetting(get, "trafficBudgetLastRx")
	if err != nil {
		return trafficBudgetState{}, err
	}
	lastTx, err := parseUintSetting(get, "trafficBudgetLastTx")
	if err != nil {
		return trafficBudgetState{}, err
	}
	iface, err := get("trafficBudgetLastInterface")
	if err != nil {
		return trafficBudgetState{}, err
	}
	return trafficBudgetState{PeriodStart: period, AccumulatedRx: rx, AccumulatedTx: tx, LastRx: lastRx, LastTx: lastTx, LastInterface: iface}, nil
}

func (s *TrafficBudgetService) persistTrafficBudgetState(state trafficBudgetState, offset uint64, persistOffset bool) error {
	return retryWriteTx(func(tx *gorm.DB) error {
		values := map[string]string{
			"trafficBudgetPeriodStart":   strconv.FormatInt(state.PeriodStart, 10),
			"trafficBudgetAccumulatedRx": strconv.FormatUint(state.AccumulatedRx, 10),
			"trafficBudgetAccumulatedTx": strconv.FormatUint(state.AccumulatedTx, 10),
			"trafficBudgetLastRx":        strconv.FormatUint(state.LastRx, 10),
			"trafficBudgetLastTx":        strconv.FormatUint(state.LastTx, 10),
			"trafficBudgetLastInterface": state.LastInterface,
		}
		if persistOffset {
			values["trafficBudgetOffsetBytes"] = strconv.FormatUint(offset, 10)
		}
		for key, value := range values {
			if err := s.saveSettingTx(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

func trafficBudgetPeriod(now time.Time, day, hour int, loc *time.Location) (time.Time, time.Time) {
	local := now.In(loc)
	current := trafficBudgetBoundary(local.Year(), local.Month(), day, hour, loc)
	if local.Before(current) {
		prev := local.AddDate(0, -1, 0)
		current = trafficBudgetBoundary(prev.Year(), prev.Month(), day, hour, loc)
	}
	nextMonth := time.Date(current.Year(), current.Month()+1, 1, 0, 0, 0, 0, loc)
	next := trafficBudgetBoundary(nextMonth.Year(), nextMonth.Month(), day, hour, loc)
	return current, next
}

func trafficBudgetBoundary(year int, month time.Month, day, hour int, loc *time.Location) time.Time {
	lastDay := time.Date(year, month+1, 0, hour, 0, 0, 0, loc).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, hour, 0, 0, 0, loc)
}

func trafficBudgetUsage(mode string, rx, tx uint64) uint64 {
	switch mode {
	case TrafficBudgetModeBoth:
		return saturatingAdd(rx, tx)
	case TrafficBudgetModeMax:
		if rx > tx {
			return rx
		}
		return tx
	default:
		return tx
	}
}

func counterDelta(previous, current uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	return current
}
func saturatingAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}
func saturatingSub(a, b uint64) uint64 {
	if b >= a {
		return 0
	}
	return a - b
}

func parseUintSetting(get func(string) (string, error), key string) (uint64, error) {
	raw, err := get(key)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}
func parseIntSetting(get func(string) (string, error), key string) (int, error) {
	raw, err := get(key)
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}
func parseInt64Setting(get func(string) (string, error), key string) (int64, error) {
	raw, err := get(key)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}
func parseFloatSetting(get func(string) (string, error), key string) (float64, error) {
	raw, err := get(key)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}

func readLinuxInterfaceCounters(requested string) (string, uint64, uint64, error) {
	if runtime.GOOS != "linux" {
		return "", 0, 0, common.NewError("network interface traffic accounting is only supported on Linux")
	}
	iface := strings.TrimSpace(requested)
	if iface == "" || iface == "auto" {
		var err error
		iface, err = linuxDefaultRouteInterface()
		if err != nil {
			return "", 0, 0, err
		}
	}
	if strings.Contains(iface, "/") || strings.Contains(iface, "..") {
		return "", 0, 0, common.NewError("invalid network interface")
	}
	read := func(name string) (uint64, error) {
		raw, err := os.ReadFile(filepath.Join("/sys/class/net", iface, "statistics", name))
		if err != nil {
			return 0, err
		}
		return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	}
	rx, err := read("rx_bytes")
	if err != nil {
		return "", 0, 0, fmt.Errorf("read %s rx bytes: %w", iface, err)
	}
	tx, err := read("tx_bytes")
	if err != nil {
		return "", 0, 0, fmt.Errorf("read %s tx bytes: %w", iface, err)
	}
	return iface, rx, tx, nil
}

func linuxDefaultRouteInterface() (string, error) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	first := true
	best := ""
	bestMetric := int64(math.MaxInt64)
	for scanner.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 64)
		if err != nil || flags&1 == 0 {
			continue
		}
		metric, err := strconv.ParseInt(fields[6], 10, 64)
		if err != nil {
			metric = math.MaxInt64 - 1
		}
		if best == "" || metric < bestMetric {
			best, bestMetric = fields[0], metric
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if best != "" {
		return best, nil
	}
	if ipv6, err := linuxDefaultIPv6RouteInterface(); err == nil && ipv6 != "" {
		return ipv6, nil
	}
	return "", common.NewError("无法自动识别默认公网网卡，请手动填写网卡名称")
}

func linuxDefaultIPv6RouteInterface() (string, error) {
	file, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return "", err
	}
	defer file.Close()
	zeroDest := strings.Repeat("0", 32)
	best := ""
	bestMetric := uint64(math.MaxUint64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[0] != zeroDest || strings.TrimLeft(fields[1], "0") != "" {
			continue
		}
		flags, err := strconv.ParseUint(fields[8], 16, 64)
		if err != nil || flags&0x1 == 0 || flags&0x200 != 0 {
			continue
		}
		metric, err := strconv.ParseUint(fields[5], 16, 64)
		if err != nil {
			metric = math.MaxUint64 - 1
		}
		iface := fields[len(fields)-1]
		if iface == "lo" {
			continue
		}
		if best == "" || metric < bestMetric {
			best, bestMetric = iface, metric
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if best == "" {
		return "", common.NewError("no IPv6 default route")
	}
	return best, nil
}

func stopProxyDataPlaneForTrafficBudget() error {
	var errs []error
	if mieru := GetMieruService(); mieru != nil {
		if err := mieru.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	if masque := GetMasqueService(); masque != nil {
		if err := masque.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	startCoreMu.Lock()
	if corePtr != nil && corePtr.IsRunning() {
		if err := corePtr.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	startCoreMu.Unlock()
	return errors.Join(errs...)
}

func restoreProxyDataPlaneAfterTrafficBudget() error {
	var errs []error
	if corePtr != nil {
		if err := (&ConfigService{}).StartCore(); err != nil {
			errs = append(errs, err)
		}
	}
	if masque := GetMasqueService(); masque != nil {
		if err := masque.SyncFromDB(); err != nil {
			errs = append(errs, err)
		}
	}
	if mieru := GetMieruService(); mieru != nil {
		if err := mieru.SyncFromDB(); err != nil {
			errs = append(errs, err)
		} else {
			mieru.StartWatchdog()
		}
	}
	return errors.Join(errs...)
}
