package core

import (
	panelLog "github.com/CatMsg/NovaPanel/logger"

	"github.com/sagernet/sing-box/log"
)

// PlatformWriter keeps sing-box logs in NovaPanel's existing log pipeline while
// the upstream factory owns buffering, observability and lifecycle details.
type PlatformWriter struct{}

func (PlatformWriter) WriteMessage(level log.Level, message string) {
	switch level {
	case log.LevelInfo:
		panelLog.Info(message)
	case log.LevelWarn:
		panelLog.Warning(message)
	case log.LevelPanic, log.LevelFatal, log.LevelError:
		panelLog.Error(message)
	default:
		panelLog.Debug(message)
	}
}

func NewFactory(options log.Options) (log.Factory, error) {
	options.PlatformWriter = PlatformWriter{}
	return log.New(options)
}
