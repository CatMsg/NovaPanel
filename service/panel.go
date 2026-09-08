package service

import (
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
)

type PanelService struct {
}

func (s *PanelService) RestartPanel(delay time.Duration) error {
	p, err := os.FindProcess(syscall.Getpid())
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(delay)
		var signalErr error
		if runtime.GOOS == "windows" {
			signalErr = p.Kill()
		} else {
			signalErr = p.Signal(syscall.SIGHUP)
		}
		if signalErr != nil {
			logger.Error("send signal SIGHUP failed:", signalErr)
		}
	}()
	return nil
}
