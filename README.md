# mcli — Minecraft Launcher CLI

A pure command line launcher for **Minecraft: Java Edition**, written in Go with
**zero third-party dependencies**. Microsoft accounts only, no GUI, no
interactive wizard: everything is a flag.

* Chinese docs: [README.zh-CN.md](README.zh-CN.md)
* Rules for working on the code: [AGENTS.md](AGENTS.md)
* What is verified right now, and what is left: [docs/status.md](docs/status.md)
* Architecture and decisions: [docs/architecture.md](docs/architecture.md),
  [docs/design-decisions.md](docs/design-decisions.md)

## Features

| | |
|---|---|
| Login | Microsoft device-code flow, refresh tokens stored AES-256-GCM encrypted with a key derived from this machine |
| Downloads | BMCLAPI mirror first, automatic fallback to the official Mojang source, SHA-1 verification, retries, large-download confirmation |
| Versions | release / snapshot / old_beta / old_alpha, manifest cache with TTL, `inheritsFrom` merging, legacy `minecraftArguments` support |
| Java | finds local runtimes (PATH, `JAVA_HOME`, distro dirs, `~/.minecraft/runtime`, mcli's own `jdk/`), matches the major version a version needs, can install a runtime from Mojang's mirrored product manifest |
| Instances | multiple named instances, each with its own game directory, JVM args and memory settings; libraries/assets/versions are shared; edit, clone and rename them later with `mcli instance`, and adopt a `.minecraft` that another launcher already filled (`instance scan` / `instance import`) |
| Accounts | several Microsoft accounts at once: `mcli account list` shows them, `use` switches, `launch --account` overrides per run; refresh token and Minecraft token live in two separately encrypted files |
| Interface | English and Simplified Chinese, `--lang auto\|en\|zh`; tables pad by display width so a CJK path does not shift the columns |
| Logs | concise terminal output, full log file under `~/.mcl-cli/logs/` |

Not implemented on purpose: installing Fabric/Forge/Quilt, mod or resource-pack
management, offline accounts, GUI. Running an existing modded profile (as
installed by PCL, HMCL or the official launcher) *is* supported.

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
mcli account list                            # who is logged in, and which account is active
mcli list --available --limit 10             # what can be installed
mcli create vanilla --version latest         # isolated instance (latest | latest-snapshot | a version id)
mcli launch vanilla                          # installs what is missing, then starts the game
```

Already have a `.minecraft` (the official launcher, PCL, HMCL)? Adopt it:

```sh
mcli instance scan                           # every versions/<id> on disk, registered or not
mcli instance import "26.3-Fabric 0.20.1"    # take one over, files and saves stay put
mcli launch "26.3-Fabric 0.20.1"             # runs what is there; mcli never installs loaders
```

Everyday use:

```sh
mcli launch vanilla --xmx 4G --width 1920 --height 1080 --dry-run   # see the exe command line
mcli launch vanilla --xmx 4G --xms 1G        # one-off overrides
mcli launch --last                            # replay the previous launch (bare `mcli launch` does the same)
mcli launch 1.21.4                            # a version id works too: the instance is created for you
mcli list                                     # instances
mcli instance set vanilla --xmx 6G            # change settings for good
mcli instance show vanilla                    # what this instance will actually run with
mcli config set bmclapi_endpoint https://bmclapi2.bangbang93.com
mcli java which 1.21.4                        # which Java this version needs / will use
```

## Commands

| Command | Purpose |
|---|---|
| `mcli login [--flow browser\|device] [--label <name>] [--no-browser] [--no-switch] [--timeout <sec>]` | Microsoft login (browser flow by default); `--no-switch` adds an account without making it the default |
| `mcli logout [<account>] [--all]` | remove stored credentials (identity and both token files) |
| `mcli account [list \| use <name\|uuid\|label> \| remove <name>]` | show and switch the logged-in accounts |
| `mcli list [--instances] [--accounts] [--available] [--type <t>] [--search <text>] [--limit N]` | instances, accounts or downloadable versions |
| `mcli create <name> --version <v>|latest|latest-snapshot [--loader vanilla\|fabric] [--loader-version <v>] [--java <path>] [--xmx 4G] [--xms 1G] [--jvm-arg ...] [--game-arg ...] [--force]` | create an instance, optionally on a mod loader |
| `mcli instance [list]` | instances with version, java, readiness, last played, game dir |
| `mcli instance show <name>` | everything about one instance |
| `mcli instance set <name> [--version <v>] [--java <path>|auto] [--xmx 4G|auto] [--xms 1G|auto] [--jvm-arg ...] [--game-arg ...] [--clear-jvm-args] [--clear-game-args]` | change an instance's settings |
| `mcli instance copy <src> <dst> [--with-files]` | clone an instance (optionally its saves) |
| `mcli instance rename <old> <new>` | rename an instance and its game directory |
| `mcli instance scan [--all]` | version directories found on disk, with their type, jar, parent and instance |
| `mcli instance import <version> [--as <name>] [--force]` | adopt an existing install (official launcher, PCL, HMCL, a modded profile) |
| `mcli fabric list <game> [--limit N]` | the Fabric loader builds published for a game version |
| `mcli fabric list <game> [--limit N]` | the loader builds Fabric publishes for a game version (installing is the `--loader` choice below) |
| `mcli search <query> [--type mod\|resourcepack\|shader\|datapack\|modpack] [--version <game>] [--loader <loader>] [--limit N] [--offset N]` | search Modrinth (one query, one page) |
| `mcli add <project> [--instance <name>] [--version <game>] [--loader <loader>] [--dry-run]` | install a project's matching file into an instance |
| `mcli mods [<instance>]` | what an instance already has in mods/, resourcepacks/, shaderpacks/ |
| `mcli server create <name> --version <v> [--java <path>] [--xmx 2G] [--xms 512M] [--port 25565] [--dir <path>] [--jvm-arg ...] [--eula]` | register a server: its own directory, world and port; the Minecraft EULA is printed and has to be accepted |
| `mcli server [list \| show <name> \| set <name> [--xmx] [--xms] [--port] [--java] [--version] \| delete <name> [--keep-files] [--yes]]` | inspect and change servers |
| `mcli server start <name> [--dry-run] [--detach] [--eula] [--port N]` | fetch the server jar if needed and start it; attached by default, so the console is yours and Ctrl+C saves and stops |
| `mcli server attach <name> [--lines N]` | follow a detached server's console and type into it (Ctrl+C leaves the server running) |
| `mcli server cmd <name> <command…>` | send one command into a running server's console |
| `mcli server stop <name> [--force]` | ask a detached server to stop: it gets SIGINT, exactly what Ctrl+C would send, so the world is saved |
| `mcli doctor [<name>] [--all] [--deep]` | health check: the machine (home, java, manifest, log) and every file an instance needs; exit 1 when something is wrong |
| `mcli repair <name> [--deep] [--force] [--dry-run]` | fetch exactly the files doctor found missing or damaged (`--force` re-downloads everything planned) |
| `mcli delete <name> [--keep-files] [--yes]` | delete an instance (and its saves) |
| `mcli launch <name>|<version>|--last [--version <v>] [--java <path>] [--xmx] [--xms] [--jvm-arg] [--game-arg] [--width N] [--height N] [--server host[:port]] [--fullscreen] [--demo] [--dry-run] [--wait] [--account <a>]` | install if needed and start |
| `mcli refresh [--versions] [--all]` | force-refresh cached metadata |
| `mcli config [get\|set\|unset\|list\|path]` | read or change settings |
| `mcli java [list \| install <major> [--force] \| which <version>]` | inspect or install Java runtimes (`--force` reinstalls an incomplete one) |
| `mcli version`, `mcli help [command]` | self-explanatory |

Global options accepted anywhere: `--lang <auto\|en\|zh>`, `--config <path>`,
`--yes` (never prompt), `--quiet`, `--verbose`, `--no-color`.

## Configuration

`mcli config list` shows everything with descriptions; `mcli config path` prints
where each file lives. mcli keeps five small TOML documents in `~/.mcl-cli/`:

| File | Contents | Notes |
|---|---|---|
| `mcli.toml` | the config table | hand-editable; `config set` patches one line, so your comments and ordering survive; portable between machines |
| `state.toml` | accounts (identity only), instances, manifest-cache pointer | machine state (0600); carries no secrets, so it is safe to copy |
| `refresh.toml` | the long-lived refresh token per account, plus its key salt | 0600; written at login and when Microsoft rotates the token — back it up like a password |
| `session.toml` | the short-lived Minecraft access token and its expiry | 0600; rewritten on every token refresh, safe to delete (a new one is minted from `refresh.toml`) |
| `last.toml` | the previous launch | deliberately tiny, so `launch --last` never reads anything else |

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
├─ refresh.toml             long-lived refresh tokens per account (0600)
├─ session.toml             short-lived Minecraft tokens + expiry (0600)
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
config is meant to be opened in an editor, the previous launch lives in its own
tiny file, and — most importantly — the credentials never share a file with
something mcli rewrites constantly (see `docs/design-decisions.md`).

## Accounts and security

Only Microsoft accounts are supported, and any number of them at once: log in
again (optionally `--no-switch`), then `mcli account list` shows them all, `use`
picks the default and `mcli launch <instance> --account <name>` overrides it for
one run. `mcli login` opens the Microsoft authorize page (`--flow device`
switches to the device-code flow if your client id supports it); no password ever
reaches mcli.

Tokens are encrypted with AES-256-GCM under a key derived (PBKDF2-SHA256, 200k
iterations) from this machine's identifiers **plus a per-account salt stored with
that account**. The long-lived refresh token lives in `refresh.toml` and the
short-lived Minecraft token in `session.toml`, both 0600: tokens are never in the
same file as the instance list, so the files worth backing up are also the files
that change least. There is no OS keychain, an account entry copied to another
machine cannot be decrypted, and `mcli account list` still works without them.
Everything else (config, instances, last launch) carries no secrets at all and
works on any machine.

## Servers

Server-side installs live next to the client ones but are their own thing: a
server has no account, no assets and no classpath, so `mcli server` downloads one
file and runs it.

```sh
mcli server create survival --version 26.3 --port 25566 --xmx 4G   # prints the EULA URL, asks for Y
mcli server start survival                                          # fetches ~60 MB once, then runs attached
mcli server start survival --detach                                 # or in the background, pid recorded
mcli server attach survival                                         # follow that console, type commands
mcli server stop survival                                           # only needed for --detach runs
```

* The directory is `<minecraft_dir>/servers/<name>` — jar, world, `eula.txt` and
  `server.properties` live there, and nothing else touches them. An existing
  `server.properties` is never rewritten.
* The EULA is never accepted for you: the URL is printed, the question is asked,
  and `--eula` (or a global `--yes`) only skips the question, not the notice.
* `--detach` starts the server in the background and records its pid, so
  `mcli server stop` can signal it later. A detached server writes its console to
  `<dir>/logs/console.log` and its own log to `<dir>/logs/latest.log`.
* Java comes from the same runtime rules as the client: the version's
  `javaVersion.majorVersion`, and mcli installs the runtime when it is missing.
* `server attach` needs a FIFO for the server's stdin (Linux, macOS); elsewhere
  the option is simply reported as unavailable and `server start` runs in the
  foreground instead.

## Fabric

```sh
mcli fabric list latest                                     # loader builds for a game version
mcli create modded --version latest --loader fabric         # newest stable loader
mcli create pinned --version latest --loader fabric --loader-version 0.16.9
mcli instance set modded --loader vanilla                    # and back again
```

The mod loader is a **choice on the version**, not a separate command: `--loader
vanilla|fabric` (with `--loader-version` to pin a build) on `create` and on
`instance set`, which is also how an existing instance becomes moddable or goes
back to vanilla. Fabric publishes its loader as a version JSON that inherits from
the game version (`meta.fabricmc.net`) — exactly the shape mcli reads — so
installing one is fetching a document and writing it where the version loader
looks. After that the instance launches, repairs and diagnoses like any other.
`mcli fabric list <game>` is the discovery half: which loader builds exist.
OptiFine, Forge, Quilt and NeoForge are not supported yet, and the `--loader`
choice says so instead of guessing.

## Mirror behaviour

Every download is expressed as an ordered candidate list: BMCLAPI first (when
configured), the official Mojang URL last. A candidate that fails, stalls or
returns a wrong SHA-1 is abandoned for the next one; the whole list is retried
`download_retry_times` times. When one source fails three times in a row it is
demoted to the end of the list for the rest of the run (it is never dropped, so a
network that can only reach the mirror still installs), and that decision is
logged once. Verified mappings:

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
in the catalogs, if the English and Chinese catalogs drift apart, or if a call
site passes the wrong number of arguments for its translation, so translations
cannot silently rot.

The documentation lives in `docs/` and is a VitePress site:

```sh
pnpm install
pnpm docs:dev      # http://localhost:5173, live reload
pnpm docs:build    # static site in docs/.vitepress/dist
```

`skill/SKILL.md` is the working guide for anyone (or anything) editing mcli:
where things live, how to verify a change without a display, and the traps that
have already been hit. It is updated in the same commit as the feature it
describes.

## License

No license file yet — ask the maintainer before redistributing.