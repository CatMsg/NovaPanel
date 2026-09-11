package sub

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
	if err := renderUserCenter(&output, newUserCenterView(client, true, 12, now)); err != nil {
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
	if err := renderUserCenter(&output, newUserCenterView(client, false, 12, time.Unix(0, 0).UTC())); err != nil {
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
