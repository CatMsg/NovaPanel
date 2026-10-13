package sub

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestUserCenterRendersStatusTrafficExpiryAndReset(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	client := &model.Client{
		Enable:     true,
		Name:       "Alice",
		Volume:     10 << 30,
		Up:         1 << 30,
		Down:       2 << 30,
		TotalUp:    3 << 30,
		TotalDown:  4 << 30,
		Expiry:     now.Add(49 * time.Hour).Unix(),
		AutoReset:  true,
		ResetDays:  30,
		NextReset:  now.Add(25 * time.Hour).Unix(),
		DelayStart: false,
	}

	var output bytes.Buffer
	if err := renderUserCenter(&output, newUserCenterView(client, true, 12, now, now.Location())); err != nil {
		t.Fatalf("render user center: %v", err)
	}
	html := output.String()
	for _, expected := range []string{
		`data-testid="enabled-state"><span class="dot"></span>已启用`,
		`data-testid="online-state"><span class="dot"></span>在线`,
		`data-testid="used-traffic">3.0 GB`,
		`剩余 7.0 GB`,
		`data-testid="upload-traffic">1.0 GB`,
		`data-testid="download-traffic">2.0 GB`,
		`data-testid="lifetime-upload">4.0 GB`,
		`data-testid="lifetime-download">6.0 GB`,
		`剩余 2 天 1 小时`,
		`剩余 1 天 1 小时`,
		`data-testid="update-interval">每 12 小时`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("rendered page is missing %q", expected)
		}
	}
}

func TestUserCenterEscapesDynamicValuesAndOmitsPrivateClientData(t *testing.T) {
	client := &model.Client{
		Name:    `Alice <script>alert("name")</script>`,
		Desc:    "PRIVATE DESCRIPTION",
		Group:   "ADMIN GROUP",
		Config:  []byte(`{"password":"CONFIG SECRET"}`),
		Links:   []byte(`[{"uri":"LINK SECRET"}]`),
		History: []byte(`[{"domain":"HISTORY SECRET"}]`),
	}

	var output bytes.Buffer
	now := time.Unix(0, 0).UTC()
	if err := renderUserCenter(&output, newUserCenterView(client, false, 12, now, now.Location())); err != nil {
		t.Fatalf("render user center: %v", err)
	}
	html := output.String()
	if strings.Contains(html, client.Name) || !strings.Contains(html, `Alice &lt;script&gt;alert(&#34;name&#34;)&lt;/script&gt;`) {
		t.Fatalf("client name was not safely escaped")
	}
	for _, privateValue := range []string{"PRIVATE DESCRIPTION", "ADMIN GROUP", "CONFIG SECRET", "LINK SECRET", "HISTORY SECRET"} {
		if strings.Contains(html, privateValue) {
			t.Errorf("page exposed private client data %q", privateValue)
		}
	}
	if !strings.Contains(html, `data-testid="enabled-state"><span class="dot"></span>已停用`) || !strings.Contains(html, `data-testid="online-state"><span class="dot"></span>离线`) {
		t.Errorf("disabled/offline state was not rendered")
	}
}

func TestDescribeClientResetUsesMonthlyCalendarAndPreservesDelayedExpiry(t *testing.T) {
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load reset timezone: %v", err)
	}
	tests := []struct {
		name       string
		client     model.Client
		now        time.Time
		wantTitle  string
		wantDetail string
	}{
		{
			name:       "delayed first use uses monthly reset wording",
			client:     model.Client{DelayStart: true, AutoReset: true, ResetDays: 30},
			now:        time.Date(2026, time.October, 20, 12, 0, 0, 0, location),
			wantTitle:  "等待首次使用",
			wantDetail: "首次产生流量后启用月度重置；每月 1 日 00:00（Asia/Tokyo）重置本周期流量",
		},
		{
			name: "next monthly boundary crosses month in configured zone",
			client: model.Client{
				AutoReset: true, ResetDays: 45,
				NextReset: time.Date(2026, time.November, 1, 0, 0, 0, 0, location).Unix(),
			},
			now:        time.Date(2026, time.October, 31, 23, 30, 0, 0, location),
			wantTitle:  "每月 1 日重置",
			wantDetail: "2026-11-01 00:00 JST (Asia/Tokyo) · 剩余 30 分钟",
		},
		{
			name: "next monthly boundary crosses year",
			client: model.Client{
				AutoReset: true, ResetDays: 1,
				NextReset: time.Date(2027, time.January, 1, 0, 0, 0, 0, location).Unix(),
			},
			now:        time.Date(2026, time.December, 31, 23, 30, 0, 0, location),
			wantTitle:  "每月 1 日重置",
			wantDetail: "2027-01-01 00:00 JST (Asia/Tokyo) · 剩余 30 分钟",
		},
		{
			name: "due monthly reset is pending",
			client: model.Client{
				AutoReset: true,
				NextReset: time.Date(2026, time.November, 1, 0, 0, 0, 0, location).Unix(),
			},
			now:        time.Date(2026, time.November, 1, 0, 1, 0, 0, location),
			wantTitle:  "每月 1 日重置",
			wantDetail: "重置任务等待执行",
		},
		{
			name:       "missing next reset schedule",
			client:     model.Client{AutoReset: true, ResetDays: 30},
			now:        time.Date(2026, time.October, 20, 12, 0, 0, 0, location),
			wantTitle:  "每月 1 日重置",
			wantDetail: "下一次重置时间尚未生成",
		},
		{
			name:       "delayed non-auto reset remains day-based expiry",
			client:     model.Client{DelayStart: true, ResetDays: 45},
			now:        time.Date(2026, time.October, 20, 12, 0, 0, 0, location),
			wantTitle:  "等待首次使用",
			wantDetail: "首次产生流量后开始 45 天有效期",
		},
		{
			name:       "non-auto reset remains disabled",
			client:     model.Client{ResetDays: 45},
			now:        time.Date(2026, time.October, 20, 12, 0, 0, 0, location),
			wantTitle:  "未启用周期重置",
			wantDetail: "当前用量不会按周期自动清零",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			title, detail := describeClientReset(&test.client, test.now, location)
			if title != test.wantTitle || detail != test.wantDetail {
				t.Fatalf("describeClientReset() = (%q, %q), want (%q, %q)", title, detail, test.wantTitle, test.wantDetail)
			}
		})
	}
}

func TestUserCenterBuildUsesConfiguredResetTimezone(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "user-center.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	seedSubscriptionTestSettings(t, model.Setting{Key: "timeLocation", Value: "Asia/Tokyo"})
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load reset timezone: %v", err)
	}
	client := model.Client{
		Enable: true, Name: "monthly-reset", AutoReset: true, ResetDays: 30,
		NextReset: time.Date(2026, time.November, 1, 0, 0, 0, 0, location).Unix(),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	view, _, err := (&UserCenterService{}).Build(client.Name, time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build user center: %v", err)
	}
	if view.ResetTitle != "每月 1 日重置" || !strings.Contains(view.ResetDetail, "2026-11-01 00:00 JST (Asia/Tokyo)") {
		t.Fatalf("user center did not use configured reset timezone: title=%q detail=%q", view.ResetTitle, view.ResetDetail)
	}
}
