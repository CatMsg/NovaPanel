package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestValidateClientRateLimits(t *testing.T) {
	if err := validateClientRateLimits(&model.Client{UploadLimit: 125_000, DownloadLimit: 250_000}); err != nil {
		t.Fatalf("valid rate limits were rejected: %v", err)
	}
	if err := validateClientRateLimits(&model.Client{UploadLimit: -1}); err == nil {
		t.Fatal("negative upload limit was accepted")
	}
	if err := validateClientRateLimits(&model.Client{DownloadLimit: -1}); err == nil {
		t.Fatal("negative download limit was accepted")
	}
}

func TestNormalizeClientResetSchedule(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load reset timezone: %v", err)
	}
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, location).Unix()

	client := model.Client{AutoReset: true, ResetDays: 30}
	if err := normalizeClientResetSchedule(&client, now, location); err != nil {
		t.Fatalf("normalize schedule: %v", err)
	}
	if want := time.Date(2026, time.October, 1, 0, 0, 0, 0, location).Unix(); client.NextReset != want {
		t.Fatalf("next reset = %d, want %d", client.NextReset, want)
	}

	client.DelayStart = true
	if err := normalizeClientResetSchedule(&client, now, location); err != nil {
		t.Fatalf("normalize delayed schedule: %v", err)
	}
	if client.NextReset != 0 {
		t.Fatalf("delayed client next reset = %d, want 0", client.NextReset)
	}

	client.DelayStart = false
	client.ResetDays = maxClientResetDays + 1
	if err := normalizeClientResetSchedule(&client, now, location); err != nil {
		t.Fatalf("legacy reset-days value should not affect monthly reset: %v", err)
	}
	if want := time.Date(2026, time.October, 1, 0, 0, 0, 0, location).Unix(); client.NextReset != want {
		t.Fatalf("legacy schedule = %d, want %d", client.NextReset, want)
	}

	client = model.Client{DelayStart: true, ResetDays: maxClientResetDays + 1}
	if err := normalizeClientResetSchedule(&client, now, location); err == nil {
		t.Fatal("excessive delayed-expiry period was accepted")
	}
}

func TestNextClientMonthlyResetFollowsCalendarAndTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load reset timezone: %v", err)
	}
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "mid month",
			now:  time.Date(2026, time.October, 14, 9, 30, 0, 0, location),
			want: time.Date(2026, time.November, 1, 0, 0, 0, 0, location),
		},
		{
			name: "exact reset boundary advances to next month",
			now:  time.Date(2026, time.November, 1, 0, 0, 0, 0, location),
			want: time.Date(2026, time.December, 1, 0, 0, 0, 0, location),
		},
		{
			name: "month end",
			now:  time.Date(2026, time.December, 31, 23, 59, 59, 0, location),
			want: time.Date(2027, time.January, 1, 0, 0, 0, 0, location),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := nextClientMonthlyReset(test.now.Unix(), location)
			if got != test.want.Unix() {
				t.Fatalf("next reset = %s, want %s", time.Unix(got, 0).In(location), test.want)
			}
		})
	}
}

func TestResetClientsInitializesMissingScheduleWithoutResettingUsage(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "client-reset.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	now := int64(1_800_000_000)
	client := model.Client{
		Enable: true, Name: "reset-test", Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`),
		Links: json.RawMessage(`[]`), AutoReset: true, ResetDays: 30,
		Up: 100, Down: 200, TotalUp: 300, TotalDown: 400,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	legacyClient := model.Client{
		Enable: true, Name: "legacy-reset-test", Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`),
		Links: json.RawMessage(`[]`), AutoReset: true, ResetDays: 30, NextReset: now + 3*86400,
		Up: 50, Down: 75, TotalUp: 100, TotalDown: 125,
	}
	if err := db.Create(&legacyClient).Error; err != nil {
		t.Fatalf("create legacy client: %v", err)
	}

	svc := &ClientService{}
	if _, changed, err := svc.ResetClients(db, now); err != nil || !changed {
		t.Fatalf("initialize missing schedule: changed=%v err=%v", changed, err)
	}
	var stored model.Client
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatalf("load initialized client: %v", err)
	}
	location, err := (&SettingService{}).getTimeLocationTx(db)
	if err != nil {
		t.Fatalf("get reset timezone: %v", err)
	}
	firstReset := nextClientMonthlyReset(now, location)
	if stored.NextReset != firstReset {
		t.Fatalf("initialized next reset = %d, want %d", stored.NextReset, firstReset)
	}
	if stored.Up != 100 || stored.Down != 200 || stored.TotalUp != 300 || stored.TotalDown != 400 {
		t.Fatalf("initializing schedule unexpectedly reset usage: up/down=%d/%d totals=%d/%d", stored.Up, stored.Down, stored.TotalUp, stored.TotalDown)
	}
	var migrated model.Client
	if err := db.First(&migrated, legacyClient.Id).Error; err != nil {
		t.Fatalf("load migrated client: %v", err)
	}
	if migrated.NextReset != firstReset || migrated.Up != 50 || migrated.Down != 75 || migrated.TotalUp != 100 || migrated.TotalDown != 125 {
		t.Fatalf("legacy schedule migration changed usage or missed month boundary: next=%d up/down=%d/%d totals=%d/%d", migrated.NextReset, migrated.Up, migrated.Down, migrated.TotalUp, migrated.TotalDown)
	}

	dueAt := stored.NextReset
	if _, changed, err := svc.ResetClients(db, dueAt); err != nil || !changed {
		t.Fatalf("reset at due time: changed=%v err=%v", changed, err)
	}
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatalf("load reset client: %v", err)
	}
	if stored.Up != 0 || stored.Down != 0 || stored.TotalUp != 400 || stored.TotalDown != 600 {
		t.Fatalf("due reset did not roll usage into lifetime totals: up/down=%d/%d totals=%d/%d", stored.Up, stored.Down, stored.TotalUp, stored.TotalDown)
	}
	if stored.NextReset != nextClientMonthlyReset(dueAt, location) {
		t.Fatalf("following reset = %d, want next calendar month", stored.NextReset)
	}
}

func TestBulkClientEditPreservesTrafficAndResetState(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "client-editbulk.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	client := model.Client{
		Enable: true, Name: "bulk-test", Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`),
		Links: json.RawMessage(`[]`), AutoReset: true, ResetDays: 30, NextReset: 1_800_000_000,
		Up: 10, Down: 20, TotalUp: 30, TotalDown: 40,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	svc := &ClientService{}
	clients, err := svc.GetAll()
	if err != nil || len(*clients) != 1 {
		t.Fatalf("load clients: count=%d err=%v", len(*clients), err)
	}
	(*clients)[0].Desc = "bulk edit applied"
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
		"up": 111, "down": 222, "total_up": 333, "total_down": 444,
		"auto_reset": true, "reset_days": 45, "next_reset": 1_900_000_000,
	}).Error; err != nil {
		t.Fatalf("update live counters and schedule: %v", err)
	}

	data, err := json.Marshal(*clients)
	if err != nil {
		t.Fatalf("marshal bulk clients: %v", err)
	}
	if _, err := svc.Save(db, "editbulk", data, ""); err != nil {
		t.Fatalf("bulk save: %v", err)
	}

	var stored model.Client
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatalf("load saved client: %v", err)
	}
	if stored.Desc != "bulk edit applied" {
		t.Fatalf("bulk edit did not apply description: %q", stored.Desc)
	}
	if stored.Up != 111 || stored.Down != 222 || stored.TotalUp != 333 || stored.TotalDown != 444 {
		t.Fatalf("bulk edit lost latest traffic counters: up/down=%d/%d totals=%d/%d", stored.Up, stored.Down, stored.TotalUp, stored.TotalDown)
	}
	if !stored.AutoReset || stored.ResetDays != 45 || stored.NextReset != 1_900_000_000 {
		t.Fatalf("bulk edit lost reset schedule: auto=%v days=%d next=%d", stored.AutoReset, stored.ResetDays, stored.NextReset)
	}
}

func TestClientEditPreservesLiveCountersAndUnchangedResetSchedule(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "client-edit-live.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	client := model.Client{
		Enable: true, Name: "edit-live-test", Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`),
		Links: json.RawMessage(`[]`), AutoReset: true, ResetDays: 30, NextReset: 1_800_000_000,
		Up: 10, Down: 20, TotalUp: 30, TotalDown: 40,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	var stale model.Client
	if err := db.First(&stale, client.Id).Error; err != nil {
		t.Fatalf("load edit snapshot: %v", err)
	}
	stale.Desc = "description updated"
	unchanged := false
	stale.ResetScheduleChanged = &unchanged
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
		"up": 111, "down": 222, "total_up": 333, "total_down": 444,
		"reset_days": 45, "next_reset": 1_900_000_000,
	}).Error; err != nil {
		t.Fatalf("update live state: %v", err)
	}

	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal client: %v", err)
	}
	if _, err := (&ClientService{}).Save(db, "edit", data, ""); err != nil {
		t.Fatalf("save client: %v", err)
	}

	var saved model.Client
	if err := db.First(&saved, client.Id).Error; err != nil {
		t.Fatalf("load saved client: %v", err)
	}
	if saved.Desc != "description updated" {
		t.Fatalf("edit did not update description: %q", saved.Desc)
	}
	if saved.Up != 111 || saved.Down != 222 || saved.TotalUp != 333 || saved.TotalDown != 444 {
		t.Fatalf("edit overwrote live counters: up/down=%d/%d totals=%d/%d", saved.Up, saved.Down, saved.TotalUp, saved.TotalDown)
	}
	resetLocation, err := (&SettingService{}).getTimeLocationTx(db)
	if err != nil {
		t.Fatalf("get reset timezone: %v", err)
	}
	if saved.ResetDays != 45 || !isClientMonthlyReset(saved.NextReset, resetLocation) {
		t.Fatalf("edit did not align reset schedule to month boundary: days=%d next=%d", saved.ResetDays, saved.NextReset)
	}
}

func TestManualUsageResetUsesLatestCountersAndAllowsScheduleChange(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "client-manual-reset.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	client := model.Client{
		Enable: true, Name: "manual-reset-test", Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`),
		Links: json.RawMessage(`[]`), AutoReset: true, ResetDays: 30, NextReset: 1_800_000_000,
		Up: 10, Down: 20, TotalUp: 30, TotalDown: 40,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	var edited model.Client
	if err := db.First(&edited, client.Id).Error; err != nil {
		t.Fatalf("load edit snapshot: %v", err)
	}
	edited.ResetDays = 35
	edited.NextReset = 1_900_100_000
	changed := true
	edited.ResetScheduleChanged = &changed
	edited.ResetUsage = true
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
		"up": 111, "down": 222, "total_up": 333, "total_down": 444,
	}).Error; err != nil {
		t.Fatalf("update live counters: %v", err)
	}

	data, err := json.Marshal(edited)
	if err != nil {
		t.Fatalf("marshal client: %v", err)
	}
	if _, err := (&ClientService{}).Save(db, "edit", data, ""); err != nil {
		t.Fatalf("save manual reset: %v", err)
	}

	var saved model.Client
	if err := db.First(&saved, client.Id).Error; err != nil {
		t.Fatalf("load saved client: %v", err)
	}
	if saved.Up != 0 || saved.Down != 0 || saved.TotalUp != 444 || saved.TotalDown != 666 {
		t.Fatalf("manual reset failed to archive current usage: up/down=%d/%d totals=%d/%d", saved.Up, saved.Down, saved.TotalUp, saved.TotalDown)
	}
	resetLocation, err := (&SettingService{}).getTimeLocationTx(db)
	if err != nil {
		t.Fatalf("get reset timezone: %v", err)
	}
	if saved.ResetDays != 35 || !isClientMonthlyReset(saved.NextReset, resetLocation) {
		t.Fatalf("requested schedule was not aligned to monthly reset: days=%d next=%d", saved.ResetDays, saved.NextReset)
	}
}

func TestUpdateLinksWithFixedInboundsUsesEachClientsOwnInbounds(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "client-links.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	inboundA := model.Inbound{
		Type:  "socks",
		Tag:   "socks-a",
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 1080
		}`),
	}
	inboundB := model.Inbound{
		Type:  "socks",
		Tag:   "socks-b",
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 2080
		}`),
	}
	if err := db.Create(&inboundA).Error; err != nil {
		t.Fatalf("create inboundA: %v", err)
	}
	if err := db.Create(&inboundB).Error; err != nil {
		t.Fatalf("create inboundB: %v", err)
	}

	clientA := &model.Client{
		Name: "client-a",
		Config: json.RawMessage(`{
			"socks": {
				"username": "user-a",
				"password": "pass-a"
			}
		}`),
		Inbounds: json.RawMessage(`[1]`),
		Links: json.RawMessage(`[
			{"remark":"remote-a","type":"remote","uri":"remote://a"}
		]`),
	}
	clientB := &model.Client{
		Name: "client-b",
		Config: json.RawMessage(`{
			"socks": {
				"username": "user-b",
				"password": "pass-b"
			}
		}`),
		Inbounds: json.RawMessage(`[2]`),
		Links: json.RawMessage(`[
			{"remark":"remote-b","type":"remote","uri":"remote://b"}
		]`),
	}

	svc := &ClientService{}
	if err := svc.updateLinksWithFixedInbounds(db, []*model.Client{clientA, clientB}, "panel.example.com"); err != nil {
		t.Fatalf("update links: %v", err)
	}

	linksA, err := decodeClientLinks(clientA.Links)
	if err != nil {
		t.Fatalf("decode linksA: %v", err)
	}
	linksB, err := decodeClientLinks(clientB.Links)
	if err != nil {
		t.Fatalf("decode linksB: %v", err)
	}

	if !hasLinkRemarkAndURI(linksA, "socks-a", "socks5://user-a:pass-a@panel.example.com:1080") {
		t.Fatalf("clientA missing local link for its own inbound: %#v", linksA)
	}
	if hasLinkRemark(linksA, "socks-b") {
		t.Fatalf("clientA should not get clientB inbound links: %#v", linksA)
	}
	if !hasLinkRemarkAndURI(linksB, "socks-b", "socks5://user-b:pass-b@panel.example.com:2080") {
		t.Fatalf("clientB missing local link for its own inbound: %#v", linksB)
	}
	if hasLinkRemark(linksB, "socks-a") {
		t.Fatalf("clientB should not get clientA inbound links: %#v", linksB)
	}
	if !hasLinkRemarkAndURI(linksA, "remote-a", "remote://a") || !hasLinkRemarkAndURI(linksB, "remote-b", "remote://b") {
		t.Fatalf("non-local links should be preserved: linksA=%#v linksB=%#v", linksA, linksB)
	}
}

func TestUpdateLinksWithFixedInboundsKeepsOnlyNonLocalWhenNoInbounds(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "client-no-inbounds.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	client := &model.Client{
		Name:     "client-no-local",
		Config:   json.RawMessage(`{"socks":{"username":"user","password":"pass"}}`),
		Inbounds: json.RawMessage(`[]`),
		Links: json.RawMessage(`[
			{"remark":"local-old","type":"local","uri":"socks5://old"},
			{"remark":"remote-keep","type":"remote","uri":"remote://keep"}
		]`),
	}

	svc := &ClientService{}
	if err := svc.updateLinksWithFixedInbounds(database.GetDB(), []*model.Client{client}, "panel.example.com"); err != nil {
		t.Fatalf("update links: %v", err)
	}

	links, err := decodeClientLinks(client.Links)
	if err != nil {
		t.Fatalf("decode links: %v", err)
	}
	if len(links) != 1 || links[0]["remark"] != "remote-keep" || links[0]["uri"] != "remote://keep" {
		t.Fatalf("expected only non-local links to remain: %#v", links)
	}
}

func TestUpdateClientsOnInboundAddRebuildsAllLocalLinksPerClient(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "client-inbound-add.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	inboundA := model.Inbound{
		Type:  "socks",
		Tag:   "socks-a",
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 1080
		}`),
	}
	inboundB := model.Inbound{
		Type:  "socks",
		Tag:   "socks-b",
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 2080
		}`),
	}
	if err := db.Create(&inboundA).Error; err != nil {
		t.Fatalf("create inboundA: %v", err)
	}
	if err := db.Create(&inboundB).Error; err != nil {
		t.Fatalf("create inboundB: %v", err)
	}
	clientInboundIDs, err := encodeClientInboundIDs([]uint{inboundA.Id})
	if err != nil {
		t.Fatalf("encode client inbound ids: %v", err)
	}

	client := model.Client{
		Name: "client-a",
		Config: json.RawMessage(`{
			"socks": {
				"username": "user-a",
				"password": "pass-a"
			}
		}`),
		Inbounds: clientInboundIDs,
		Links: json.RawMessage(`[
			{"remark":"remote-a","type":"remote","uri":"remote://a"}
		]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	svc := &ClientService{}
	if err := svc.UpdateClientsOnInboundAdd(db, "1", inboundB.Id, "panel.example.com"); err != nil {
		t.Fatalf("update clients on inbound add: %v", err)
	}

	var updated model.Client
	if err := db.First(&updated, client.Id).Error; err != nil {
		t.Fatalf("load updated client: %v", err)
	}
	links, err := decodeClientLinks(updated.Links)
	if err != nil {
		t.Fatalf("decode links: %v", err)
	}
	if !hasLinkRemark(links, "socks-a") || !hasLinkRemark(links, "socks-b") {
		t.Fatalf("expected both local links after inbound add: %#v", links)
	}
	if !hasLinkRemarkAndURI(links, "remote-a", "remote://a") {
		t.Fatalf("expected remote links preserved after inbound add: %#v", links)
	}
}

func TestUpdateClientsOnInboundDeleteRebuildsRemainingLocalLinks(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "client-inbound-delete.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	inboundA := model.Inbound{
		Type:  "socks",
		Tag:   "socks-a",
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 1080
		}`),
	}
	inboundB := model.Inbound{
		Type:  "socks",
		Tag:   "socks-b",
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 2080
		}`),
	}
	if err := db.Create(&inboundA).Error; err != nil {
		t.Fatalf("create inboundA: %v", err)
	}
	if err := db.Create(&inboundB).Error; err != nil {
		t.Fatalf("create inboundB: %v", err)
	}
	clientInboundIDs, err := encodeClientInboundIDs([]uint{inboundA.Id, inboundB.Id})
	if err != nil {
		t.Fatalf("encode client inbound ids: %v", err)
	}

	client := model.Client{
		Name: "client-a",
		Config: json.RawMessage(`{
			"socks": {
				"username": "user-a",
				"password": "pass-a"
			}
		}`),
		Inbounds: clientInboundIDs,
		Links: json.RawMessage(`[
			{"remark":"remote-a","type":"remote","uri":"remote://a"}
		]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := (&ClientService{}).updateLinksWithFixedInbounds(db, []*model.Client{&client}, "panel.example.com"); err != nil {
		t.Fatalf("prime local links: %v", err)
	}
	if err := db.Save(&client).Error; err != nil {
		t.Fatalf("persist primed client: %v", err)
	}

	svc := &ClientService{}
	if err := svc.UpdateClientsOnInboundDelete(db, inboundA.Id, inboundA.Tag); err != nil {
		t.Fatalf("update clients on inbound delete: %v", err)
	}

	var updated model.Client
	if err := db.First(&updated, client.Id).Error; err != nil {
		t.Fatalf("load updated client: %v", err)
	}
	links, err := decodeClientLinks(updated.Links)
	if err != nil {
		t.Fatalf("decode links: %v", err)
	}
	if hasLinkRemark(links, "socks-a") {
		t.Fatalf("expected deleted inbound local links removed: %#v", links)
	}
	if !hasLinkRemark(links, "socks-b") {
		t.Fatalf("expected remaining inbound local links preserved: %#v", links)
	}
	if !hasLinkRemarkAndURI(links, "remote-a", "remote://a") {
		t.Fatalf("expected remote links preserved after inbound delete: %#v", links)
	}
}

func hasLinkRemark(links []map[string]string, remark string) bool {
	for _, link := range links {
		if link["remark"] == remark {
			return true
		}
	}
	return false
}

func hasLinkRemarkAndURI(links []map[string]string, remark string, uriPrefix string) bool {
	for _, link := range links {
		if link["remark"] == remark && strings.HasPrefix(link["uri"], uriPrefix) {
			return true
		}
	}
	return false
}
