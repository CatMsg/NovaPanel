# sing-box 1.16 Upgrade Plan

Status: compatibility preparation in progress; do not upgrade production before an official 1.16 release and completion of the gates below.

## Recommendation

Upgrade when 1.16 is stable and the panel passes its compatibility and canary checks. Do not upgrade solely for a speed increase: the new TUN stack is not expected to materially improve the panel's usual VPS proxy-inbound workload.

Current source baseline: sing-box `v1.15.0-alpha.13`, replaced by the pinned local source at `third_party/sing-box` with the audited MASQUE and URLTest patches documented in `SING_BOX_MASQUE_INTEGRATION.md`. The default configuration already declares `http_clients` and `route.default_http_client`. The TLS editor and runtime path previously used inline `tls.acme`; arbitrary saved Sing-Box JSON can also preserve deprecated fields, so an engine bump alone is unsafe.

Implementation progress (2026-10-29): the local TLS UI and runtime-config path now convert legacy inline ACME to `certificate_provider` without rewriting stored JSON during runtime generation; TLS template edits save the modern form. Empty legacy drafts remain stored but are omitted from runtime config. Configuration validation migrates legacy remote-rule-set `download_detour`, removes `independent_cache`, migrates `store_rdrc` to `store_dns`, materializes the old implicit remote-rule-set HTTP-client behavior as an explicit client using the prior default outbound (`route.final`, or sing-box's generated direct fallback when unset), and migrates Hysteria v1 legacy QUIC fields using the upstream's direction-specific replacements. Existing HTTP-client lists retain their first-client default. Ambiguous DNS action strategies, `rule_set_ip_cidr_accept_empty=true`, old response-address filters, `rdrc_timeout`, and conflicting old/new Hysteria values fail with a field path rather than being discarded. The local development database was read-only scanned for these deprecated fields; none were found in its settings, TLS, inbound, or service config JSON. ACME issuance/renewal and actual sing-box 1.16 startup have not been exercised, so the compatibility gate remains open.

Raw full sing-box JSON now also migrates nested legacy `tls.acme` during validation; this path bypasses database model serialization and was separately tested against the pinned sing-box parser. Existing stored JSON remains unchanged until the normalized config is saved or generated.

Interim upstream review (2026-10-30): the [official release page](https://github.com/SagerNet/sing-box/releases) lists sing-box `1.14.4` as the latest stable release and `1.15.0-alpha.13` as a prerelease; no 1.16 release is available. The alpha.12-to-alpha.13 comparison contains 109 commits, including MASQUE, HTTP proxy, DNS cache persistence, TUN, gVisor removal, and UDP/QUIC changes. Alpha.13 has been imported from its pinned official module archive; the source-integrity verifier, release Cronet pin, and CI baseline were updated. Local project gates and the hosted Linux release gates passed for NovaPanel `v1.6.149` (workflow [`40975087801`](https://github.com/CatMsg/NovaPanel/actions/runs/40975087801)). Public-network MASQUE interoperability and production canary/ACME behavior remain unverified; no 1.16 production upgrade should be inferred from the alpha baseline.

Additional alpha.13 compatibility review (2026-10-30): alpha.13 removes the route `action: direct` variant. The config normalizer moves its dialer options to a generated direct outbound and rewrites the rule to `action: route`, preserving rule matches and avoiding tag collisions. Since direct outbounds default UDP fragmentation on while the old action defaulted it off, migration writes `udp_fragment: false` when the old option was absent or null. These migrations pass the pinned alpha.13 parser. The checked-in alpha.13 tree passed full Go tests, vet, staticcheck, build, feature-tagged Sing-Box race acceptance, and digest-pinned Mihomo TCP/UDP/auth/subscription acceptance. The frontend checks and all 57 E2E tests also pass. Hosted Linux release gates subsequently passed in workflow `40975087801`; local/CI verification still does not substitute for public-network protocol validation.

Hysteria v1 compatibility review (2026-10-30): upstream's current inbound/outbound migration docs specify direction-specific old names for the shared QUIC fields. Inbounds map `recv_window_conn` to `stream_receive_window`, `recv_window_client` to `connection_receive_window`, `max_conn_client` to `max_concurrent_streams`, and `disable_mtu_discovery` to `disable_path_mtu_discovery`. Outbounds map `recv_window_conn` to `stream_receive_window`, `recv_window` to `connection_receive_window`, and `disable_mtu_discovery` to `disable_path_mtu_discovery`. The previous validator had the window mappings reversed and rejected these documented safe migrations; it now migrates them, rejects conflicting old/new values, and preserves legacy inbound values in generated client subscriptions. The upstream testing changelog also briefly marked Tailscale endpoint dial fields for removal, then explicitly reverted that deprecation in a later testing entry; recheck the final 1.16 migration guide before changing those fields.

## Before 1.16 Is Stable

- [x] Replace the TLS UI/config path from inline `tls.acme` with the certificate-provider model. Existing TLS template behavior, ACME settings, and certificate selection are preserved; live issuance and renewal remain to be tested.
- [x] Inventory persisted and user-supplied JSON for the 1.16 removal set, including cache-file `rdrc_timeout`; the current local development DB scan found no deprecated occurrences in its settings, TLS, inbound, or service config JSON.
- [x] Apply tested, lossless migrations where upstream documents a direct replacement; reject ambiguous fields with an exact JSON path instead of silently discarding them.
- [x] Migrate Hysteria v1's documented legacy QUIC field names in both inbound and outbound configs, reject conflicting old/new values, and preserve inbound tuning in generated client subscriptions.
- [x] Add regression coverage for migrations, conflicts, implicit HTTP-client fallback, and rejected saves leaving persisted state unchanged. ACME issue/renewal behavior remains to be tested.
- [x] Keep explicit HTTP-client setup while migrating old implicit-client configs to the equivalent explicit default outbound.
- [x] Reassess the next official 1.15 prerelease and replay the local MASQUE integration patch against an isolated alpha.13 source copy; preserve the upstream blocking packet-write semantics.
- [x] Import alpha.13 into the project, update source-integrity and CI pins, and rerun local regression plus native/Mihomo MASQUE TCP/UDP acceptance against the checked-in source. Hosted Linux release gates passed in v1.6.149; public-network interoperability and production canary validation remain pending.

## After Official 1.16 Release

- [ ] Review the final migration guide and release notes; update this plan if removals or schema semantics changed.
- [ ] Import the exact official 1.16 source into `third_party/sing-box`; update module/version checksums, source-integrity verifier, patch/test fingerprints, build tags, and any required Go toolchain pins. Review API changes against the current alpha.13 integration rather than assuming prerelease compatibility.
- [ ] Run the full release-pinned Go test/build/vet/race gates and front-end checks. Confirm generated and saved configurations parse and start under the actual 1.16 binary.
- [ ] Preserve and pass protocol acceptance: HTTP/1, HTTP/2, HTTP/3 and CONNECT-UDP; native sing-box MASQUE; Mihomo MASQUE authentication plus real TCP/UDP round trips; DNS/rule-set behavior; TLS/ACME renewal.

## Canary and Rollout

- [ ] Publish a candidate only after all local and CI gates pass.
- [ ] Upgrade one canary VPS first with the official updater and its rollback snapshot. Verify the panel health check, Sing-Box startup, listeners, existing database/configuration, ACME state, and representative user traffic.
- [ ] Roll out to the remaining fleet only after the canary is healthy; verify each host independently and stop the rollout on the first failure.

## Out of Scope

- No claim of improved speed for ordinary VPS proxy inbounds; measure any performance claim separately.
- Upgrading sing-box does not add native Mieru inbound support.

## Separate 1.17 Follow-Up

- [ ] The panel exposes a TUN inbound editor. Before pinning sing-box 1.17, inventory persisted and raw user-supplied TUN inbounds for the deprecated `stack` field, implement a version-targeted migration that removes it, and verify startup and TUN behavior for each previously supported stack value. Do not strip it while running the current 1.15 baseline or during the 1.16 upgrade, because that would change runtime behavior before the removal boundary.
