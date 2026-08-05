package core

import (
	"testing"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
)

func TestValidateConfigRejectsInvalidOutboundOptions(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	core := NewCore()
	err := core.ValidateConfig([]byte(`{"outbounds":[{"type":"direct","tag":"direct","bind_interface":123}]}`))
	if err == nil {
		t.Fatal("expected invalid outbound option type to be rejected")
	}
}
