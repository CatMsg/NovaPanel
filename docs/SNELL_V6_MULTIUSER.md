# Snell v6 Single-Port Multi-User

## Design

- One Snell v6 inbound listens on one TCP port and uses one shared server PSK for the Snell transport.
- Each enabled panel client receives a separate cryptographically random `userkey`. Mihomo and sing-box send it as the Snell v6 ClientID; the server maps that ID to the local panel user.
- TCP and UDP-over-TCP use the same per-user identity, so traffic limits, totals, active sessions, and history remain attributed to that user.
- New inbounds default to per-user authentication. Existing inbounds without `user_mode` keep legacy shared-PSK behavior; shared mode remains available when using an unmodified Mihomo core.
- Removing a user from the inbound removes that user's ID from the running listener. A listener with no enabled users is not started.

The `userkey` is a bearer credential and must not be shared. The PSK remains common to the inbound; it is not copied or regenerated per user.

## Client Compatibility

sing-box JSON subscriptions support per-user `userkey` directly. Upstream Mihomo v1.19.34 does not expose this field, so per-user Clash/Mihomo subscriptions require the NovaPanel-patched Mihomo core. The normal upstream core cannot authenticate to an inbound configured for per-user mode. For upstream Mihomo, choose shared-PSK mode and accept that per-user counters, limits, and history are unavailable.

The Mihomo patch is pinned to upstream source commit `0455d0d28c4526937380505153e65a843d8612e8` (v1.19.34) and is stored at `docs/patches/mihomo-v1.19.34-snell-userkey.patch`. It adds `userkey` parsing and writes the ID in both TCP and UDP headers while retaining empty-ID compatibility for existing clients.

## Build and Verification

The custom ARM64 Linux core prepared for AX88U MC2 is stored at:

`~/Downloads/MC2-Mihomo-v1.19.34-userkey/clash`

It is a UPX-compressed Mihomo v1.19.34 build with the `with_gvisor` tag. The uncompressed build identifies as `v1.19.34-userkey`. It has not been installed on the router.

The integration test `TestMihomoSnellV6MultiUserSinglePort` runs the custom Mihomo process against one NovaPanel sing-box Snell listener, sends TCP and UDP traffic as two different users, and verifies echo, server-side identity, and user history. Set `NOVAPANEL_MIHOMO_USERKEY_TEST_BINARY` to a host-runnable custom Mihomo binary to run it; it is skipped when the variable is unset. The upstream Mihomo binary must not be substituted because it lacks the `userkey` option.
