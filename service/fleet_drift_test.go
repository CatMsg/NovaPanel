package service

import "testing"

func TestApplyFleetConfigurationDriftUsesLocalBaseline(t *testing.T) {
	baseline := &FleetConfigProfile{
		AppVersion: "1.2.3", WebPort: 9999, WebPath: "/", WebTLS: "enabled",
		SubPort: 2096, SubPath: "/sub/", SubTLS: "enabled", SubMode: "master",
		SubEncode: true, SubShowInfo: true,
	}
	remote := *baseline
	remote.AppVersion = "1.2.2"
	remote.SubPort = 3096
	remote.SubMode = "slave"
	servers := []FleetServerView{
		{ID: "local", Reachable: true, Configuration: baseline},
		{ID: "remote", Reachable: true, Configuration: &remote},
	}

	applyFleetConfigurationDrift(servers)
	if servers[0].DriftCount != 0 {
		t.Fatalf("baseline must not drift: %+v", servers[0])
	}
	if servers[1].DriftCount != 2 {
		t.Fatalf("expected version and subscription port drift, got %+v", servers[1].Drift)
	}
	for _, drift := range servers[1].Drift {
		if drift.Field == "subMode" {
			t.Fatalf("subscription role is intentionally excluded from drift: %+v", drift)
		}
	}
}

func TestApplyFleetConfigurationDriftSkipsUnavailableOrStaleServer(t *testing.T) {
	baseline := &FleetConfigProfile{AppVersion: "1.2.3"}
	remote := &FleetConfigProfile{AppVersion: "1.0.0"}
	servers := []FleetServerView{
		{ID: "local", Reachable: true, Configuration: baseline},
		{ID: "offline", Reachable: false, Configuration: remote},
		{ID: "stale", Reachable: false, LastKnown: true, Configuration: remote},
	}
	applyFleetConfigurationDrift(servers)
	if servers[1].DriftCount != 0 || servers[2].DriftCount != 0 {
		t.Fatalf("offline or stale servers must not report current drift: %+v", servers)
	}
}
