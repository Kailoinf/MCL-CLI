---
name: mcli-launcher
description: Use when working on mcli, the Go Minecraft launcher CLI.
version: 1.1.0
author: Kailo
license: unpublished
platforms: [linux, macos, windows]
metadata:
  hermes:
    tags: [minecraft, go, cli, launcher]
    related_skills: [github, python-debugpy]
---

# mcli (MCL-CLI)

Command-line Minecraft: Java Edition launcher in Go, standard library only
(`github.com/Kailoinf/MCL-CLI`, branch `main`). This file is the copy that ships in
the project at `skill/SKILL.md`; the same text lives wherever the agent reads skills
from, and the two are kept identical.

`AGENTS.md` holds **only** the rules that stay true (the maintainer keeps it lean).
The code map, verified facts, quirks and the ordered plan live in
**`docs/status.md`** — read it first, update it in the same commit, and do not copy
it here. `AGENTS.md` is protected: propose edits, never write them.

## When to Use

- The user mentions mcli, MCL-CLI, or their Minecraft launcher project
- Extending or fixing the CLI: install, launch, instances, accounts, servers, loaders, content
- Verifying a change actually builds, downloads and assembles a command line
- Answering questions about BMCLAPI, Mojang metadata, Modrinth or Fabric endpoints

Don't use for: playing Minecraft, or anything about a launcher other than mcli.

## Prerequisites

- Repo at `/root/mcli`; Go toolchain (`go version`) and `pnpm` for the docs site
- Never touch a real home: point the scratch homes at test fixtures (the paths below
  are the development box's; elsewhere use any scratch directory)
  - `MCLI_HOME=/root/.hermes/cache/scratch/mtest/home`,
    `MCLI_MINECRAFT=/root/.hermes/cache/scratch/mtest/mc` (empty, fast)
  - `MCLI_HOME=/root/.hermes/cache/scratch/mcuser-home`,
    `MCLI_MINECRAFT=/root/.hermes/cache/scratch/mcuser/.minecraft` (a real PCL
    install: `26.3`, `26.3-Fabric 0.20.1`, server `mc-test`)
- No display on this box: the game is never launched, only planned
  (`--dry-run`). A **server** needs no display and is the one thing to verify by
  running it for real.

## How to Run

```sh
cd /root/mcli && gofmt -l . && go vet ./... && go test ./... \
  && ./scripts/check-cross.sh | tail -1 && go build -o mcli .
```

Every one of those is a gate, not a suggestion: `gofmt -l .` empty, vet clean, tests
green, all eight cross targets compiling (the maintainer runs mcli from Git Bash on
Windows, so a Linux-only build is unfinished), binary built. Commit and push in
small steps as each piece passes.

## Quick Reference

| What | Command |
| --- | --- |
| Plan a launch | `./mcli launch <instance> --dry-run --yes` |
| Check without an account | `./mcli doctor --all`, `./mcli repair <instance> --yes` |
| Real server end to end | `./mcli server create/start/stop/attach/cmd` |
| Docs site | `pnpm docs:build` (settings: `pnpm-workspace.yaml`, not package.json) |
| CurseForge | `./mcli search <q> --source curseforge` (needs `config set curseforge_api_key <key>`) |

## Procedure

1. **Read `docs/status.md`** for the current state and the ordered plan; check
   `git log --oneline -5` for where the last step stopped.
2. **Make the change** with the rules below (all user-facing strings in
   `i18n.T("key")`, both catalogs; no third-party modules).
3. **Verify it for real** — a feature is not done because it compiles. Follow
   `references/verification-recipes.md` for the area you touched, against a scratch
   home. A server may be started, stopped and inspected; a client launch is judged
   from the printed command line only. Completion: the evidence is in the transcript,
   not in a promise.
4. **Update the docs in both languages** (README.md + README.zh-CN.md, `docs/<page>.md`
   + `docs/zh/<page>.md`) and `docs/status.md` with what was verified and what broke.
5. **Commit and push**, then update this skill if the change altered how the project
   works. Keep it lean: when you add a rule, delete the wording it replaces.

## Rules that change behaviour

- **No hardcoded user-visible strings.** Everything through `i18n.T("key")` with the
  key in both `internal/i18n/en.go` and `internal/i18n/zh.go`. `go test ./...` fails
  on a missing key, drifting catalogs, or a call passing the wrong number of verbs.
- **Zero third-party modules**, TOML and SQLite included. Ask before adding any.
- **Never print credential values** — show paths, lengths or counts. The decision is
  made in code, not by habit: a config field tagged `secret:"1"` is reported by
  `config.Secret(key)` as "set (N characters)", so no read path can leak it by
  accident.
- **Tables pad by display width**, not bytes (`table.go`), or a CJK path shifts
  every following column.
- **One on-disk state shape, no migration.** mcli has no users on an older layout:
  never add a compatibility shim. The store is **not a database** — no `.db`, no
  `DBVersion`, no `LegacyDB`; it is `store.Store`, key `state_version`, five TOML
  documents under `~/.mcl-cli/` (`mcli.toml`, `state.toml`, `refresh.toml`,
  `session.toml`, `last.toml`), parsed by `internal/toml`.
- **Platform code lives in its own file behind a build tag** (`mem_{linux,darwin,
  windows,other}.go`, `disk_{unix,windows}.go`, `console_{unix,windows}.go`,
  `server_signals_*.go`), never an `if runtime.GOOS ==` inside shared logic. Make the
  platform-shaped parts injectable so they can be tested from Linux
  (`launch.Options.PathSeparator` is the model). Where a feature cannot work, refuse
  with the alternative named in both languages, not a low-level error.
- **`--yes` suppresses every prompt.** A new confirmation goes through
  `confirmFile`/`confirmBatch` or a guard on `a.Yeseverything`; a raw prompt hangs
  non-interactive runs. Anything that prints one line per file must be capped.
- **Argument values from a version JSON pass through verbatim**, even when they look
  wrong: the Fabric profile's `-DFabricMcEmu= net.minecraft.client.main.Main ` really
  carries a leading and a trailing space. Enable only the JSON features you need
  (`is_quick_play_multiplayer` alone, not `has_quick_plays_support`).
- **A launcher owns the console, it does not go around it.** Stop a server by writing
  its own `stop` into its console and waiting for the save; a signal does not save a
  headless JVM's world. Record a child's pid the moment it exists, as an attached run
  waits for as long as the server lives. Hold console input until the server reports
  readiness (`WaitReady(dir, since, timeout)`), and never open the console pipe before
  the server holds the other end.
- **Ask the version JSON what an instance is** (`Version.GameVersion()`,
  `Version.Loader()`), never its name or id: another launcher writes its own id, keeps
  the game version in `clientVersion` and names its loader only through libraries.
- **`doctor` reports, `repair` owns only what mcli fetches.** Every installed thing
  gets checked — a version's files, a server's jar against the published checksum,
  its EULA, its port, its last log — and `repair` puts back exactly those files. For a
  server that is the jar: `eula.txt`, `server.properties` and the world belong to the
  person running it and are never written.
- **Docs ship in both languages in the same commit**, and `docs/status.md` says what
  was *run* versus merely compiled.

## Pitfalls

- Never test against a real `.minecraft`, and never launch the game here (no display).
  Verify from `--dry-run` output: classpath ending with the client jar, log4j before
  `-cp`, natives per version, `--gameDir` on `versions/<instance>`.
- Never test `mcli login` interactively or ask for credentials; the flow can be smoke
  tested up to the printed URL/device code and no further.
- Long downloads (~560 MB for a modern install) belong in a background process with
  `notify`, read back from a log file — never piped through a pager or a `tail`.
- A detached child must not inherit mcli's stdout, or the caller waits for a process
  that never ends and it looks like a hang.
- Do not "fix" a pack, profile or instance by rewriting its paths or ids: refuse and
  say what to run instead (a mismatch that is stated is worth more than a guess).
- Reinstalling the binary does not clean `~/.mcl-cli`, so an older layout survives: an
  unknown-key warning from `refresh.toml`/`session.toml` means that login cannot be
  read. Never migrate it — print what was skipped, say the login is unusable, and name
  `mcli login` (or deleting the file) as the way out.
- `mcli list` lists registered instances, not the contents of the game directory;
  pointing `minecraft_dir` at a full `.minecraft` registers nothing. When the list is
  empty, in both the folder-is-populated case and the truly-empty case, say which is
  which and name `mcli instance scan` / `import`.
- CurseForge needs an API key belonging to the user; without one, name the exact
  command that sets it. A refused key (401/403) is `ErrKeyRejected`, not a network
  error — and the live success path cannot be verified here, so say so instead of
  implying otherwise.
- The EULA is printed, never assumed: `--eula`/`--yes` skip the question, not the
  notice. Accepting it writes `eula.txt` outright (`mcserver.AcceptEula`) — going
  through `WriteFiles` silently did nothing when the file already said false.

## Verification

- Build gates: the `How to Run` line, plus `./scripts/check-cross.sh` for every claim
  about platforms.
- Feature recipes (launch, doctor/repair, servers and their console, Fabric, Modrinth
  content, packs and pack updates, instance moves, Windows-shaped arguments) are in
  **`references/verification-recipes.md`**; the endpoint facts behind them are in
  **`references/service-facts.md`**.
- Fixtures to reuse: the scratch homes above, the PCL install, the `mc-test` server
  (port 25566), and `fabulously-optimized` as a real modpack.
