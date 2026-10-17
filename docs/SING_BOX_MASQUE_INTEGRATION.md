# Audited native MASQUE integration source

This is the current source-verification contract for the native MASQUE
integration. It supersedes the historical "no local production source patches"
and nonempty-test-only verification descriptions in
`third_party/SING_BOX_PATCH.md`.

## Scope and provenance

The baseline remains `github.com/sagernet/sing-box v1.15.0-alpha.12`, tag commit
`25b0d5ea3e466d246c708ac0f20d0448110e566c`. Its official download is unchanged:

- Module sum: `h1:mAzZow7/pGOmTLVcFUnmXKgkLEmVmOUDswNjTdYf3Ns=`.
- Module go.mod sum: `h1:1gZ6h/h1qzAEVu8bQhvtVQWW/gy1EsueQcJ9uCl+vCU=`.
- Module ZIP SHA-256: `7d6e28f0b3380a92f34f30cd22bca2bc05e88384dfe6f3b6cc19f9848a84414f`.

Of the ZIP's 1,566 files, two have audited MASQUE integration hooks and one
has a URLTest selection race fix; the other 1,563 remain byte-identical to the ZIP. Native authentication,
CONNECT-IP, HTTP/3, the internal stack, and packet/session processing remain
upstream implementations. Optional embedding hooks adapt authentication,
address allocation and accepted route announcements, not the data plane.

The patch is deliberately limited to:

- `protocol/masque/server.go`: pass the optional typed
  `ServerSessionObserver` from the embedding context to the native transport,
  and allow a typed HTTP-options customizer to wrap tunnel authentication.
- `transport/masque/server.go`: expose the optional observer and invoke
  `OnSessionOpened` with authenticated user, real peer, and cloned assigned
  addresses before accepting the tunnel. A returned release callback runs
  before pool address reuse, including error exits. Check `AllowPeerRoutes`
  before mutating peer advertisements and `AllowPeerForwarding` before direct
  tunnel-to-tunnel forwarding. Prefer the transport's actual remote address
  over forwarding headers for identity records. Optional address and route
  policies reserve fixed client addresses and discard Mihomo's exact default
  announcements before the existing peer-route rejection. With no observer,
  the upstream allocation/authentication behavior is unchanged.
- `protocol/group/urltest.go`: protect selected TCP/UDP outbound snapshots
  with one read/write lock. Automatic selection updates already have a
  separate writer lock; readers in references, dialing and selection now
  synchronize with those writes. A QUIC-enabled aggregated MASQUE profile
  reproduced the race between URLTest updates and the reference manager.
  The same real-start regression passed 20 times under `-race` after the fix.

Embedding policy, accounting, and runtime behavior are reviewed and tested
separately by the parent integration task. The source gate does not prove
authentication, traffic accounting, quotas, or session behavior at runtime.

## Exact integrity pins

| File | Official baseline SHA-256 | Accepted patched SHA-256 |
| --- | --- | --- |
| `protocol/masque/server.go` | `9cd47ca60adbcc0ff440ae4ab04c1b7ba44c1e9ba111812e21d8e7d5b86ea45e` | `b8a77ff30e5036999c21702856694e8c1c1d9483179477f42cf587bb45e1cb57` |
| `transport/masque/server.go` | `75e07c475865fe3539cf1470fe7fa15d019d3261958111c341ebba5f38157086` | `14c4a4ed410363f3f19cd261632eda137defa7b6916edfd8a3772547e1277d4d` |
| `protocol/group/urltest.go` | `fc3d26ed1cecf664f23c870b5caa3f96af868dd4cdbca4d46bc6a309fdaff2c2` | `edc7dd0a2a91d19e81b036f65786b3b3a96ab0b5c11fbf543249b0874ee95779` |

Artifact:
`docs/patches/sing-box-v1.15.0-alpha.12-masque-integration.patch`.
Artifact SHA-256:
`82a9a1e307fcfdd377034251e5c322cedccee9649e69a9c35e64be6690da04ee`.

The sole added vendor file is the existing regression test
`route/network_started_test.go`, SHA-256
`2f7b8eb0aa76ccc1348dca086e1ee8bd4eb68d360041a030fc8592b58898c0c2`.
It is now pinned byte-for-byte, not merely required to be nonempty.

The verifier first authenticates module identity, cached module sums, and ZIP
SHA-256, then recomputes both Go `h1` sums from ZIP contents. It authenticates
the patch SHA and all three baseline file SHAs before replaying the unified patch.
Replay requires the fixed three paths in order, exact offsets/counts/context,
and exact patched SHAs. It has no fuzzy matching, wildcard exclusions,
rename/removal support, or arbitrary allowlist supplied by an external file.

Every source file must match the resulting expectation. Missing, unknown,
nonregular, ignored/untracked, or changed files fail, including the three
approved files and the added regression test. The patch must also be
Git-visible. Working source and `--source` use the same exact expectation.
As before, nonignored untracked files are considered Git-visible for
pre-commit validation; this is not proof they are present in a committed
archive. Validate the actual extracted archive before publication.

The official embedded `go.mod`, `go.sum`, and `.github/CRONET_GO_VERSION`
are not patched. Existing release-workflow/root-module Cronet alignment and
no-replacement checks remain enforced.

## Reproduce and independently replay

Run from the repository root, using Go 1.26.9. This operates only on a temporary
copy of the official ZIP, never on the vendor tree or expanded module cache:

```sh
export GOTOOLCHAIN=go1.26.9
go run -mod=readonly ./scripts/verify-sing-box.go
tmp=$(mktemp -d)
zip="$(go env GOMODCACHE)/cache/download/github.com/sagernet/sing-box/@v/v1.15.0-alpha.12.zip"
unzip -q "$zip" -d "$tmp"
baseline="$tmp/github.com/sagernet/sing-box@v1.15.0-alpha.12"
artifact="$PWD/docs/patches/sing-box-v1.15.0-alpha.12-masque-integration.patch"
(
  for source_file in protocol/masque/server.go transport/masque/server.go protocol/group/urltest.go; do
    diff_status=0
    LC_ALL=C diff -u -L "a/$source_file" -L "b/$source_file" \
      "$baseline/$source_file" "third_party/sing-box/$source_file" || diff_status=$?
    if [ "$diff_status" -ne 1 ]; then exit 1; fi
  done
) > "$tmp/reproduced.patch"
cmp "$tmp/reproduced.patch" "$artifact"
git -C "$baseline" apply --check "$artifact"
git -C "$baseline" apply "$artifact"
for source_file in protocol/masque/server.go transport/masque/server.go protocol/group/urltest.go; do
  cmp "$baseline/$source_file" "third_party/sing-box/$source_file"
done
cp third_party/sing-box/route/network_started_test.go "$baseline/route/"
go run -mod=readonly ./scripts/verify-sing-box.go --source "$baseline"
```

The timestamp-free unified artifact uses three context lines and explicit
`a/`/`b/` labels, in the listed file order. The verifier replays the artifact
without invoking an external patch utility. The independent `git apply`
check above confirms interoperability. Any future hook change requires a
new review of the diff, artifact, and all integrity pins; do not
refresh hashes just to make a failing gate pass.

## Acceptance checks

```sh
GOTOOLCHAIN=go1.26.9 go run -mod=readonly ./scripts/verify-sing-box.go
GOTOOLCHAIN=go1.26.9 go test -mod=readonly -race \
  ./scripts/verify-sing-box.go ./scripts/verify-sing-box_test.go \
  -count=1 -timeout=120s
GOTOOLCHAIN=go1.26.9 go test -mod=readonly \
  ./scripts/verify-sing-box.go ./scripts/verify-sing-box_test.go \
  -run='^$' -fuzz=FuzzExactAuditedPatch -fuzztime=5s -parallel=2
GOTOOLCHAIN=go1.26.9 go run -mod=readonly ./scripts/verify-sing-box.go \
  --source /absolute/path/to/extracted/archive/third_party/sing-box
GOTOOLCHAIN=go1.26.9 GOMAXPROCS=2 \
  bash ./scripts/test-sing-box-race.sh
```

The test wrapper `scripts/go-test-sing-box.sh` supplies the QUIC and
linkname-compatible tags required for the full feature suite. The ordinary
`go test ./core` command remains useful on Go 1.27: tests for optional H2
CONNECT-UDP support skip when that build tag is absent, while the tagged race
gate requires the H2 and H3 cases to pass.

Tests cover the real pinned ZIP and checksums, exact patch replay, altered
baseline/patched hashes, artifact tampering, unknown/reordered allowlists,
path traversal, file creation/removal, changed context/offsets/counts,
truncation, integer overflow, symlinks, missing/extra files, and Cronet drift.
An end-to-end test verifies the real checkout, then creates a temporary Git
repository and a genuine `git archive` from a tree object without making any
commit or using the real checkout's index. Source and archive tampering and
Git visibility failures are checked there without modifying real vendor files.

The race gate requires explicit QUIC-enabled PASS records for managed H3
loopback, actual TCP/UDP rate limits, spoofed peer route rejection, wrong TLS
identity rejection, generated server/user revocation and the aggregate native
runtime subtest. A skipped parent or child does not satisfy these checks.
The default full-core race run includes `TestManagedMASQUEIdentityLease`.
These checks do not prove public-network client interoperability or performance.

## Optional Mihomo compatibility

Native Basic authentication and Mihomo's key authentication are different wire
profiles. An inbound may opt into both on the same UDP listener. The panel
verifies ECDSA client certificates against per-user public keys, maps trusted
identities into native accounting, and accepts `cf-connect-ip` alongside
`connect-ip`. The server TLS certificate must use ECDSA; a conflicting existing
TLS client-authentication policy is rejected, never silently weakened.

Each user/inbound has stable generated ECDSA keys and a reserved /32 address.
Native dynamic allocation skips these addresses, including inactive ones.
An exhausted pool stops after a full allocation cycle instead of repeatedly
rescanning the same inactive reserved address.
One key/address supports one concurrent tunnel; separate client installations
must use separate users. Removing a user or rotating credentials replaces the
native endpoint, closes existing tracked connections and removes the old key.
Normal saves preserve keys and unrelated protocol credentials.

Mihomo advertises IPv4/IPv6 default routes unconditionally. Only exact,
protocol-zero full-default announcements are discarded; they are never
installed. Other announcements, spoofed sources, direct tunnel forwarding and
ICMP bypasses remain rejected by the managed policy.

sing-box JSON contains native credentials only. Clash YAML contains Mihomo
keys and the expanded CONNECT-IP URI only when compatibility is enabled.
There is no invented universal `masque://` link. The server public key is
pinned, so a TLS public-key rotation requires clients to refresh subscriptions.

Run `GOTOOLCHAIN=go1.26.9 bash scripts/test-mihomo-masque.sh`. This downloads an
official digest-pinned Mihomo v1.19.31 binary for Linux amd64 or macOS arm64
(or accepts an explicitly supplied `NOVAPANEL_MIHOMO_TEST_BINARY`) and requires
non-skipped PASS records for both-client TCP/UDP, tracked accounting, native
dynamic/static isolation, revoked-key traffic refusal, authentication and
route controls, stable/batch credentials and real generated YAML parsing.
Tests use loopback, temporary synthetic keys and databases, not a host TUN or
live user subscriptions. Both CI and release workflows invoke the gate.
