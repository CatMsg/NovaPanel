package service

import (
	"os"
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
		if signalErr := p.Signal(syscall.SIGHUP); signalErr != nil {
			logger.Error("send signal SIGHUP failed:", signalErr)
		}
	}()
	return nil
}
