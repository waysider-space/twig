# Using twig

## CLI

```sh
twig validate twig.yml
twig validate twig.yml --artifacts dist
twig generate twig.yml --output dist
```

```yaml
schema: 1
name: grove
version: 1.0.0
targets:
  - os: linux
    arch: amd64
    url: https://release.camp/grove/1.0.0/grove-linux-amd64.tar.gz
    sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    artifact: grove-linux-amd64.tar.gz
    files:
      - source: bin/grove
        destination: bin
        path: grove
      - source: defaults/config.yaml
        destination: config
        path: grove/config.yaml
      - source: assets/theme.json
        destination: data
        path: grove/theme.json
```

Targets support `linux`, `darwin`, and `windows`, with `amd64` or `arm64`. Use tar.gz for Unix and ZIP for Windows. Windows binaries must include `.exe` in their source and destination names and match the target architecture.

`source` names an exact, non-empty regular file in the archive; globs aren't supported. Paths use `/`, and sources may start with `./`. `path` is relative to the destination root. Include an app directory for config and data, as above.

## Go library

Use Go structs with your own configuration format; YAML is optional.

```go
import "github.com/waysider-space/twig/installer"

scripts, err := installer.Generate(installer.Config{
    Targets: []installer.Target{{
        OS: "linux",
        Arch: "amd64",
        URL: artifactURL,
        SHA256: artifactChecksum,
        Files: []installer.File{
            {Source: "bin/grove", Destination: "bin", Path: "grove"},
            {Source: "defaults/config.yaml", Destination: "config", Path: "grove/config.yaml"},
        },
    }},
})
```

`Generate` returns `install.sh` and/or `install.ps1` as bytes. `ValidateFiles`, `ValidateTarGz`, and `ValidateZIP` check mappings and local archives separately.

## Installation directories

| Platform | bin | config | data |
| --- | --- | --- | --- |
| Linux | `~/.local/bin` | `$XDG_CONFIG_HOME` or `~/.config` | `$XDG_DATA_HOME` or `~/.local/share` |
| macOS | `~/.local/bin` | `~/Library/Application Support` | `~/Library/Application Support` |
| Windows | LocalApplicationData + `Programs\Twig\bin` | ApplicationData | LocalApplicationData |

Override roots with `TWIG_BIN_DIR`, `TWIG_CONFIG_DIR`, and `TWIG_DATA_DIR`, set to absolute paths before installation. Relative XDG values are ignored. macOS config and data share a root, so their paths must not collide. Twig leaves PATH setup to the user.

Config defaults to `existing: preserve`; bin and data default to `existing: replace`. Set `existing: error` to refuse an existing file. Preserved files keep their contents and permissions. Unix bin files are executable; other files are private unless marked `executable: true`.

## Downloads

Artifact URLs may redirect. Bearer tokens are sent only to the initial URL. HTTPS is required except for loopback HTTP during development.

For bearer authentication, set `auth_env: GROVE_DOWNLOAD_TOKEN` or `Config.AuthEnv`. Users set that variable when installing. Tokens aren't embedded in scripts and may contain letters, digits, `.`, `_`, `~`, `+`, `/`, `=`, or `-`.

## Limits and safety

Downloads are checked against their SHA-256 checksum. Only run installers from publishers you trust.

Archive limits are optional; omitted or zero fields are unlimited:

```yaml
limits:
  max_entries: 10000
  max_expanded_bytes: 21474836480
```

Go callers set `Config.Limits` with `installer.Limits`. The byte limit applies separately to expanded tar content and all staged files, including repeated mappings.

Allow temporary space for expanded archives and staged files. Files are checked before installation, but a later write failure can leave earlier files updated.

Use installation directories you control. Paths below the roots cannot contain symlinks or Windows reparse points; Windows roots and their parents cannot contain reparse points either.
