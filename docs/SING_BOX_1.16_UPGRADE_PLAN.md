# sing-box 1.16 Upgrade Plan

Status: planned; do not upgrade production before an official 1.16 release and completion of the compatibility gates below.

## Recommendation

Upgrade when 1.16 is stable and the panel passes its compatibility and canary checks. Do not upgrade solely for a speed increase: the new TUN stack is not expected to materially improve the panel's usual VPS proxy-inbound workload.

Current source baseline: sing-box `v1.15.0-alpha.12`, replaced by the pinned local source at `third_party/sing-box`. The default configuration already declares `http_clients` and `route.default_http_client`. The TLS ACME editor still writes legacy inline `tls.acme`, and arbitrary saved Sing-Box JSON can preserve old fields, so an engine bump alone is unsafe.

## Before 1.16 Is Stable

- [ ] Replace the TLS UI/config path from inline `tls.acme` with the certificate-provider model. Preserve existing TLS template behavior, ACME account/provider settings, certificate selection, and renewals.
- [ ] Inventory persisted and user-supplied Sing-Box JSON for fields scheduled for removal: remote rule-set `download_detour`, implicit HTTP-client fallback, inline `tls.acme`, DNS action `strategy`, `rule_set_ip_cidr_accept_empty`, DNS `independent_cache`, cache-file `store_rdrc`, and legacy DNS response address filters (`ip_cidr` / `ip_is_private` without `match_response`).
- [ ] For old saved configurations, implement either a tested, lossless migration or a preflight error that identifies the exact unsupported field. Never silently drop user configuration.
- [ ] Add regression coverage for existing TLS templates, saved legacy configurations, rejected saves leaving persisted state unchanged, and ACME issue/renewal behavior.
- [ ] Keep the explicit HTTP-client setup and add a regression proving remote rule-set downloads do not rely on the removed implicit fallback.

## After Official 1.16 Release

- [ ] Review the final migration guide and release notes; update this plan if removals or schema semantics changed.
- [ ] Import the exact official source into `third_party/sing-box`; update module/version checksums, source-integrity verifier, patch/test fingerprints, build tags, and any required Go toolchain pins. Review upstream API changes rather than carrying alpha.12 assumptions forward.
- [ ] Run the full release-pinned Go test/build/vet/race gates and front-end checks. Confirm generated and saved configurations parse and start under the actual 1.16 binary.
- [ ] Preserve and pass protocol acceptance: HTTP/1, HTTP/2, HTTP/3 and CONNECT-UDP; native sing-box MASQUE; Mihomo MASQUE authentication plus real TCP/UDP round trips; DNS/rule-set behavior; TLS/ACME renewal.

## Canary and Rollout

- [ ] Publish a candidate only after all local and CI gates pass.
- [ ] Upgrade one canary VPS first with the official updater and its rollback snapshot. Verify the panel health check, Sing-Box startup, listeners, existing database/configuration, ACME state, and representative user traffic.
- [ ] Roll out to the remaining fleet only after the canary is healthy; verify each host independently and stop the rollout on the first failure.

## Out of Scope

- No claim of improved speed for ordinary VPS proxy inbounds; measure any performance claim separately.
- The TUN `stack` removal is a later 1.17 compatibility item and should be tracked separately if the panel exposes or stores TUN configurations.
- Upgrading sing-box does not add native Mieru inbound support.
