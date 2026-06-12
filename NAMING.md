# NovaPanel Naming Guide

This document describes which names should stay for compatibility and which names are safe to present as NovaPanel in public-facing text.

## Keep For Compatibility

Keep these identifiers unless you are deliberately planning a migration path:

- `sui` binary name
- `s-ui` system service name and command entrypoint
- `SUI_*` environment variables
- `/usr/local/s-ui` and `C:\Program Files\s-ui` install paths
- `config/name` value used for the database filename
- session key `"s-ui"` in the web session store
- legacy Windows service IDs and wrapper filenames
- database backup filename prefix `s-ui_`

These names are part of upgrade paths, old scripts, existing services, or persisted data.

## Safe To Present As NovaPanel

These are safe to change in documentation, UI, package metadata, and display text:

- README titles and badges
- Web page title and navigation labels
- Frontend package name and lockfile metadata
- Windows service display name and description
- Docker example service/container names
- Release notes, image tags, and public-facing descriptions
- Menu labels, helper text, and user-facing prompts

## Conditional Changes

These can be changed if you are also willing to adjust migration or compatibility behavior:

- systemd unit file names
- Windows service wrapper names
- default data directories
- backup/export filename prefixes
- install/uninstall script filenames

Changing these usually requires a migration or compatibility shim so existing installations do not break.

## Recommended Policy

1. Keep the compatibility layer stable.
2. Present NovaPanel in all new public-facing text.
3. Only rename runtime identifiers when there is a migration plan.
4. If a new name is introduced, document the compatibility alias once and do not repeat the old brand everywhere.

## Current Repository State

The repository is already aligned with the policy above:

- public branding is NovaPanel
- compatibility commands still use `s-ui`
- install paths still use `/usr/local/s-ui`
- environment variables still use `SUI_*`

