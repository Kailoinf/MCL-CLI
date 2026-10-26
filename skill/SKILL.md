---
name: mcli-launcher
description: Use when working on mcli, the Go Minecraft launcher CLI.
version: 1.0.0
author: Kailo
license: unpublished
metadata:
  hermes:
    tags: [minecraft, go, cli, launcher]
    related_skills: [github, python-debugpy]
---

# mcli (MCL-CLI)

Command line Minecraft: Java Edition launcher, Go + standard library only.
`mcli` lives at `github.com/Kailoinf/MCL-CLI` (branch `main`); this file is the
copy that ships with the project, at `skill/SKILL.md`. The same skill is
installed where an agent reads its skills from, and the two are kept identical
apart from machine-specific paths.
mcli.md is the original requirements doc; `AGENTS.md` holds **only** the rules
that stay true (the maintainer keeps it lean on purpose — no code map, no
recipes). The code map, the working conventions, what was verified with real
commands, service facts, quirks, loose ends and the planned work all live in
**`docs/status.md`**: read it first, and update it in the same commit.

AGENTS.md is protected: propose edits, do not write them.

## Keeping this skill current

**Adding or changing a feature means updating this skill in the same commit** —
that is a standing instruction from the maintainer. Concretely: a new command
(and its flags) belongs in the layout and verification lists below, a new
pitfall or service fact belongs in Pitfalls/BMCLAPI facts, and a change of scope
(the roadmap moving) belongs in "Where the project is going". Walk the file the
way you would walk the README: if a user following this skill would be
surprised by the code, the skill is stale.

## When to Use

* the user mentions mcli, MCL-CLI, or the Minecraft launcher project
* extending/fixing the CLI, downloads, Microsoft auth, Java matching or launch
* verifying that a change actually builds, downloads and assembles a launch
* answering questions about BMCLAPI endpoints or Mojang's launcher metadata

## The docs site

`docs/` is a VitePress site (`pnpm docs:dev` / `pnpm docs:build`, output in
`docs/.vitepress/dist`); `docs/status.md` is published as-is, so keep it readable
by people, not only by agents.

**Every page exists in both languages**: English at `docs/<page>.md`, Chinese at
`docs/zh/<page>.md`, and the site's locale switcher lands on the pair. A change to
one language is a change to both, in the same commit — that is AGENTS.md rule 5 and
the maintainer asks for it explicitly. Before committing a docs change, diff the
heading lists of the two files (`grep '^#' docs/<page>.md docs/zh/<page>.md`): equal
headings means nothing was dropped in translation. CurseForge-grade facts (commands,
paths, byte counts, commit hashes) stay verbatim in both.

## Non-negotiables

* **No hardcoded user-visible strings.** Everything through `i18n.T("key")` with
  the key present in both `internal/i18n/en.go` and `internal/i18n/zh.go`.
  `go test ./...` fails (`TestSourceKeysExist`, `TestCatalogsInSync`,
  `TestFormatVerbsMatch`) when a key is missing, the catalogs drift, or the zh
  string uses different printf verbs — verb *order* counts, not just the set.
* Zero third-party modules. `go build` with stdlib only — ask the user before
  adding any dependency, TOML and SQLite included.
* `gofmt -l .` empty, `go vet ./...` clean, small commits pushed as you go.
* Docs are part of the change: README.md + README.zh-CN.md + `docs/*` (and
  AGENTS.md only with approval) must match the code in the same commit.
* Never print credential values; show paths, lengths or counts.
* Tables must pad by display width, not bytes (`table.go`): a CJK path or a
  Chinese status word otherwise shifts every following column.

## Layout

The CLI lives in `main.go`/`flags.go` (dispatch, argv parser), `commands.go`
(login/logout/list/create/delete/refresh/config/java + version aliases),
`instance_cmd.go` (instance list/show/set/copy/rename), `instance_import.go`
(instance scan/import + name validation), `account_cmd.go` (account
list/use/remove), `doctor_cmd.go` (doctor = report, repair = fetch exactly what
doctor found; both read `mcv.Plan`, so they cannot disagree), `server_cmd.go` +
`internal/mcserver` (server-side installs: one jar, one directory, one world),
`loaders.go` (the mod loader as a *choice*: `--loader vanilla|fabric` on create and
instance set), `fabric_cmd.go` + `internal/fabric` (discovery: the loader builds
`meta.fabricmc.net` publishes), `mods_cmd.go` + `internal/modrinth` (search and
install content),
`launch_cmd.go`
(launch + java selection + the per-run `--server`/`--fullscreen`/`--demo`
switches), `app.go` (shared
context), `table.go`; internals under `internal/{i18n,paths,config,store,secret,
logx,mirror,dl,mcv,java,launch,auth,mcserver}`.

State: `~/.mcl-cli/` holds **five** TOML files — `mcli.toml` (config,
hand-editable, patched a line at a time), `state.toml` (accounts *identity only*,
instances, manifest pointer; no secrets, so it is portable), `refresh.toml`
(long-lived refresh tokens + their salts, 0600), `session.toml` (short-lived
Minecraft tokens + expiry, 0600), `last.toml` (previous launch, tiny so
`launch --last` reads nothing else), plus `cache/`, `jdk/`, `logs/`.

`state_version` is 1 and there is **no migration code, on purpose**: mcli has no
users on an older shape, so only one shape exists and an older file is simply
written over by the next save. Never add a compatibility shim or a fallback path
for a previous layout, and never resurrect the old naming: the store is **not a
database** — no `.db` file, no `DBVersion`, no `LegacyDB`. The on-disk key is
`state_version`, the Go constant `store.StateVersion`, the type `store.Store`.
TOML is read/written by `internal/toml` — never add a TOML dependency.
`store.Store` also holds the manifest cache pointer and the unknown-key report
that warns about typos instead of swallowing them.

Game root `~/.minecraft` with `libraries/`, `assets/`, `versions/<version>/`
(files) and `versions/<instance>/` (game dir per instance).
`MCLI_HOME`/`MCLI_MINECRAFT` override both.

## Login flows (verified 2026-10-26)

* Default `--flow browser`: authorize at
  `https://login.live.com/oauth20_authorize.srf` (`response_type=code`, scope
  `XboxLive.signin offline_access`, `prompt=select_account`), loopback server on
  ports 29116-29126 answering `/authentication-response`, exchange at
  `https://login.live.com/oauth20_token.srf`. No secret, no PKCE. Refresh also
  goes to login.live.com and sends **no scope**.
* `--flow device`: `login.microsoftonline.com/consumers/oauth2/v2.0/devicecode`
  with scope `service::user.auth.xboxlive::MBI_SSL offline_access`.
* The reference launcher's client id `288ec5dd-6736-4d4b-9b96-30e083a8cad2`
  (from its release jar manifest) is accepted but **not enabled for device
  codes** (`AADSTS9002346`); `00000000402b5328` no longer exists
  (`AADSTS700016`). `auth_client_id` is configurable for that reason.
* Smoke-test the flow with real network but no account: run
  `./mcli login --no-browser --timeout 25`, read the redirect port from the
  printed URL, then `curl "http://localhost:<port>/authentication-response?code=FAKE"`
  — the CLI should render the callback page and then fail with Microsoft's
  `invalid_grant` for the fake code, which proves the wiring.

## Verified BMCLAPI facts (2026-10)

* Manifest may answer **302 to a `*.sakura.sld.tw` node that stalls**; keep the
  official `piston-meta.mojang.com` copy as fallback and never remove the stall
  watchdog in `dl` (`newStallReader`, 20 s).
* Mirror paths that work: `/mc/game/version_manifest_v2.json`,
  `/version/<id>/json`, `/version/<id>/client`, `/maven/<maven path>`,
  `/assets/<first2>/<hash>`, `/v1/packages/<sha1>/<file>.json`,
  `/v1/products/java-runtime/<hash>/all.json`. `/assets/indexes/<id>.json` is
  **404** and `/java/list` is **403** — do not build on either.
* Version JSONs: modern = `arguments.jvm`/`arguments.game` (string or
  `{rules,value}`), natives as separate `…:natives-linux` libraries; legacy =
  `minecraftArguments` + `natives` map + `downloads.classifiers`. Both paths are
  covered by tests; keep them working.
* Java: Mojang's java-runtime product manifest is the source of runtimes
  (`javaVersion.component` names it). Components: `jre-legacy`=8,
  `java-runtime-alpha`=16, `-beta`/`-gamma`=17, `-delta`=21, `-epsilon`=25.
  Current releases (`26.x`) need Java 25; `javaVersion.majorVersion` wins over
  the id-based table.
* Auth endpoints: `login.microsoftonline.com/consumers/oauth2/v2.0/{devicecode,token}`,
  `user.auth.xboxlive.com/user/authenticate`, `xsts.auth.xboxlive.com/xsts/authorize`,
  `api.minecraftservices.com/authentication/login_with_xbox`, `/minecraft/profile`.
  Client id `00000000402b5328`, scope `service::user.auth.xboxlive.com::MBI_SSL offline_access`.

## Verification recipe

```sh
cd /root/mcli && gofmt -l . && go vet ./... && go test ./... && go build -o mcli .
export MCLI_HOME=/root/.hermes/cache/scratch/mtest/home MCLI_MINECRAFT=/root/.hermes/cache/scratch/mtest/mc
export LANG=en_US.UTF-8   # exercise the English catalog; drop it for Chinese
./mcli version && ./mcli help && ./mcli config list
./mcli list --available --limit 5
./mcli create demo --version 1.21.4 && ./mcli create old --version 1.7.10
./mcli launch demo --dry-run --yes    # prints the exact java command line
./mcli launch old  --dry-run --yes    # legacy minecraftArguments path
```

Check in the dry-run output: classpath ends with the client jar, the log4j
argument sits **before** `-cp`, natives dir per version, `--gameDir` points at
`versions/<instance>`, memory flags first.

Health checks without needing an account (both work on a scratch home):

```sh
./mcli doctor --all                  # machine + every instance; exit 1 when broken
./mcli doctor "<instance>" --deep    # also verifies asset SHA-1s, not just sizes
./mcli repair "<instance>" --dry-run # exactly the files repair would fetch
./mcli repair "<instance>" --yes     # fetch them — this is how you install a
                                     # version without logging in
```

Per-run game switches, verified on both argument generations (2026-10-26):

```sh
./mcli launch <profile> --server example.com --dry-run --yes
#   modern: --quickPlayMultiplayer example.com:25565 (no --quickPlayPath)
./mcli launch <legacy> --server play.example.net:25566 --demo --fullscreen --dry-run --yes
#   legacy: --server play.example.net --port 25566 --demo --fullscreen
```

Use the PCL fixture when a real modded install is needed:
`MCLI_HOME=/root/.hermes/cache/scratch/mcuser-home`,
`MCLI_MINECRAFT=/root/.hermes/cache/scratch/mcuser/.minecraft` (it holds
`26.3-Fabric 0.20.1`, the Fabric profile a real launcher installed, plus `26.3`).

A server is the one thing that can be verified end to end on a headless box — run
one for real:

```sh
./mcli server create mc-test --version 26.3 --port 25566 --eula   # prints the EULA URL
./mcli server start mc-test --detach                              # fetches the jar once
sleep 25 && grep 'Done (' "$MCLI_MINECRAFT/servers/mc-test/logs/latest.log"
./mcli server list && ./mcli server stop mc-test                  # SIGINT, world saved
```

Expect: `start --detach` returns in well under a second with a real pid, the log
reaches `Done (Ns)!`, the port in `server.properties` is listening, and
`world/level.dat` is rewritten at stop time (that is the proof the shutdown was
graceful, not killed).

Both console modes, and the attach that connects to a detached server:

```sh
# foreground console: stdin is the server's console, "stop" saves and exits
( sleep 12; printf 'list\n'; sleep 8; printf 'stop\n' ) | ./mcli server start <name>
./mcli server attach <name>          # follow output, type commands; Ctrl+C only leaves
./mcli server cmd <name> say hi      # one command, no session
```

A detached server reads its commands from a FIFO (`<dir>/console.in`) and writes
its output to `<dir>/logs/console.log`; `attach` does the two ends. Feeding stdin
from a pipe works, but the pipe must stay open until the server has started —
commands written before the console thread exists are lost.

Fabric, verified against the loader PCL installed on the same machine:

```sh
./mcli fabric list 26.3                                        # -> 0.20.1 stable, 255 builds listed
./mcli create fab --version 26.3 --loader fabric               # writes versions/fabric-loader-0.20.1-26.3/
./mcli instance set fab --loader vanilla                        # and back again
./mcli launch fab --dry-run --yes                               # classpath with fabric-loader, KnotClient, -DFabricMcEmu verbatim
```

The classpath must contain `net/fabricmc/fabric-loader`, `sponge-mixin` and the
`org/ow2/asm` jars: Fabric's profile lists libraries as `name` + `url` + `sha1`
with **no** `downloads` block, so a launcher that only reads `downloads.artifact`
drops the whole loader and the game dies looking for its main class.

Content from Modrinth, verified against the live API:

```sh
./mcli search sodium --version 1.21.4 --loader fabric     # one query, one page
./mcli add sodium --instance <fabric instance> --yes      # downloads the matching file
./mcli add sodium --instance <fabric instance> --yes      # second run: already intact
./mcli mods <fabric instance>                             # what is installed
./mcli add sodium --instance <vanilla instance>           # must refuse and name --loader fabric
```

Real end-to-end install (downloads ~55 MB plus a Java runtime):
`./mcli create tiny --version rd-132211 && ./mcli launch tiny --yes` — the very
first public version is the smallest (client 0 MB, assets ~50 MB).

## Pitfalls

* Big outputs: never pipe a long-running download through a pager or
  `tail`-style filter in the same command — you see nothing until it ends. Log
  to a file and grep it with `search_files`/`read_file`. Anything that prints one
  line per file (a plan can name thousands) must be capped, not dumped.
* `--yes` must suppress *every* prompt; if you add a new confirmation, call
  `App.confirmFile`/`confirmBatch` or guard on `a.Yeseverything` — a raw
  `a.prompt` hangs in non-interactive runs.
* Login needs a real Microsoft account; never test `mcli login` interactively
  and never ask the user for credentials. `auth` can be smoke tested only up to
  the device-code step (a printed URL + code).
* The maintainer explicitly does **not** want the game launched on this box (no
  display): judge launches from `launch --dry-run --yes` output and report what
  the command line actually contains. A server needs no display — that one may
  be started for real once `mcli server` exists.
* Never test against a real home: point `MCLI_HOME`/`MCLI_MINECRAFT` at a scratch
  directory and work on a copy rather than a user's `.minecraft`.
* A version JSON value passes through **verbatim**, even when it looks wrong: the
  Fabric profile's `-DFabricMcEmu= net.minecraft.client.main.Main ` really carries
  a leading and a trailing space. Do not "clean" argument values.
* When enabling a version JSON feature to unlock arguments, enable only what is
  needed: `has_quick_plays_support` also drags in `--quickPlayPath`, which mcli
  would then hand over empty. Prefer `is_quick_play_multiplayer` alone.
* A detached server must never inherit mcli's stdout: an open pipe keeps the
  caller waiting for a process that never ends (it looks like a hang). Detached
  children write to the server's `logs/console.log` and get their own session.
* The EULA is asked for, never assumed: print `https://aka.ms/MinecraftEULA` and
  require Y. `--eula`/`--yes` skip the question, not the notice.
* Stop a server with SIGINT, never `kill -9` — the world is saved on the way out.
* A network-heavy step (a full modern install is ~560 MB) belongs in a
  background process with `notify`; poll or read its log file rather than
  blocking a foreground call for minutes.

## Where the project is going

`docs/status.md` carries the ordered plan. Server-side launch and Fabric loader
installation are done (`mcli server create/list/show/set/delete/start/stop/attach/cmd`,
`mcli create|instance set --loader vanilla|fabric`, `mcli fabric list`); what is left is
mod/resource-pack search (Modrinth/CurseForge, mirroring what PCL and Console
Minecraft Launcher do — keep the search itself thin; installing a hit into the
right instance is the real work).
