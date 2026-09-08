package core

import (
	"context"

	"github.com/CatMsg/NovaPanel/logger"

	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	_ "github.com/sagernet/sing-box/experimental/clashapi"
	_ "github.com/sagernet/sing-box/experimental/v2rayapi"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	_ "github.com/sagernet/sing-box/transport/v2rayquic"
	"github.com/sagernet/sing/service"
)

var (
	globalCtx        context.Context
	inbound_manager  adapter.InboundManager
	outbound_manager adapter.OutboundManager
	service_manager  adapter.ServiceManager
	endpoint_manager adapter.EndpointManager
	router           adapter.Router
	factory          log.Factory
)

type Core struct {
	isRunning bool
	instance  *Box
}

func NewCore() *Core {
	globalCtx = context.Background()
	globalCtx = sb.Context(globalCtx, InboundRegistry(), OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry())
	return &Core{
		isRunning: false,
		instance:  nil,
	}
}

func (c *Core) GetCtx() context.Context {
	return globalCtx
}

func (c *Core) GetInstance() *Box {
	return c.instance
}

func (c *Core) Start(sbConfig []byte) error {
	var opt option.Options
	err := opt.UnmarshalJSONContext(globalCtx, sbConfig)
	if err != nil {
		logger.Error("Unmarshal config err:", err.Error())
		return err
	}

	c.instance, err = NewBox(Options{
		Context: globalCtx,
		Options: opt,
	})
	if err != nil {
		return err
	}

	err = c.instance.Start()
	if err != nil {
		_ = c.instance.Close()
		c.instance = nil
		return err
	}

	globalCtx = service.ContextWith(globalCtx, c)
	inbound_manager = service.FromContext[adapter.InboundManager](globalCtx)
	outbound_manager = service.FromContext[adapter.OutboundManager](globalCtx)
	service_manager = service.FromContext[adapter.ServiceManager](globalCtx)
	endpoint_manager = service.FromContext[adapter.EndpointManager](globalCtx)
	router = service.FromContext[adapter.Router](globalCtx)

	c.isRunning = true
	return nil
}

// ValidateConfig parses a complete sing-box configuration without changing the
// running instance. Runtime references are validated separately by the service
// layer because some of them are only resolved when sing-box starts.
func (c *Core) ValidateConfig(sbConfig []byte) error {
	ctx := globalCtx
	if ctx == nil {
		ctx = sb.Context(context.Background(), InboundRegistry(), OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry())
	}
	var opt option.Options
	return opt.UnmarshalJSONContext(ctx, sbConfig)
}

func (c *Core) Stop() error {
	c.isRunning = false
	if c.instance == nil {
		return nil
	}
	err := c.instance.Close()
	c.instance = nil
	return err
}

func (c *Core) IsRunning() bool {
	return c.isRunning
}
