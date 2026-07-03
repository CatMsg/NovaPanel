package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

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
