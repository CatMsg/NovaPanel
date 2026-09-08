package core

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/network"
)

const (
	maxClientHistoryEntries = 200
	historyDedupeWindow     = 20 * time.Second
)

type HistoryTracker struct {
	access sync.Mutex
	recent map[string]int64
}

func NewHistoryTracker() *HistoryTracker {
	return &HistoryTracker{
		recent: make(map[string]int64),
	}
}

func (c *HistoryTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	c.record(metadata, matchOutbound.Tag(), "tcp")
	return conn
}

func (c *HistoryTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	c.record(metadata, matchOutbound.Tag(), "udp")
	return conn
}

func (c *HistoryTracker) record(inboundCtx adapter.InboundContext, outboundTag string, networkType string) {
	user := strings.TrimSpace(inboundCtx.User)
	if user == "" {
		return
	}

	domain := strings.TrimSpace(inboundCtx.Domain)
	if domain == "" && inboundCtx.Destination.IsDomain() {
		domain = strings.TrimSpace(inboundCtx.Destination.Fqdn)
	}
	if domain == "" && inboundCtx.Destination.IsValid() {
		domain = inboundCtx.Destination.AddrString()
	}
	if domain == "" {
		return
	}

	destination := inboundCtx.Destination.String()
	now := time.Now().Unix()
	dedupeKey := strings.ToLower(user) + "|" + strings.ToLower(domain) + "|" + strings.ToLower(destination) + "|" + strings.ToLower(outboundTag) + "|" + strings.ToLower(networkType)

	c.access.Lock()
	if lastSeen, ok := c.recent[dedupeKey]; ok && time.Duration(now-lastSeen)*time.Second < historyDedupeWindow {
		c.access.Unlock()
		return
	}
	c.recent[dedupeKey] = now
	if len(c.recent) > 4096 {
		for key, seen := range c.recent {
			if time.Duration(now-seen)*time.Second > time.Hour {
				delete(c.recent, key)
			}
		}
	}
	c.access.Unlock()

	entry := model.ClientHistoryEntry{
		DateTime:    now,
		Domain:      domain,
		Destination: destination,
		Inbound:     inboundCtx.Inbound,
		Outbound:    outboundTag,
		Network:     networkType,
		Protocol:    inboundCtx.Protocol,
	}

	if err := appendClientHistory(user, entry); err != nil {
		logger.Warning("append client history failed: ", err)
	}
}

func appendClientHistory(username string, entry model.ClientHistoryEntry) error {
	db := database.GetDB()
	if db == nil {
		return nil
	}

	var client model.Client
	err := db.Model(model.Client{}).Select("id, history").Where("name = ?", username).First(&client).Error
	if err != nil {
		return err
	}

	var history []model.ClientHistoryEntry
	if len(client.History) > 0 {
		if err := json.Unmarshal(client.History, &history); err != nil {
			history = []model.ClientHistoryEntry{}
		}
	}

	history = append([]model.ClientHistoryEntry{entry}, history...)
	if len(history) > maxClientHistoryEntries {
		history = history[:maxClientHistoryEntries]
	}

	rawHistory, err := json.Marshal(history)
	if err != nil {
		return err
	}

	return db.Model(&model.Client{}).Where("id = ?", client.Id).Update("history", rawHistory).Error
}
