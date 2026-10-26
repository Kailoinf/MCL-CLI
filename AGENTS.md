# AGENTS.md — working on mcli

This file holds what does not change: the rules, the code map and the
conventions. Anything that changes every session — the current version, what was
verified with real commands, service facts with dates, quirks, loose ends — lives
in **`docs/status.md`**. Read that first and update it in the same commit as the
work.

## Project map

```
main.go            dispatch, help, global flags        flags.go     argv parser
app.go             shared context: paths, db, logger, confirm, auth helpers
commands.go        login/logout/list/create/delete/refresh/config/java + version aliases
instance_cmd.go    instance list/show/set/copy/rename
instance_import.go instance scan/import (adopting other launchers' installs) + name rules
account_cmd.go     account list/use/remove
launch_cmd.go      launch command + java selection + placeholder drift check
table.go           display-width padding for CJK-aware tables
internal/i18n      every user-visible string (en + zh catalogs)
internal/paths     ~/.mcl-cli layout (five TOML files, cache, jdk, logs)
internal/toml      dependency-free TOML writer/reader + comment-preserving edits
internal/config    the config table inside mcli.toml (keys + defaults + validation)
internal/store     mcli.toml + state.toml + refresh.toml + session.toml + last.toml
internal/secret    AES-GCM token encryption, PBKDF2 from machine identity + per-account salt
internal/logx      leveled terminal + file logging
internal/mirror    official URL -> ordered BMCLAPI/official candidate list
internal/dl        downloader: retries, stall watchdog, source health, SHA-1, Y/N, pool
internal/mcv       version manifest, version JSON + inheritance, rules, args, install, natives
internal/java      runtime discovery, major matching, runtime installation
internal/auth      Microsoft login: browser (authorization code + loopback) and device code
internal/launch    command line assembly + process start
docs/              architecture.md, design-decisions.md, status.md (live state)
```

## Hard rules

1. **No hardcoded user-visible text.** Every string goes through
   `i18n.T("key")`, with the key present in *both* `internal/i18n/en.go` and
   `internal/i18n/zh.go`. `TestSourceKeysExist` fails on a key used but not
   defined, `TestCatalogsInSync` when the catalogs drift apart, and
   `TestFormatVerbsMatch` when a translation uses different printf verbs (their
   order counts, not just the set). Run `go test ./...` before every commit.
2. **Zero third-party dependencies.** The build must stay `go build` with the
   standard library only. Adding a dependency needs the maintainer's explicit
   approval first.
3. **Never print secrets.** Show paths, lengths or counts instead of token
   values.
4. **Keep `gofmt -l .` empty** and `go vet ./...` clean.
5. **commit + push often** (small, working increments) — do not batch.
6. Docs are part of the deliverable: README.md, README.zh-CN.md and `docs/*`
   must match the code you just wrote. **This file is protected**: the maintainer
   approves edits to it, and it must stay free of state and dated facts.
7. Tables pad by display width (`table.go`), never `% -20s`: a CJK path or a
   Chinese status word would otherwise shift every following column.

## Adding a command

1. Add usage/description keys (`cli.cmd.<name>.usage`, `.desc`) to both catalogs.
2. Add it to the `cmds` table in `printHelp` (main.go) and to the `switch` in
   `run`.
3. Implement `func (a *App) cmdX(args []string) int` with
   `parseFlags("x", args, map[string]flagSpec{...})`; unknown options must stay
   errors so typos cannot silently change behaviour. Reject extra positionals
   with `cli.unexpected_arg` — never ignore them silently.
4. Return `a.fatal(err)` (exit 1) or `a.usageError("cli.missing_arg", …)`
   (exit 2). Print results with `a.Log.Printf`, diagnostics with `a.Log.Infof`….
5. Sub-commands (`mcli instance set`) dispatch on `args[0]` and then call
   `parseFlags` on the remainder; `-h/--help` is handled by the parser.
6. Tests: cover the parsing and decision logic, never the network.

## Testing conventions

* Unit tests stay offline and dependency-free: `httptest` servers simulate
  candidate failures (`internal/dl`), synthetic version JSONs and zip fixtures
  stand in for real versions (`internal/mcv`), temp directories stand in for a
  home (`internal/store`).
* Never point a manual run at the real `~/.mcl-cli`: export `MCLI_HOME` and
  `MCLI_MINECRAFT` into a scratch directory instead. Never write into a user's
  `.minecraft` while testing — copy it first.
* `mcli launch <instance> --dry-run --yes` prints the exact java command line and
  is the main way to judge launch correctness on a machine without a display.
* Anything that needs the network, a browser or a display is verified by hand;
  `docs/status.md` records which of those have been done and how.