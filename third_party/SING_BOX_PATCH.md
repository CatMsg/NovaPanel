# Sing-Box embedded source and patch history

## Provenance

- Module: `github.com/sagernet/sing-box v1.15.0-alpha.12`
- Official repository: https://github.com/SagerNet/sing-box
- Tag commit: `25b0d5ea3e466d246c708ac0f20d0448110e566c`
- Module checksum: `h1:mAzZow7/pGOmTLVcFUnmXKgkLEmVmOUDswNjTdYf3Ns=`
- Module go.mod checksum: `h1:1gZ6h/h1qzAEVu8bQhvtVQWW/gy1EsueQcJ9uCl+vCU=`
- Official Go module ZIP SHA-256: `7d6e28f0b3380a92f34f30cd22bca2bc05e88384dfe6f3b6cc19f9848a84414f`
- Source: all 1,566 files in the official Go module ZIP, including LICENSE,
  documentation, platform source, embedded resources, and upstream module files.
- License: GPL-3.0-or-later; see `sing-box/LICENSE` and upstream source headers.

## Production Diff

There are no local production source patches. The former alpha.4 patch changed
`NetworkManager.started` from `bool` to `atomic.Bool` and used atomic load/store.
Alpha.12 removes that flag entirely: `interfaceUpdateAccess` guards publication
and use of `startedCtx`, dispatch begins at PostStart, and Scope cancellation plus
`resetRunAccess` drain pending callbacks. Applying the old patch would be wrong.

The only local module file is `route/network_started_test.go`, adapted to the
official scoped lifecycle. It checks pre-PostStart callbacks, callbacks racing
PostStart, reset dispatch, cancellation and joining on close, and repeated close.
Tests synchronize with channels and locks rather than sleeps.

NovaPanel's hot add/remove/replace compatibility is implemented in `core`, not by
altering the official managers. Each dynamic instance owns its own Scope, so a
failed start, replacement, removal, or shutdown runs registered cleanup. The
self-maintained MASQUE service is not migrated to the new native implementation.

Core startup follows alpha.12's stage ordering, including DNS Initialize before
outbound Initialize, and registers ConnectionManager before constructing
NetworkManager. Manager ownership, not structural `adapter.Endpoint` conformance,
determines who starts each outbound: ordinary lifecycle outbounds satisfy that
interface too. Caller metadata survives dynamic construction, and instance
contexts are canceled on constructor failure, removal, replacement and shutdown.

Default selection retains the legacy first-remaining-outbound behavior. Unlike
alpha.4's manager, removing the last default or failing its runtime replacement
installs an implicit direct fallback rather than leaving `Default()` nil. That
runtime fallback uses the reserved empty tag, so a user-defined `direct` tag
cannot collide; the last reserved fallback cannot itself be removed. Initial
startup retains the legacy implicit `direct` tag unless an endpoint owns it.
Validation failure
preserves the old instance. Replacement closes the old listener before starting
the candidate, permitting reuse of the same port; a failed candidate is closed
and not published. Referenced outbounds/endpoints cannot be removed.

## Resolution And Verification

The root `go.mod` pins alpha.12 and replaces it with `./third_party/sing-box`.
The embedded `go.mod` and `go.sum` remain byte-for-byte official; only the root
module graph and checksums are resolved for the newer dependencies.

From the repository root:

```sh
GOMAXPROCS=2 go run -p=2 -mod=readonly ./scripts/verify-sing-box.go
GOMAXPROCS=2 go test -p=2 -mod=readonly -race ./scripts/verify-sing-box.go ./scripts/verify-sing-box_test.go -count=1 -timeout=60s
GOMAXPROCS=2 go test -p=2 -mod=readonly -race github.com/sagernet/sing-box/route -run '^TestNetworkManagerStarted' -count=20 -timeout=120s
GOTOOLCHAIN=go1.26.9 GOMAXPROCS=2 bash ./scripts/test-sing-box-race.sh
```

For full panel validation, use the canonical feature tags and linker flag:

```sh
TAGS='with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_musl,badlinkname,tfogo_checklinkname0,with_tailscale'
GOTOOLCHAIN=go1.26.9 GOMAXPROCS=2 go build -p=2 -mod=readonly -tags "$TAGS" -ldflags=-checklinkname=0 ./...
GOTOOLCHAIN=go1.26.9 GOMAXPROCS=2 go test -p=2 -mod=readonly -tags "$TAGS" -ldflags=-checklinkname=0 ./... -count=1 -timeout=300s
GOTOOLCHAIN=go1.26.9 GOMAXPROCS=2 go test -p=2 -mod=readonly -race -tags "$TAGS" -ldflags=-checklinkname=0 ./core -run 'TestComponentScope|TestOutboundScope|TestBoxConstructorFailure|TestBoxStartFailure|TestBoxCloseBeforeStart|TestCoreImplicit|TestCoreDynamic' -count=10 -timeout=180s
GOTOOLCHAIN=go1.27.2 bash ./scripts/go-test-packages.sh
GOTOOLCHAIN=go1.27.2 GOMAXPROCS=2 bash ./scripts/test-sing-box-race.sh
```

The linker flag is required for upstream's standard-library linkname references
on current Go toolchains; it is not a source patch. Tests use temporary databases,
never the running panel's database. Full CI/build entrypoints are maintained
outside this dependency import and must carry the same linker flag.

Release-equivalent verification is pinned per command to Go 1.26.9; it does not
change the host toolchain. Go 1.26.9 uses upstream's legacy HTTP/2 extended CONNECT
hook in `transport/http/extended_connect_legacy.go`. Go 1.27 uses the standard
library wrapper and `extended_connect_wrap_badlinkname.go` under `badlinkname`;
without that tag its HTTP/2 CONNECT-UDP support is unavailable. The canonical
tags and linker flag retain upstream's required hooks, not a vendored patch.
HTTP/2 UDP must therefore be exercised under the actual release toolchain as
well as any newer local toolchain; a Go 1.27-only pass is not release evidence.

The verifier checks actual module resolution, both Go module checksums and ZIP
SHA-256, and every imported file byte-for-byte against the pinned official ZIP.
Missing, changed, extra, nonregular, or Git-invisible files fail. The one named
regression test is permitted but must be present and nonempty. The same checks
can validate an extracted source archive using
`-source /absolute/path/to/archive/third_party/sing-box`.

To reproduce the import, download with Go's checksum database enabled:

```sh
GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org go mod download -json github.com/sagernet/sing-box@v1.15.0-alpha.12
```

Extract the reported ZIP into an empty temporary directory and copy its complete
module tree to `third_party/sing-box`, deleting stale upstream files while
retaining only the named local regression test. Use content-based comparison
(for example, `rsync -ac --delete` with that test excluded), not size/mtime alone:
Go module ZIP entries share a fixed timestamp. Do not copy a potentially modified
expanded module-cache directory, and never mutate the module cache.

Git/Docker ignore exceptions preserve all official resources, including public
certificates and Windows WinDivert embedded drivers. The legacy Windows checker
script is read-only: alpha.12 uses `MyInterfaces()`, so the old cache patch remains
obsolete. Packages retain the original Sing-Box license alongside the full GPL
text and product notices.
