package logger

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/op/go-logging"
)

var (
	// CLI commands can use service code before the application configures its
	// final backend. Keep the package logger valid during that bootstrap phase.
	logger    atomic.Pointer[logging.Logger]
	logBuffer []struct {
		time  string
		level logging.Level
		log   string
	}
	logBufferMu sync.RWMutex
)

func init() {
	logger.Store(logging.MustGetLogger("novas"))
}

func InitLogger(level logging.Level) {
	newLogger := logging.MustGetLogger("novas")
	var err error
	var backend logging.Backend
	var format logging.Formatter

	_, inContainer := os.LookupEnv("container")
	if !inContainer {
		if _, statErr := os.Stat("/.dockerenv"); statErr == nil {
			inContainer = true
		}
	}
	if inContainer {
		backend = logging.NewLogBackend(os.Stderr, "", 0)
		format = logging.MustStringFormatter(`%{time:2006/01/02 15:04:05} %{level} - %{message}`)
	} else {
		backend, err = logging.NewSyslogBackend("")
		if err != nil {
			fmt.Println("Unable to use syslog: " + err.Error())
			backend = logging.NewLogBackend(os.Stderr, "", 0)
		}
		if err != nil {
			format = logging.MustStringFormatter(`%{time:2006/01/02 15:04:05} %{level} - %{message}`)
		} else {
			format = logging.MustStringFormatter(`%{level} - %{message}`)
		}
	}

	backendFormatter := logging.NewBackendFormatter(backend, format)
	backendLeveled := logging.AddModuleLevel(backendFormatter)
	backendLeveled.SetLevel(level, "novas")
	newLogger.SetBackend(backendLeveled)

	logger.Store(newLogger)
}

func GetLogger() *logging.Logger {
	return logger.Load()
}

func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
	addToBuffer("DEBUG", fmt.Sprint(args...))
}

func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
	addToBuffer("DEBUG", fmt.Sprintf(format, args...))
}

func Info(args ...interface{}) {
	GetLogger().Info(args...)
	addToBuffer("INFO", fmt.Sprint(args...))
}

func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
	addToBuffer("INFO", fmt.Sprintf(format, args...))
}

func Warning(args ...interface{}) {
	GetLogger().Warning(args...)
	addToBuffer("WARNING", fmt.Sprint(args...))
}

func Warningf(format string, args ...interface{}) {
	GetLogger().Warningf(format, args...)
	addToBuffer("WARNING", fmt.Sprintf(format, args...))
}

// AuditWarningf writes security events directly to stderr so systemd-journald
// reliably associates them with novas.service for Fail2ban matching.
func AuditWarningf(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, "WARNING - "+message)
	addToBuffer("WARNING", message)
}

func Error(args ...interface{}) {
	GetLogger().Error(args...)
	addToBuffer("ERROR", fmt.Sprint(args...))
}

func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
	addToBuffer("ERROR", fmt.Sprintf(format, args...))
}

func addToBuffer(level string, newLog string) {
	logBufferMu.Lock()
	defer logBufferMu.Unlock()

	t := time.Now()
	if len(logBuffer) >= 10240 {
		logBuffer = logBuffer[1:]
	}

	logLevel, _ := logging.LogLevel(level)
	logBuffer = append(logBuffer, struct {
		time  string
		level logging.Level
		log   string
	}{
		time:  t.Format("2006/01/02 15:04:05"),
		level: logLevel,
		log:   newLog,
	})
}

func GetLogs(c int, level string) []string {
	if c < 1 {
		c = 10
	} else if c > 1000 {
		c = 1000
	}
	logBufferMu.RLock()
	defer logBufferMu.RUnlock()

	var output []string
	logLevel, _ := logging.LogLevel(level)

	for i := len(logBuffer) - 1; i >= 0 && len(output) < c; i-- {
		if logBuffer[i].level <= logLevel {
			output = append(output, fmt.Sprintf("%s %s - %s", logBuffer[i].time, logBuffer[i].level, logBuffer[i].log))
		}
	}
	return output
}
