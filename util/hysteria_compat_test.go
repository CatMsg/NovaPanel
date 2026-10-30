package util

import "testing"

func TestHysteriaLinksRejectRemovedTuningOptions(t *testing.T) {
	for _, field := range []string{"recv_window_conn", "recv_window"} {
		if _, _, err := GetOutbound("hysteria://secret@example.com:443?"+field+"=123", 0); err == nil {
			t.Fatalf("Hysteria link with removed option %q was accepted", field)
		}
	}
}

func TestHysteriaOutboundUsesCurrentTuningNames(t *testing.T) {
	outbound := map[string]interface{}{
		"recv_window_conn":      1,
		"recv_window":           2,
		"disable_mtu_discovery": true,
	}
	inbound := map[string]interface{}{
		"connection_receive_window":  100,
		"stream_receive_window":      200,
		"max_concurrent_streams":     30,
		"disable_path_mtu_discovery": true,
	}
	hysteriaOut(&outbound, inbound)
	for _, field := range []string{"recv_window_conn", "recv_window", "disable_mtu_discovery"} {
		if _, exists := outbound[field]; exists {
			t.Fatalf("generated Hysteria client kept removed field %q: %#v", field, outbound)
		}
	}
	for field, want := range map[string]interface{}{
		"connection_receive_window":  100,
		"stream_receive_window":      200,
		"max_concurrent_streams":     30,
		"disable_path_mtu_discovery": true,
	} {
		if got := outbound[field]; got != want {
			t.Fatalf("generated Hysteria field %q = %#v, want %#v", field, got, want)
		}
	}
}

func TestHysteriaOutboundMigratesLegacyInboundTuningNames(t *testing.T) {
	outbound := map[string]interface{}{}
	inbound := map[string]interface{}{
		"recv_window_conn":      100,
		"recv_window_client":    200,
		"max_conn_client":       30,
		"disable_mtu_discovery": true,
	}
	hysteriaOut(&outbound, inbound)
	for field, want := range map[string]interface{}{
		"stream_receive_window":      100,
		"connection_receive_window":  200,
		"max_concurrent_streams":     30,
		"disable_path_mtu_discovery": true,
	} {
		if got := outbound[field]; got != want {
			t.Fatalf("generated Hysteria field %q = %#v, want %#v", field, got, want)
		}
	}
}
