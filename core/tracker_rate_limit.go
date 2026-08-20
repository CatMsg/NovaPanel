package core

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
	"golang.org/x/time/rate"
)

type userRateLimit struct {
	upload   *rate.Limiter
	download *rate.Limiter
}

// UserRateLimitTracker applies one shared token bucket per user and direction.
// All connections and protocols carrying the same user identity share it.
type UserRateLimitTracker struct {
	mu     sync.RWMutex
	limits map[string]userRateLimit
}

func NewUserRateLimitTracker() *UserRateLimitTracker {
	return &UserRateLimitTracker{limits: make(map[string]userRateLimit)}
}

func (t *UserRateLimitTracker) Reload() error {
	var clients []model.Client
	if err := database.GetDB().
		Select("name", "upload_limit", "download_limit").
		Where("enable = ?", true).
		Find(&clients).Error; err != nil {
		return err
	}
	t.SetLimits(clients)
	return nil
}

func (t *UserRateLimitTracker) SetLimits(clients []model.Client) {
	limits := make(map[string]userRateLimit, len(clients))
	for _, client := range clients {
		name := strings.TrimSpace(client.Name)
		if name == "" || (client.UploadLimit <= 0 && client.DownloadLimit <= 0) {
			continue
		}
		limits[name] = userRateLimit{
			upload:   newByteLimiter(client.UploadLimit),
			download: newByteLimiter(client.DownloadLimit),
		}
	}
	t.mu.Lock()
	t.limits = limits
	t.mu.Unlock()
}

func newByteLimiter(bytesPerSecond int64) *rate.Limiter {
	if bytesPerSecond <= 0 {
		return nil
	}
	burst := int(bytesPerSecond / 10)
	if burst < 1500 {
		burst = 1500
	}
	if burst > 64*1024 {
		burst = 64 * 1024
	}
	return rate.NewLimiter(rate.Limit(bytesPerSecond), burst)
}

func (t *UserRateLimitTracker) limiter(user string, upload bool) *rate.Limiter {
	if t == nil || user == "" {
		return nil
	}
	t.mu.RLock()
	limit := t.limits[user]
	t.mu.RUnlock()
	if upload {
		return limit.upload
	}
	return limit.download
}

func waitBytes(ctx context.Context, limiter *rate.Limiter, size int) error {
	if limiter == nil || size <= 0 {
		return nil
	}
	burst := limiter.Burst()
	for size > 0 {
		chunk := min(size, burst)
		if err := limiter.WaitN(ctx, chunk); err != nil {
			return err
		}
		size -= chunk
	}
	return nil
}

func (t *UserRateLimitTracker) WaitUpload(ctx context.Context, user string, size int) error {
	return waitBytes(ctx, t.limiter(user, true), size)
}

func (t *UserRateLimitTracker) WaitDownload(ctx context.Context, user string, size int) error {
	return waitBytes(ctx, t.limiter(user, false), size)
}

func (t *UserRateLimitTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) net.Conn {
	if strings.TrimSpace(metadata.User) == "" {
		return conn
	}
	return &rateLimitedConn{Conn: conn, ctx: ctx, user: metadata.User, tracker: t}
}

func (t *UserRateLimitTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) network.PacketConn {
	if strings.TrimSpace(metadata.User) == "" {
		return conn
	}
	return &rateLimitedPacketConn{PacketConn: conn, ctx: ctx, user: metadata.User, tracker: t}
}

type rateLimitedConn struct {
	net.Conn
	ctx     context.Context
	user    string
	tracker *UserRateLimitTracker
}

func (c *rateLimitedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		if limitErr := c.tracker.WaitUpload(c.ctx, c.user, n); limitErr != nil {
			return n, limitErr
		}
	}
	return n, err
}

func (c *rateLimitedConn) Write(p []byte) (int, error) {
	if err := c.tracker.WaitDownload(c.ctx, c.user, len(p)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

type rateLimitedPacketConn struct {
	network.PacketConn
	ctx     context.Context
	user    string
	tracker *UserRateLimitTracker
}

func (c *rateLimitedPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	destination, err := c.PacketConn.ReadPacket(buffer)
	if err == nil && buffer != nil {
		if limitErr := c.tracker.WaitUpload(c.ctx, c.user, buffer.Len()); limitErr != nil {
			return destination, limitErr
		}
	}
	return destination, err
}

func (c *rateLimitedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	if buffer != nil {
		if err := c.tracker.WaitDownload(c.ctx, c.user, buffer.Len()); err != nil {
			return err
		}
	}
	return c.PacketConn.WritePacket(buffer, destination)
}

var _ net.Conn = (*rateLimitedConn)(nil)
var _ network.PacketConn = (*rateLimitedPacketConn)(nil)
