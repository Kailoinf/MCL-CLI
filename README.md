# mcli — Minecraft Launcher CLI

A pure command line launcher for **Minecraft: Java Edition**, written in Go with
**zero third-party dependencies**. Microsoft accounts only, no GUI, no
interactive wizard: everything is a flag.

* Chinese docs: [README.zh-CN.md](README.zh-CN.md)
* Agent/conventions docs: [AGENTS.md](AGENTS.md)
* Architecture and decisions: [docs/architecture.md](docs/architecture.md),
  [docs/design-decisions.md](docs/design-decisions.md)

## Features

| | |
|---|---|
| Login | Microsoft device-code flow, refresh tokens stored AES-256-GCM encrypted with a key derived from this machine |
| Downloads | BMCLAPI mirror first, automatic fallback to the official Mojang source, SHA-1 verification, retries, large-download confirmation |
| Versions | release / snapshot / old_beta / old_alpha, manifest cache with TTL, `inheritsFrom` merging, legacy `minecraftArguments` support |
| Java | finds local runtimes (PATH, `JAVA_HOME`, distro dirs, `~/.minecraft/runtime`, mcli's own `jdk/`), matches the major version a version needs, can install a runtime from Mojang's mirrored product manifest |
| Instances | multiple named instances, each with its own game directory, JVM args and memory settings; libraries/assets/versions are shared |
| Interface | English and Simplified Chinese, `--lang auto\|en\|zh` |
| Logs | concise terminal output, full log file under `~/.mcl-cli/logs/` |

Not implemented on purpose: Fabric/Forge/Quilt, mod or resource-pack management,
offline accounts, GUI.

## Install

```sh
git clone https://github.com/Kailoinf/MCL-CLI.git
cd MCL-CLI
go build -o mcli .        # Go 1.24+ (uses crypto/pbkdf2 and crypto/hkdf-era stdlib)
./mcli version
```

Optionally `install -m755 mcli /usr/local/bin/mcli`.

## Quick start

```sh
mcli login                                   # opens your browser (use --flow device for a code)
mcli list --available --limit 10             # what can be installed
mcli create vanilla --version 1.21.4         # isolated instance
mcli launch vanilla                          # installs what is missing, then starts the game
```

Everyday use:

```sh
mcli launch vanilla --xmx 4G --width 1920 --height 1080 --dry-run   # see the exe command line
mcli launch --last                            # replay the previous launch
mcli list                                     # instances
mcli config set bmclapi_endpoint https://bmclapi2.bangbang93.com
mcli java which 1.21.4                        # which Java this version needs / will use
```

## Commands

| Command | Purpose |
|---|---|
| `mcli login [--flow browser\|device] [--label <name>] [--no-browser] [--timeout <sec>]` | Microsoft login (browser flow by default) |
| `mcli logout [<account>] [--all]` | remove stored credentials |
| `mcli list [--instances] [--accounts] [--available] [--type <t>] [--search <text>] [--limit N]` | instances, accounts or downloadable versions |
| `mcli create <name> --version <v> [--java <path>] [--xmx 4G] [--xms 1G] [--jvm-arg ...] [--game-arg ...]` | create an instance |
| `mcli delete <name> [--keep-files] [--yes]` | delete an instance (and its saves) |
| `mcli launch <name>\|--last [--version <v>] [--java <path>] [--xmx] [--xms] [--jvm-arg] [--game-arg] [--width N] [--height N] [--dry-run] [--wait] [--account <a>]` | install if needed and start |
| `mcli refresh [--versions] [--all]` | force-refresh cached metadata |
| `mcli config [get\|set\|unset\|list\|path]` | read or change settings |
| `mcli java [list \| install <major> \| which <version>]` | inspect or install Java runtimes |
| `mcli version`, `mcli help [command]` | self-explanatory |

Global options accepted anywhere: `--lang <auto\|en\|zh>`, `--config <path>`,
`--yes` (never prompt), `--quiet`, `--verbose`, `--no-color`.

## Configuration

`mcli config list` shows everything with descriptions. mcli keeps three small
TOML documents in `~/.mcl-cli/`:

| File | Contents | Notes |
|---|---|---|
| `mcli.toml` | the config table | hand-editable; `config set` patches one line, so your comments and ordering survive; portable between machines |
| `state.toml` | accounts, instances, manifest-cache pointer | machine state (0600); tokens are encrypted |
| `last.toml` | the previous launch | deliberately tiny, so `launch --last` never reads the config or state |

| Key | Default | Meaning |
|---|---|---|
| `download_retry_times` | 3 | attempts per file, including the first |
| `http_timeout` | 30 | HTTP timeout in seconds |
| `bmclapi_endpoint` | `https://bmclapi2.bangbang93.com` | mirror root |
| `mirror_fallback` | true | fall back to the official source |
| `manifest_auto_refresh` | true | silently refresh the manifest after its TTL |
| `manifest_ttl_hours` | 6 | manifest cache lifetime |
| `minecraft_dir` | `~/.minecraft` | game root, same layout as the official launcher |
| `versions_dir` | `versions` | version/instance directory name |
| `java_dir` | `~/.mcl-cli/jdk` | where mcli keeps downloaded runtimes |
| `java_path` | (empty) | pin a specific java executable |
| `log_level_file` / `log_level_terminal` | debug / info | `debug\|info\|warn\|error\|off` |
| `auto_memory_percent` | 50 | share of available memory used when sizing the heap |
| `default_xmx` / `default_xms` | (auto) | heap defaults, e.g. `4G` |
| `extra_jvm_args` | (empty) | extra JVM flags for every launch |
| `confirm_large_download` | true | ask Y/N before large downloads |
| `large_download_mb` | 50 | threshold for "large" |
| `close_after_launch` | true | return right after spawning the game |
| `auth_client_id` | reference launcher's public id | OAuth client id for `mcli login` |
| `language` | auto | `auto\|en\|zh` |

Environment overrides: `MCLI_HOME` (default `~/.mcl-cli`), `MCLI_MINECRAFT`
(default `~/.minecraft`), `MCLI_LANG`, and the usual proxy variables
(`HTTPS_PROXY` / `ALL_PROXY`) — useful when the BMCLAPI redirect node is slow.

## Layout

```
~/.mcl-cli/                 state that is not shared with other launchers
├─ mcli.toml                configuration table (hand-editable TOML)
├─ state.toml               accounts, instances, manifest pointer (0600)
├─ last.toml                previous launch, for `mcli launch --last`
├─ jdk/<component>/         Java runtimes installed by mcli
├─ cache/                   cached version manifest (raw JSON)
└─ logs/                    mcli-YYYYMMDD.log, game-<instance>-<time>.log

~/.minecraft/               identical layout to the official launcher
├─ libraries/               shared, maven layout
├─ assets/                  shared: objects/, indexes/, log_configs/
└─ versions/{id}/           version json/jar + natives/
   └─ versions/{instance}/  per-instance game dir (saves, options.txt, ...)
```

Splitting the documents is deliberate: TOML is written and parsed by
`internal/toml` (no dependency), the file name always matches its format, the
config is meant to be opened in an editor, and the previous launch lives in its
own tiny file (see `docs/design-decisions.md`).

## Accounts and security

Only Microsoft accounts are supported. `mcli login` opens the Microsoft
authorize page (`--flow device` switches to the device-code flow if your client
id supports it); no password ever reaches mcli. Tokens are encrypted with
AES-256-GCM under a key derived (PBKDF2-SHA256, 200k iterations) from this
machine's identifiers **plus a per-account salt stored with that account** — no
OS keychain, and an account entry copied to another machine cannot be
decrypted. Everything else (config, instances, last launch) carries no secrets
and works on any machine.

## Mirror behaviour

Every download is expressed as an ordered candidate list: BMCLAPI first (when
configured), the official Mojang URL last. A candidate that fails, stalls or
returns a wrong SHA-1 is abandoned for the next one; the whole list is retried
`download_retry_times` times. Verified mappings:

| Official | BMCLAPI |
|---|---|
| `piston-meta.mojang.com/<path>` | `<endpoint>/<path>` |
| `<version json url>` | `<endpoint>/version/<id>/json` |
| `piston-data…/v1/objects/<sha1>/client.jar` | `<endpoint>/version/<id>/client` |
| `libraries.minecraft.net/<path>` | `<endpoint>/maven/<path>` |
| `resources.download.minecraft.net/<aa>/<h>` | `<endpoint>/assets/<aa>/<h>` |
| `launchermeta.mojang.com/v1/products/java-runtime/<…>/all.json` | `<endpoint>/v1/products/java-runtime/…` |

## Development

```sh
go build ./...
go test ./...          # unit tests cover rules, inheritance, args, downloads,
                       # native extraction, encryption, config, store, java matching
gofmt -l .             # must stay empty
./mcli --lang en help  # bilingual surface can be checked from the CLI
```

The i18n test suite fails if any `i18n.T("key")` in the repository has no entry
in the catalogs, or if the English and Chinese catalogs drift apart, so
translations cannot silently rot.

## License

No license file yet — ask the maintainer before redistributing.