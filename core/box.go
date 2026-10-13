package core

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/util/common"

	"github.com/sagernet/sing-box/adapter"
	boxCertificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/common/certificate"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/httpclient"
	"github.com/sagernet/sing-box/common/netns"
	"github.com/sagernet/sing-box/common/taskmonitor"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/experimental"
	"github.com/sagernet/sing-box/experimental/cachefile"
	"github.com/sagernet/sing-box/experimental/clashmode"
	"github.com/sagernet/sing-box/experimental/deprecated"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/route"
	sbCommon "github.com/sagernet/sing/common"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/ntp"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

var _ adapter.SimpleLifecycle = (*Box)(nil)

type Box struct {
	ctx              context.Context
	createdAt        time.Time
	logFactory       log.Factory
	logger           log.ContextLogger
	network          *route.NetworkManager
	endpoint         *endpointManager
	inbound          *inboundManager
	outbound         *outboundManager
	service          *serviceManager
	certificate      *boxCertificate.Manager
	dnsTransport     *dns.TransportManager
	dnsRouter        *dns.Router
	connection       *route.ConnectionManager
	router           *route.Router
	httpClient       adapter.LifecycleService
	internalService  []adapter.LifecycleService
	statsTracker     *StatsTracker
	connTracker      *ConnTracker
	rateLimitTracker *UserRateLimitTracker
	ntpService       *ntp.Service
	scope            *adapter.Scope
	closeOnce        sync.Once
	closeErr         error
}

type Options struct {
	option.Options
	Context                    context.Context
	NetworkNamespaceHolderArgs []string
}

func Context(
	ctx context.Context,
	inboundRegistry adapter.InboundRegistry,
	outboundRegistry adapter.OutboundRegistry,
	endpointRegistry adapter.EndpointRegistry,
	dnsTransportRegistry adapter.DNSTransportRegistry,
	serviceRegistry adapter.ServiceRegistry,
	certificateProviderRegistry adapter.CertificateProviderRegistry,
) context.Context {
	if service.FromContext[option.InboundOptionsRegistry](ctx) == nil ||
		service.FromContext[adapter.InboundRegistry](ctx) == nil {
		ctx = service.ContextWith[option.InboundOptionsRegistry](ctx, inboundRegistry)
		ctx = service.ContextWith[adapter.InboundRegistry](ctx, inboundRegistry)
	}
	if service.FromContext[option.OutboundOptionsRegistry](ctx) == nil ||
		service.FromContext[adapter.OutboundRegistry](ctx) == nil {
		ctx = service.ContextWith[option.OutboundOptionsRegistry](ctx, outboundRegistry)
		ctx = service.ContextWith[adapter.OutboundRegistry](ctx, outboundRegistry)
	}
	if service.FromContext[option.EndpointOptionsRegistry](ctx) == nil ||
		service.FromContext[adapter.EndpointRegistry](ctx) == nil {
		ctx = service.ContextWith[option.EndpointOptionsRegistry](ctx, endpointRegistry)
		ctx = service.ContextWith[adapter.EndpointRegistry](ctx, endpointRegistry)
	}
	if service.FromContext[adapter.DNSTransportRegistry](ctx) == nil {
		ctx = service.ContextWith[option.DNSTransportOptionsRegistry](ctx, dnsTransportRegistry)
		ctx = service.ContextWith[adapter.DNSTransportRegistry](ctx, dnsTransportRegistry)
	}
	if service.FromContext[adapter.ServiceRegistry](ctx) == nil {
		ctx = service.ContextWith[option.ServiceOptionsRegistry](ctx, serviceRegistry)
		ctx = service.ContextWith[adapter.ServiceRegistry](ctx, serviceRegistry)
	}
	if service.FromContext[adapter.CertificateProviderRegistry](ctx) == nil {
		ctx = service.ContextWith[option.CertificateProviderOptionsRegistry](ctx, certificateProviderRegistry)
		ctx = service.ContextWith[adapter.CertificateProviderRegistry](ctx, certificateProviderRegistry)
	}
	return ctx
}

func NewBox(options Options) (result *Box, err error) {
	createdAt := time.Now()
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = service.ContextWithDefaultRegistry(ctx)

	endpointRegistry := service.FromContext[adapter.EndpointRegistry](ctx)
	inboundRegistry := service.FromContext[adapter.InboundRegistry](ctx)
	outboundRegistry := service.FromContext[adapter.OutboundRegistry](ctx)
	dnsTransportRegistry := service.FromContext[adapter.DNSTransportRegistry](ctx)
	serviceRegistry := service.FromContext[adapter.ServiceRegistry](ctx)
	certificateProviderRegistry := service.FromContext[adapter.CertificateProviderRegistry](ctx)

	if endpointRegistry == nil {
		return nil, common.NewError("missing endpoint registry in context")
	}
	if inboundRegistry == nil {
		return nil, common.NewError("missing inbound registry in context")
	}
	if outboundRegistry == nil {
		return nil, common.NewError("missing outbound registry in context")
	}
	if dnsTransportRegistry == nil {
		return nil, common.NewError("missing DNS transport registry in context")
	}
	if serviceRegistry == nil {
		return nil, common.NewError("missing service registry in context")
	}
	if certificateProviderRegistry == nil {
		return nil, common.NewError("missing certificate provider registry in context")
	}

	ctx = pause.WithDefaultManager(ctx)
	ctx, cancelConstruction := context.WithCancel(ctx)
	defer func() {
		if result == nil {
			cancelConstruction()
		}
	}()
	experimentalOptions := sbCommon.PtrValueOrDefault(options.Experimental)
	var needCacheFile bool
	var needClashAPI bool
	var needV2RayAPI bool
	if experimentalOptions.CacheFile != nil && experimentalOptions.CacheFile.Enabled {
		needCacheFile = true
	}
	if experimentalOptions.ClashAPI != nil {
		needClashAPI = true
	}
	if experimentalOptions.V2RayAPI != nil && experimentalOptions.V2RayAPI.Listen != "" {
		needV2RayAPI = true
	}
	needAPIService := sbCommon.Any(options.Services, func(it option.Service) bool {
		return it.Type == C.TypeAPI
	})
	if service.PtrFromContext[urltest.HistoryStorage](ctx) == nil {
		ctx = service.ContextWithPtr(ctx, urltest.NewHistoryStorage())
	}
	platformInterface := service.FromContext[adapter.PlatformInterface](ctx)
	defaultLogWriter := io.Writer(io.Discard)
	var logFactory log.Factory
	logFactory, err = NewFactory(log.Options{
		Context:       ctx,
		Options:       sbCommon.PtrValueOrDefault(options.Log),
		Observable:    needClashAPI && experimentalOptions.ClashAPI.ExternalController != "",
		DefaultWriter: defaultLogWriter,
		BaseTime:      createdAt,
	})
	if err != nil {
		return nil, common.NewError("create log factory", err)
	}
	service.MustRegister[log.Factory](ctx, logFactory)
	factory = logFactory
	scope := adapter.NewScope(ctx, logFactory.Logger())
	scope.Add(logFactory.Close)
	scope.Add(func() error { cancelConstruction(); return nil })
	ctx = scope.Context()
	defer func() {
		if result == nil {
			err = errors.Join(err, scope.Close())
		}
	}()

	var internalServices []adapter.LifecycleService
	certificateOptions := sbCommon.PtrValueOrDefault(options.Certificate)
	if C.IsAndroid || certificateOptions.Store != "" && certificateOptions.Store != C.CertificateStoreSystem ||
		len(certificateOptions.Certificate) > 0 ||
		len(certificateOptions.CertificatePath) > 0 ||
		len(certificateOptions.CertificateDirectoryPath) > 0 {
		certificateStore, err := certificate.NewStore(ctx, logFactory.NewLogger("certificate"), certificateOptions)
		if err != nil {
			return nil, err
		}
		service.MustRegister[adapter.CertificateStore](ctx, certificateStore)
		internalServices = append(internalServices, certificateStore)
	}
	netnsManager, err := netns.NewManager(logFactory.NewLogger("netns"), options.NetworkNamespaces, options.NetworkNamespaceHolderArgs)
	if err != nil {
		return nil, err
	}
	service.MustRegister[adapter.NetworkNamespaceManager](ctx, netnsManager)
	internalServices = append(internalServices, netnsManager)

	routeOptions := sbCommon.PtrValueOrDefault(options.Route)
	dnsOptions := sbCommon.PtrValueOrDefault(options.DNS)
	endpointManager := &endpointManager{componentSet: newComponentSet[adapter.Endpoint]("endpoint", logFactory.NewLogger("endpoint")), registry: endpointRegistry}
	inboundManager := &inboundManager{componentSet: newComponentSet[adapter.Inbound]("inbound", logFactory.NewLogger("inbound")), registry: inboundRegistry, endpoint: endpointManager}
	outboundManager := &outboundManager{componentSet: newComponentSet[adapter.Outbound]("outbound", logFactory.NewLogger("outbound")), registry: outboundRegistry, endpoint: endpointManager, defaultTag: routeOptions.Final}
	endpointManager.outbound = outboundManager
	dnsTransportManager := dns.NewTransportManager(dnsTransportRegistry, outboundManager, dnsOptions.Final)
	serviceManager := &serviceManager{componentSet: newComponentSet[adapter.Service]("service", logFactory.NewLogger("service")), registry: serviceRegistry}
	// These also cover constructor errors before any lifecycle stage is entered.
	// Once started, the manager's child scope performs cleanup first; this is
	// then a no-op, preserving the official reverse startup cleanup ordering.
	scope.Add(endpointManager.close)
	scope.Add(inboundManager.close)
	scope.Add(outboundManager.close)
	scope.Add(serviceManager.close)
	certificateProviderManager := boxCertificate.NewManager(certificateProviderRegistry)

	service.MustRegister[adapter.EndpointManager](ctx, endpointManager)
	service.MustRegister[adapter.InboundManager](ctx, inboundManager)
	service.MustRegister[adapter.OutboundManager](ctx, outboundManager)
	service.MustRegister[adapter.DNSTransportManager](ctx, dnsTransportManager)
	service.MustRegister[adapter.ServiceManager](ctx, serviceManager)
	service.MustRegister[adapter.CertificateProviderManager](ctx, certificateProviderManager)

	dnsRouter, err := dns.NewRouter(ctx, logFactory, dnsOptions)
	if err != nil {
		return nil, common.NewError("initialize DNS router", err)
	}
	service.MustRegister[adapter.DNSRouter](ctx, dnsRouter)
	service.MustRegister[adapter.DNSRuleSetUpdateValidator](ctx, dnsRouter)

	connectionManager := route.NewConnectionManager(logFactory.NewLogger("connection"))
	service.MustRegister[adapter.ConnectionManager](ctx, connectionManager)
	networkManager, err := route.NewNetworkManager(ctx, logFactory.NewLogger("network"), routeOptions, dnsOptions)
	if err != nil {
		return nil, common.NewError("initialize network manager", err)
	}
	service.MustRegister[adapter.NetworkManager](ctx, networkManager)
	httpClientManager := httpclient.NewManager(ctx, logFactory.NewLogger("httpclient"), options.HTTPClients, routeOptions.DefaultHTTPClient)
	service.MustRegister[adapter.HTTPClientManager](ctx, httpClientManager)
	httpClientService := adapter.LifecycleService(httpClientManager)
	router := route.NewRouter(ctx, logFactory, routeOptions, dnsOptions)
	service.MustRegister[adapter.Router](ctx, router)
	err = router.Initialize(routeOptions.Rules, routeOptions.RuleSet)
	if err != nil {
		return nil, common.NewError("initialize router", err)
	}
	if needClashAPI || needAPIService {
		trafficManager := trafficcontrol.NewManager()
		service.MustRegisterPtr(ctx, trafficManager)
		router.AppendTracker(trafficManager)
		internalServices = append(internalServices, trafficManager)
		var clashDefaultMode string
		if experimentalOptions.ClashAPI != nil {
			clashDefaultMode = experimentalOptions.ClashAPI.DefaultMode
		}
		clashMode := clashmode.NewManager(ctx, logFactory.NewLogger("clash-mode"), clashDefaultMode, clashmode.CalculateModeList(options.Options))
		service.MustRegisterPtr(ctx, clashMode)
		internalServices = append(internalServices, clashMode)
	}
	internalServices = append(internalServices, route.NewReferenceManager(ctx, logFactory.NewLogger("reference"), options.Options))
	ntpOptions := sbCommon.PtrValueOrDefault(options.NTP)
	var timeService *tls.TimeServiceWrapper
	if ntpOptions.Enabled {
		timeService = new(tls.TimeServiceWrapper)
		service.MustRegister[ntp.TimeService](ctx, timeService)
	}
	for i, transportOptions := range dnsOptions.Servers {
		var tag string
		if transportOptions.Tag != "" {
			tag = transportOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		err = dnsTransportManager.Create(
			ctx,
			logFactory.NewLogger(F.ToString("dns/", transportOptions.Type, "[", tag, "]")),
			tag,
			transportOptions.Type,
			transportOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize DNS server[", i, "]", err)
		}
	}
	err = dnsRouter.Initialize(dnsOptions.Rules)
	if err != nil {
		return nil, common.NewError("initialize dns router", err)
	}
	for i, endpointOptions := range options.Endpoints {
		var tag string
		if endpointOptions.Tag != "" {
			tag = endpointOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		endpointCtx := ctx
		if tag != "" {
			endpointCtx = adapter.WithContext(endpointCtx, &adapter.InboundContext{Outbound: tag})
		}
		err = endpointManager.Create(
			endpointCtx,
			router,
			logFactory.NewLogger(F.ToString("endpoint/", endpointOptions.Type, "[", tag, "]")),
			tag,
			endpointOptions.Type,
			endpointOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize endpoint["+F.ToString(i)+"] "+tag, err)
		}
	}
	for i, inboundOptions := range options.Inbounds {
		var tag string
		if inboundOptions.Tag != "" {
			tag = inboundOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		err = inboundManager.Create(
			ctx,
			router,
			logFactory.NewLogger(F.ToString("inbound/", inboundOptions.Type, "[", tag, "]")),
			tag,
			inboundOptions.Type,
			inboundOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize inbound[", i, "] ", tag, err)
		}
	}
	for i, outboundOptions := range options.Outbounds {
		var tag string
		if outboundOptions.Tag != "" {
			tag = outboundOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		outboundCtx := ctx
		if tag != "" {
			// TODO: remove this
			outboundCtx = adapter.WithContext(outboundCtx, &adapter.InboundContext{
				Outbound: tag,
			})
		}
		err = outboundManager.Create(
			outboundCtx,
			router,
			logFactory.NewLogger(F.ToString("outbound/", outboundOptions.Type, "[", tag, "]")),
			tag,
			outboundOptions.Type,
			outboundOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize outbound["+F.ToString(i)+"] "+tag, err)
		}
	}
	for i, serviceOptions := range options.Services {
		var tag string
		if serviceOptions.Tag != "" {
			tag = serviceOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		err = serviceManager.Create(
			ctx,
			logFactory.NewLogger(F.ToString("service/", serviceOptions.Type, "[", tag, "]")),
			tag,
			serviceOptions.Type,
			serviceOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize service["+F.ToString(i)+"]"+tag, err)
		}
	}
	for i, providerOptions := range options.CertificateProviders {
		var tag string
		if providerOptions.Tag != "" {
			tag = providerOptions.Tag
		} else {
			tag = F.ToString(i)
		}
		err = certificateProviderManager.Create(
			ctx,
			logFactory.NewLogger(F.ToString("certificate-provider/", providerOptions.Type, "[", tag, "]")),
			tag,
			providerOptions.Type,
			providerOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize certificate provider[", i, "]", err)
		}
	}
	outboundManager.Initialize(func(ctx context.Context) (adapter.Outbound, error) {
		return direct.NewOutbound(
			ctx,
			router,
			logFactory.NewLogger("outbound/direct"),
			"",
			option.DirectOutboundOptions{},
		)
	})
	dnsTransportManager.Initialize(func() (adapter.DNSTransport, error) {
		return dnsTransportRegistry.CreateDNSTransport(
			ctx,
			logFactory.NewLogger("dns/local"),
			"local",
			C.DNSTypeLocal,
			&option.LocalDNSServerOptions{},
		)
	})
	httpClientManager.Initialize(func() (*httpclient.ManagedTransport, error) {
		deprecated.Report(ctx, deprecated.OptionImplicitDefaultHTTPClient)
		var httpClientOptions option.HTTPClientOptions
		httpClientOptions.DefaultOutbound = true
		return httpclient.NewTransport(ctx, logFactory.NewLogger("httpclient"), "", httpClientOptions)
	})
	if platformInterface != nil {
		err = platformInterface.Initialize(networkManager)
		if err != nil {
			return nil, common.NewError("initialize platform interface", err)
		}
	}
	statsTracker := NewStatsTracker()
	connTracker := NewConnTracker()
	historyTracker := NewHistoryTracker()
	rateLimitTracker := NewUserRateLimitTracker()
	if err := rateLimitTracker.Reload(); err != nil {
		return nil, common.NewError(err, "load user rate limits")
	}
	router.AppendTracker(statsTracker)
	router.AppendTracker(connTracker)
	router.AppendTracker(historyTracker)
	router.AppendTracker(rateLimitTracker)

	if needCacheFile {
		cacheFile := cachefile.New(ctx, logFactory.NewLogger("cache-file"), sbCommon.PtrValueOrDefault(experimentalOptions.CacheFile))
		service.MustRegister[adapter.CacheFile](ctx, cacheFile)
		internalServices = append(internalServices, cacheFile)
	}
	if needClashAPI {
		clashServer, err := experimental.NewClashServer(ctx, logFactory.(log.ObservableFactory), sbCommon.PtrValueOrDefault(experimentalOptions.ClashAPI))
		if err != nil {
			return nil, common.NewError(err, "create clash-server")
		}
		internalServices = append(internalServices, clashServer)
	}
	if needV2RayAPI {
		v2rayServer, err := experimental.NewV2RayServer(logFactory.NewLogger("v2ray-api"), sbCommon.PtrValueOrDefault(experimentalOptions.V2RayAPI))
		if err != nil {
			return nil, common.NewError(err, "create v2ray-server")
		}
		if v2rayServer.StatsService() != nil {
			router.AppendTracker(v2rayServer.StatsService())
			internalServices = append(internalServices, v2rayServer)
			service.MustRegister[adapter.V2RayServer](ctx, v2rayServer)
		}
	}
	var ntpService *ntp.Service
	if ntpOptions.Enabled {
		if ntpOptions.WriteToSystem {
			if err = adapter.CheckSecurityFeature(ctx, "NTP `write_to_system`"); err != nil {
				return nil, err
			}
		}
		ntpDialer, err := dialer.New(ctx, ntpOptions.DialerOptions, ntpOptions.ServerIsDomain())
		if err != nil {
			return nil, common.NewError(err, "create NTP service")
		}
		ntpService = ntp.NewService(ntp.Options{
			Context:       ctx,
			Dialer:        ntpDialer,
			Logger:        logFactory.NewLogger("ntp"),
			Server:        ntpOptions.ServerOptions.Build(),
			Interval:      time.Duration(ntpOptions.Interval),
			WriteToSystem: ntpOptions.WriteToSystem,
		})
		timeService.TimeService = ntpService
	}
	return &Box{
		ctx:              ctx,
		network:          networkManager,
		endpoint:         endpointManager,
		inbound:          inboundManager,
		outbound:         outboundManager,
		dnsTransport:     dnsTransportManager,
		service:          serviceManager,
		certificate:      certificateProviderManager,
		dnsRouter:        dnsRouter,
		connection:       connectionManager,
		router:           router,
		httpClient:       httpClientService,
		createdAt:        createdAt,
		logFactory:       logFactory,
		logger:           logFactory.Logger(),
		internalService:  internalServices,
		statsTracker:     statsTracker,
		connTracker:      connTracker,
		rateLimitTracker: rateLimitTracker,
		ntpService:       ntpService,
		scope:            scope,
	}, nil
}

func (s *Box) PreStart() error {
	err := s.preStart()
	if err != nil {
		return errors.Join(err, s.Close())
	}
	s.logger.Info("sing-box pre-started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

func (s *Box) Start() error {
	err := s.start()
	if err != nil {
		return errors.Join(err, s.Close())
	}
	s.logger.Info("sing-box started (", F.Seconds(time.Since(s.createdAt).Seconds()), "s)")
	return nil
}

type boxComponent struct {
	name      string
	lifecycle adapter.Lifecycle
}

func (s *Box) startComponents(stage adapter.StartStage, components ...boxComponent) error {
	for _, component := range components {
		if err := s.scope.Start(component.name, component.lifecycle, stage); err != nil {
			return err
		}
	}
	return nil
}

func (s *Box) startInternalServices(stage adapter.StartStage) error {
	for _, component := range s.internalService {
		if err := s.scope.Start(component.Name(), component, stage); err != nil {
			return err
		}
	}
	return nil
}

func (s *Box) preStart() error {
	if err := s.scope.Context().Err(); err != nil {
		return err
	}
	monitor := taskmonitor.New(s.logger, C.StartTimeout)
	monitor.Start("start logger")
	err := s.logFactory.Start()
	monitor.Finish()
	if err != nil {
		return common.NewError(err, "start logger")
	}
	err = s.startInternalServices(adapter.StartStateInitialize)
	if err != nil {
		return err
	}
	err = s.startComponents(adapter.StartStateInitialize,
		boxComponent{s.httpClient.Name(), s.httpClient},
		boxComponent{"network", s.network}, boxComponent{"dns-transport", s.dnsTransport},
		boxComponent{"dns-router", s.dnsRouter}, boxComponent{"connection", s.connection},
		boxComponent{"router", s.router}, boxComponent{"outbound", s.outbound},
		boxComponent{"endpoint", s.endpoint}, boxComponent{"certificate-provider", s.certificate},
		boxComponent{"inbound", s.inbound}, boxComponent{"service", s.service})
	if err != nil {
		return err
	}
	err = s.startComponents(adapter.StartStateStart,
		boxComponent{"outbound", s.outbound}, boxComponent{"dns-transport", s.dnsTransport},
		boxComponent{"network", s.network}, boxComponent{"connection", s.connection})
	if err != nil {
		return err
	}
	err = s.startComponents(adapter.StartStateStart, boxComponent{s.httpClient.Name(), s.httpClient})
	if err != nil {
		return err
	}
	err = s.startComponents(adapter.StartStateStart, boxComponent{"router", s.router}, boxComponent{"dns-router", s.dnsRouter})
	if err != nil {
		return err
	}
	return nil
}

func (s *Box) start() error {
	err := s.preStart()
	if err != nil {
		return err
	}
	err = s.startInternalServices(adapter.StartStateStart)
	if err != nil {
		return err
	}
	if s.ntpService != nil {
		if err := s.ntpService.Start(); err != nil {
			return err
		}
		s.scope.Add(s.ntpService.Close)
	}
	err = s.startComponents(adapter.StartStateStart,
		boxComponent{"endpoint", s.endpoint}, boxComponent{"certificate-provider", s.certificate},
		boxComponent{"inbound", s.inbound}, boxComponent{"service", s.service})
	if err != nil {
		return err
	}
	err = s.startComponents(adapter.StartStatePostStart,
		boxComponent{"outbound", s.outbound}, boxComponent{"network", s.network},
		boxComponent{"dns-transport", s.dnsTransport}, boxComponent{"dns-router", s.dnsRouter},
		boxComponent{"connection", s.connection}, boxComponent{"router", s.router},
		boxComponent{"endpoint", s.endpoint}, boxComponent{"certificate-provider", s.certificate},
		boxComponent{"inbound", s.inbound}, boxComponent{"service", s.service})
	if err != nil {
		return err
	}
	err = s.startInternalServices(adapter.StartStatePostStart)
	if err != nil {
		return err
	}
	err = s.startComponents(adapter.StartStateStarted,
		boxComponent{"network", s.network}, boxComponent{"dns-transport", s.dnsTransport},
		boxComponent{"dns-router", s.dnsRouter}, boxComponent{"connection", s.connection},
		boxComponent{"router", s.router}, boxComponent{"outbound", s.outbound},
		boxComponent{"endpoint", s.endpoint}, boxComponent{"certificate-provider", s.certificate},
		boxComponent{"inbound", s.inbound}, boxComponent{"service", s.service})
	if err != nil {
		return err
	}
	err = s.startInternalServices(adapter.StartStateStarted)
	if err != nil {
		return err
	}
	return nil
}

func (s *Box) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.scope.Close()
		if s.statsTracker != nil {
			s.statsTracker.Reset()
		}
		if s.connTracker != nil {
			s.connTracker.Reset()
		}
	})
	return s.closeErr
}

func (s *Box) Uptime() uint32 {
	return uint32(time.Since(s.createdAt).Seconds())
}

func (s *Box) Network() adapter.NetworkManager {
	return s.network
}

func (s *Box) Router() adapter.Router {
	return s.router
}

func (s *Box) Inbound() adapter.InboundManager {
	return s.inbound
}

func (s *Box) Outbound() adapter.OutboundManager {
	return s.outbound
}

func (s *Box) Endpoint() adapter.EndpointManager {
	return s.endpoint
}

func (s *Box) StatsTracker() *StatsTracker {
	return s.statsTracker
}

func (s *Box) ConnTracker() *ConnTracker {
	return s.connTracker
}

func (s *Box) RateLimitTracker() *UserRateLimitTracker {
	return s.rateLimitTracker
}
