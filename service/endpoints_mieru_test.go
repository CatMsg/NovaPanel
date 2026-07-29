package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestEndpointServiceHidesAndRejectsLegacyMieru(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "legacy-mieru-endpoint.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	db := database.GetDB()
	for _, endpoint := range []model.Endpoint{
		{Type: "mieru", Tag: "legacy-mieru", Options: json.RawMessage(`{"listen_port":22000}`)},
		{Type: "wireguard", Tag: "wireguard-main", Options: json.RawMessage(`{"listen_port":22001}`)},
	} {
		if err := db.Create(&endpoint).Error; err != nil {
			t.Fatalf("create endpoint %s: %v", endpoint.Tag, err)
		}
	}

	endpoints, err := (&EndpointService{}).GetAll()
	if err != nil {
		t.Fatalf("load endpoints: %v", err)
	}
	if len(*endpoints) != 1 || (*endpoints)[0]["tag"] != "wireguard-main" {
		t.Fatalf("legacy Mieru endpoint leaked into node management: %#v", *endpoints)
	}

	_, err = (&EndpointService{}).Save(
		db,
		"new",
		json.RawMessage(`{"type":"mieru","tag":"new-mieru","listen_port":22002}`),
	)
	if err == nil {
		t.Fatal("expected node management to reject new Mieru endpoints")
	}
}
