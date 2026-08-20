# Third-Party Notices

## Mieru

NovaPanel Linux release packages include the `mita` server from
[enfein/mieru](https://github.com/enfein/mieru), version `3.34.1`, built from
commit `8b42e23979d14d5afe078d21f9e7d4a6407389b2` with NovaPanel's authenticated
local bridge patch in `patches/mieru-novapanel-bridge-auth.patch`.

Mieru is licensed under the GNU General Public License v3.0. Its source code and
license are available from the upstream repository. The release build fetches
the pinned source commit, applies the published patch, runs its SOCKS5 tests,
and builds the binary reproducibly.
