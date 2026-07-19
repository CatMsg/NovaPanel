package service

import "testing"

func TestApplyFleetDatabaseInfoSupportsLocalAndRemotePayloads(t *testing.T) {
	local := &FleetServerView{}
	applyFleetDatabaseInfo(local, map[string]int64{
		"clients":   5,
		"inbounds":  4,
		"outbounds": 1,
		"endpoints": 2,
	})
	if local.Clients != 5 || local.Inbounds != 4 || local.Outbounds != 1 || local.Endpoints != 2 {
		t.Fatalf("local database payload was not applied: %+v", local)
	}

	remote := &FleetServerView{}
	applyFleetDatabaseInfo(remote, map[string]interface{}{
		"clients":   float64(6),
		"inbounds":  float64(3),
		"outbounds": float64(2),
		"endpoints": float64(1),
	})
	if remote.Clients != 6 || remote.Inbounds != 3 || remote.Outbounds != 2 || remote.Endpoints != 1 {
		t.Fatalf("remote database payload was not applied: %+v", remote)
	}
}
