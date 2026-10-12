# Third-Party Notices

## Sing-Box

NovaPanel includes `github.com/sagernet/sing-box v1.15.0-alpha.12`, official
commit `25b0d5ea3e466d246c708ac0f20d0448110e566c`, without local production
source patches. Upstream's scoped lifecycle supersedes the former atomic
start-state patch; a local lifecycle regression test is retained.
The complete corresponding source, original license, provenance, checksums,
and verification instructions are included in the NovaPanel source repository
under `third_party/sing-box` and `third_party/SING_BOX_PATCH.md`.

Copyright (C) 2022 by nekohasekai. Sing-Box is licensed under GPL-3.0-or-later,
without warranty. Its license additionally prohibits using the application's
name or implying association in derivative works without prior consent.
The original notice is included in `licenses/sing-box-LICENSE`; the full GPL v3
text is provided in the accompanying `LICENSE` file.

## Mieru

NovaPanel Linux release packages include the `mita` server from
[enfein/mieru](https://github.com/enfein/mieru), version `3.34.1`, built from
commit `8b42e23979d14d5afe078d21f9e7d4a6407389b2` with NovaPanel's authenticated
local bridge patch in `patches/mieru-novapanel-bridge-auth.patch` and session
user/IP reporting patch in `patches/mieru-novapanel-source-ip.patch`.

Mieru is licensed under the GNU General Public License v3.0. Its source code and
license are available from the upstream repository. The release build fetches
the pinned source commit, applies both published patches, runs the relevant
SOCKS5, protocol, and CLI tests, and builds the binary reproducibly.
