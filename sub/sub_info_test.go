package sub

import (
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/service"
)

func withTrafficBudgetStatus(t *testing.T, status service.TrafficBudgetStatus) {
	t.Helper()
	old := trafficBudgetStatusSnapshot
	trafficBudgetStatusSnapshot = func() service.TrafficBudgetStatus { return status }
	t.Cleanup(func() { trafficBudgetStatusSnapshot = old })
}

func TestSubscriptionHeadersKeepPersonalQuota(t *testing.T) {
	withTrafficBudgetStatus(t, service.TrafficBudgetStatus{
		Enabled: true, Supported: true, PoolRemainingBytes: 50 << 30,
	})
	client := &model.Client{Name: "alice", Up: 1 << 30, Down: 2 << 30, Volume: 100 << 30}
	headers := getClientSubscriptionHeaders(client, 12)
	want := "upload=1073741824; download=2147483648; total=107374182400; expire=0"
	if headers[0] != want {
		t.Fatalf("personal quota header = %q, want %q", headers[0], want)
	}
}
func TestSubscriptionHeadersUnlimitedWithoutBudgetStayUnlimited(t *testing.T) {
	withTrafficBudgetStatus(t, service.TrafficBudgetStatus{})
	client := &model.Client{Name: "alice", Up: 1 << 30, Down: 2 << 30}
	headers := getClientSubscriptionHeaders(client, 12)
	if !strings.Contains(headers[0], "total=0") {
		t.Fatalf("unlimited client unexpectedly received finite quota: %q", headers[0])
	}
}

func TestSubscriptionHeadersUnlimitedInheritVPSRemaining(t *testing.T) {
	withTrafficBudgetStatus(t, service.TrafficBudgetStatus{
		Enabled: true, Supported: true, PoolRemainingBytes: 50 << 30,
	})
	client := &model.Client{Name: "alice", Up: 1 << 30, Down: 2 << 30}
	headers := getClientSubscriptionHeaders(client, 12)
	wantTotal := int64(53 << 30)
	want := "upload=1073741824; download=2147483648; total=56908316672; expire=0"
	if headers[0] != want {
		t.Fatalf("inherited VPS header = %q, want %q (total=%d)", headers[0], want, wantTotal)
	}
	usage := resolveClientSubscriptionUsage(client)
	if !usage.inherited || usage.remaining != int64(50<<30) || usage.total != wantTotal {
		t.Fatalf("unexpected inherited usage: %#v", usage)
	}
	info := (&SubService{}).getClientInfo(client)
	if !strings.Contains(info, "50.00GB📊") || strings.Contains(info, "♾") {
		t.Fatalf("unexpected inherited client info: %q", info)
	}
}

func TestSubscriptionHeadersBlockedVPSShowsZeroRemaining(t *testing.T) {
	withTrafficBudgetStatus(t, service.TrafficBudgetStatus{
		Enabled: true, Supported: true, Blocked: true, PoolRemainingBytes: 0,
	})
	client := &model.Client{Name: "alice", Up: 1 << 30, Down: 2 << 30}
	usage := resolveClientSubscriptionUsage(client)
	if usage.remaining != 0 || usage.total != int64(3<<30) {
		t.Fatalf("blocked VPS usage = %#v", usage)
	}
	headers := getClientSubscriptionHeaders(client, 12)
	want := "upload=1073741824; download=2147483648; total=3221225472; expire=0"
	if headers[0] != want {
		t.Fatalf("blocked VPS header = %q, want %q", headers[0], want)
	}
	info := (&SubService{}).getClientInfo(client)
	if !strings.Contains(info, "0.00B📊") {
		t.Fatalf("blocked VPS client info should show zero remaining: %q", info)
	}
}
