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

func TestApplyFleetResourceInfoSupportsLocalAndRemotePayloads(t *testing.T) {
	local := &FleetServerView{}
	applyFleetResourceInfo(local, map[string]interface{}{
		"cpu": float64(23.5),
		"memory": map[string]interface{}{
			"current": uint64(4 * 1024),
			"total":   uint64(8 * 1024),
		},
		"network": map[string]interface{}{
			"sent": uint64(1000),
			"recv": uint64(2000),
		},
	})
	if !local.ResourcesReady || local.CPUPercent != 23.5 || local.MemoryUsed != 4096 || local.MemoryTotal != 8192 || local.NetworkSent != 1000 || local.NetworkReceived != 2000 {
		t.Fatalf("local resource payload was not applied: %+v", local)
	}

	remote := &FleetServerView{}
	applyFleetResourceInfo(remote, map[string]interface{}{
		"cpu": float64(61.25),
		"memory": map[string]interface{}{
			"current": float64(6000),
			"total":   float64(10000),
		},
		"network": map[string]interface{}{
			"sent": float64(3000),
			"recv": float64(9000),
		},
	})
	if !remote.ResourcesReady || remote.CPUPercent != 61.25 || remote.MemoryUsed != 6000 || remote.MemoryTotal != 10000 || remote.NetworkSent != 3000 || remote.NetworkReceived != 9000 {
		t.Fatalf("remote resource payload was not applied: %+v", remote)
	}
}
