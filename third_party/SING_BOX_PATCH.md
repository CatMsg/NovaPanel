# Sing-Box local dependency patch

## Provenance

- Module: `github.com/sagernet/sing-box v1.15.0-alpha.4`
- Official repository: https://github.com/SagerNet/sing-box
- Tag commit: `4566ef0890e0cde8448000e8aa3223fb286daa94`
- Module checksum: `h1:dqPrdSREKapxsSW17ISeVFZF5I1KY3FagBFCsI2LpV4=`
- Module go.mod checksum: `h1:jtgVcFwVF4XhAlhzoQZkqjLLSnSPizqedVLy8ZQqZEk=`
- Official Go module ZIP SHA-256: `04d2fe633d88676abda1010460f002b51d7e037a2caa4efa6a5ac609ad4ff473`
- Source: the complete official Go module archive, including LICENSE, embedded
  resources, platform files, build tags, documentation, and upstream module files.
- License: GPL-3.0-or-later; see `sing-box/LICENSE` and upstream source headers.

## Production diff

Only `sing-box/route/network.go` differs from the official production source:

1. Import `sync/atomic`.
2. Change `NetworkManager.started` from `bool` to `atomic.Bool`.
3. Use `started.Store(true)` at the existing PostStart publication point.
4. Use `started.Load()` at the existing asynchronous interface-update guard.

The zero value remains false. Callback registration, start/close order, reset
dispatch, and cancellation behavior are unchanged. This removes the confirmed
PostStart/interface-update data race; it does not claim to implement the broader
shutdown/reset changes in upstream commit
`491813c36096e7cd6aafa667b3ef0c9d9724d0e2` (86 insertions / 74 deletions).
That commit was reviewed but is deliberately not backported or used to upgrade
the dependency. `config/coreversion` remains unchanged.

The only added module file is `route/network_started_test.go`. It tests initial
callbacks before PostStart, callbacks racing PostStart, post-start reset dispatch,
and Close cancelling a pending callback. The tests use channels, not sleeps.

## Resolution And Verification

The root `go.mod` replaces the pinned module with `./third_party/sing-box`.
Ordinary Go build, test, development, Docker, and release builds therefore resolve
the same patched source without a cache mutation or an opt-in build flag. The
upstream module's `go.mod`/`go.sum` and the root checksums are not regenerated.

Run from the repository root:

```sh
go run -mod=readonly ./scripts/verify-sing-box.go
bash ./scripts/test-sing-box-race.sh
```

The verifier authenticates the pinned official archive, checks actual module
resolution, and compares every vendored file byte-for-byte against that archive
with exactly the four replacements above. Missing, changed, or extra files fail
verification, except for the explicitly allowed regression test. Existing module
cache source is never modified or used as the comparison baseline.

To reproduce the source import, unpack the official module ZIP reported by
`go mod download -json github.com/sagernet/sing-box@v1.15.0-alpha.4` into an empty
temporary directory, copy its complete module directory into `third_party/sing-box`,
apply only the four changes above, and add the regression test. Do not copy a
potentially modified expanded module-cache directory.

Root Git/Docker ignore exceptions preserve all 1,515 official files, including
public certificate resources and the Windows WinDivert embedded drivers. To
check a freshly extracted Git archive as well, run the verifier with
`-source /absolute/path/to/archive/third_party/sing-box` from the working checkout.
The normal gate also checks Git-visible files; in CI that is the tracked source
from a fresh checkout.

Windows build entrypoints retain the legacy compatibility-check script name,
but it is now read-only. Alpha.4's `systemconfig/source_windows.go` already uses
`MyInterfaces()`; the old `resolv_windows.go` cache patch is obsolete. The checker
verifies provenance and the current implementation instead of rewriting files.
Docker, Linux release packages, and Windows build output include the original
Sing-Box license notice alongside the existing full GPL text and product notices.
