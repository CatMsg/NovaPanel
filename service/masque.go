package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

var masquePtr *MasqueService

// MasqueService reconciles native core endpoints; it owns no socket, TUN or
// firewall lifecycle. Native core trackers are the sole traffic source.
type MasqueService struct {
	mu      sync.Mutex
	configs map[string]json.RawMessage
}

func NewMasqueService() *MasqueService {
	return &MasqueService{configs: make(map[string]json.RawMessage)}
}
func SetMasqueService(s *MasqueService) { masquePtr = s }
func GetMasqueService() *MasqueService  { return masquePtr }

func (s *MasqueService) loadMasqueInbounds() ([]*model.Inbound, error) {
	var inbounds []*model.Inbound
	err := database.GetDB().Preload("Tls").Where("type = ?", "masque").Find(&inbounds).Error
	return inbounds, err
}

func (s *MasqueService) SyncFromDB() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if corePtr == nil || !corePtr.IsRunning() {
		return nil
	}
	if IsTrafficBudgetBlocked() {
		return s.stopLocked()
	}
	inbounds, err := s.loadMasqueInbounds()
	if err != nil {
		return err
	}
	desired := make(map[string]json.RawMessage)
	for _, inbound := range inbounds {
		config, err := buildMasqueEndpointConfig(database.GetDB(), inbound)
		if err != nil {
			// An invalid new auth/TLS configuration must not keep serving the
			// previous credentials after the database state has changed.
			if _, exists := corePtr.GetInstance().Endpoint().Get(inbound.Tag); exists {
				err = errors.Join(err, corePtr.RemoveEndpoint(inbound.Tag))
			}
			delete(s.configs, inbound.Tag)
			return err
		}
		if len(config) > 0 {
			desired[inbound.Tag] = config
		}
	}
	// Include cold-start endpoints too: a newly constructed reconciler must be
	// able to revoke the last eligible user without first seeing an active one.
	for _, endpoint := range corePtr.GetInstance().Endpoint().Endpoints() {
		if endpoint.Type() == "masque-server" {
			if _, known := s.configs[endpoint.Tag()]; !known {
				s.configs[endpoint.Tag()] = nil
			}
		}
	}
	for tag := range s.configs {
		if _, exists := desired[tag]; !exists {
			if err := corePtr.RemoveEndpoint(tag); err != nil && !errors.Is(err, os.ErrInvalid) {
				return err
			}
			delete(s.configs, tag)
		}
	}
	for tag, config := range desired {
		_, exists := corePtr.GetInstance().Endpoint().Get(tag)
		previous := s.configs[tag]
		if exists && (bytes.Equal(previous, config) || corePtr.MASQUEEndpointMatches(tag, config)) {
			s.configs[tag] = config
			continue
		}
		if exists {
			if err := corePtr.RemoveEndpoint(tag); err != nil {
				return err
			}
		}
		if err := corePtr.AddEndpoint(config); err != nil {
			return err
		}
		s.configs[tag] = config
	}
	return nil
}

func (s *MasqueService) Stop() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

func (s *MasqueService) stopLocked() error {
	if corePtr != nil && corePtr.IsRunning() {
		for _, endpoint := range corePtr.GetInstance().Endpoint().Endpoints() {
			if endpoint.Type() == "masque-server" {
				s.configs[endpoint.Tag()] = nil
			}
		}
	}
	for tag := range s.configs {
		if corePtr != nil && corePtr.IsRunning() {
			if err := corePtr.RemoveEndpoint(tag); err != nil && !errors.Is(err, os.ErrInvalid) {
				return err
			}
		}
		delete(s.configs, tag)
	}
	return nil
}

func (*MasqueService) CollectStats() ([]model.Stats, onlines, error) { return nil, onlines{}, nil }
