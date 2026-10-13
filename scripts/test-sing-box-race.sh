#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

go run -mod=readonly ./scripts/verify-sing-box.go
go test -mod=readonly -race ./scripts/verify-sing-box.go ./scripts/verify-sing-box_test.go \
  -count=1 -timeout=60s
go test -mod=readonly -race ./scripts -count=1 -timeout=60s
go test -mod=readonly -race github.com/sagernet/sing-box/route \
  -run '^TestNetworkManagerStarted' -count=20 -timeout=120s
go test -mod=readonly -race ./core \
  -run '^TestAddInboundNaiveRequiresUsers$' -count=20 -timeout=120s
http_test_output="$(mktemp)"
trap 'unlink "$http_test_output"' EXIT
GOMAXPROCS=2 go test -mod=readonly -race -p 2 \
  -tags 'with_quic,badlinkname,tfogo_checklinkname0' \
  -ldflags='-checklinkname=0' ./core \
  -run '^(TestHTTPProxyLoopbackIntegration|TestHTTPProxyGeneratedSingletonVersionLoopback|TestDNSConfigurationRuleContextIntegration|TestManagedMASQUEHTTP3Loopback|TestManagedMASQUEHTTP3ActuallyRateLimitsTCPAndUDP|TestManagedMASQUEHTTP3RejectsPeerRouteSpoofing|TestManagedMASQUEHTTP3RejectsWrongTLSIdentity)$' \
  -count=1 -timeout=180s -v | tee "$http_test_output"
for required_pass in \
  '--- PASS: TestHTTPProxyLoopbackIntegration/http1_tcp_and_connect_udp' \
  '--- PASS: TestHTTPProxyLoopbackIntegration/http2_tcp_and_connect_udp' \
  '--- PASS: TestHTTPProxyLoopbackIntegration/http3_tcp_and_connect_udp' \
  '--- PASS: TestHTTPProxyGeneratedSingletonVersionLoopback/http1' \
  '--- PASS: TestHTTPProxyGeneratedSingletonVersionLoopback/http2' \
  '--- PASS: TestHTTPProxyGeneratedSingletonVersionLoopback/http3' \
  '--- PASS: TestDNSConfigurationRuleContextIntegration' \
  '--- PASS: TestManagedMASQUEHTTP3Loopback' \
  '--- PASS: TestManagedMASQUEHTTP3ActuallyRateLimitsTCPAndUDP/tcp' \
  '--- PASS: TestManagedMASQUEHTTP3ActuallyRateLimitsTCPAndUDP/udp' \
  '--- PASS: TestManagedMASQUEHTTP3ActuallyRateLimitsTCPAndUDP' \
  '--- PASS: TestManagedMASQUEHTTP3RejectsPeerRouteSpoofing' \
  '--- PASS: TestManagedMASQUEHTTP3RejectsWrongTLSIdentity'; do
  if ! grep -Fq -- "$required_pass" "$http_test_output"; then
    printf 'required race test did not pass: %s\n' "$required_pass" >&2
    exit 1
  fi
done
# Native H3 generation/revocation and aggregation must run, not silently skip.
GOMAXPROCS=2 go test -mod=readonly -race -p 2 \
  -tags 'with_quic,badlinkname,tfogo_checklinkname0' \
  -ldflags='-checklinkname=0' ./service ./sub \
  -run '^(TestMASQUEManagedHTTP3GeneratedConfigAndUserRevocation|TestAggregateNativeMASQUEEndpointsAndSourceReferences)$' \
  -count=1 -timeout=180s -v | tee "$http_test_output"
for required_pass in \
  '--- PASS: TestMASQUEManagedHTTP3GeneratedConfigAndUserRevocation' \
  '--- PASS: TestAggregateNativeMASQUEEndpointsAndSourceReferences/native_runtime'; do
  if ! grep -Fq -- "$required_pass" "$http_test_output"; then
    printf 'required race test did not pass: %s\n' "$required_pass" >&2
    exit 1
  fi
done
go test -mod=readonly -race ./core -count=1 -timeout=180s
