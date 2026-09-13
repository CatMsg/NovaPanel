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

func TestSubscriptionInfoShowsExhaustedPersonalQuota(t *testing.T) {
	withTrafficBudgetStatus(t, service.TrafficBudgetStatus{})
	client := &model.Client{Name: "alice", Up: 4 << 30, Down: 6 << 30, Volume: 10 << 30}
	info := (&SubService{}).getClientInfo(client)
	if !strings.Contains(info, "0.00B📊") || strings.Contains(info, "♾") {
		t.Fatalf("exhausted personal quota info = %q", info)
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

func TestDecorateSubscriptionOutboundTags(t *testing.T) {
	client := &model.Client{Volume: 10 << 30, Up: 1 << 30, Down: 2 << 30}
	outbounds := []map[string]interface{}{
		{"type": "hysteria2", "tag": "node-a"},
		{"type": "mieru", "tag": "node-b"},
	}
	tags := []string{"node-a", "node-b"}

	decorateSubscriptionOutboundTags(&outbounds, &tags, client, true)

	const suffix = " 7.00GB📊"
	for index, original := range []string{"node-a", "node-b"} {
		want := original + suffix
		if outbounds[index]["tag"] != want || tags[index] != want {
			t.Fatalf("decorated tag %d = %#v / %q, want %q", index, outbounds[index]["tag"], tags[index], want)
		}
	}

	result, err := (&ClashService{}).ConvertToClashMeta(&outbounds, basicClashConfig)
	if err != nil {
		t.Fatalf("convert decorated Clash subscription: %v", err)
	}
	for _, want := range []string{"node-a 7.00GB", "node-b 7.00GB"} {
		if strings.Count(result, want) < 2 {
			t.Fatalf("Clash subscription is missing decorated proxy/group references for %q", want)
		}
	}
}

func TestDecorateSubscriptionOutboundTagsDisabled(t *testing.T) {
	client := &model.Client{Volume: 10 << 30}
	outbounds := []map[string]interface{}{{"type": "hysteria2", "tag": "node-a"}}
	tags := []string{"node-a"}

	decorateSubscriptionOutboundTags(&outbounds, &tags, client, false)

	if outbounds[0]["tag"] != "node-a" || tags[0] != "node-a" {
		t.Fatalf("disabled user info changed tags: %#v / %#v", outbounds, tags)
	}
}
