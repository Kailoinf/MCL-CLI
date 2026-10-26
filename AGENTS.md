# AGENTS.md — working on mcli

Everything here is a rule or a verified fact. If you change behaviour, update the
file in the same commit.

## Project map

```
main.go            dispatch, help, global flags        flags.go     argv parser
app.go             shared context: paths, db, logger, confirm, auth helpers
commands.go        login/logout/list/create/delete/refresh/config/java
launch_cmd.go      launch command + java selection + placeholder drift check
internal/i18n      every user-visible string (en + zh catalogs)
internal/paths     ~/.mcl-cli layout
internal/config    the config table inside mcli.db (keys + defaults + validation)
internal/store     mcli.db: config, accounts, instances, manifest cache
internal/secret    AES-GCM token encryption, PBKDF2 from machine identity
internal/logx      leveled terminal + file logging
internal/mirror    official URL -> ordered BMCLAPI/official candidate list
internal/dl        downloader: retries, stall watchdog, SHA-1, Y/N confirm, worker pool
internal/mcv       version manifest, version JSON + inheritance, rules, args, install, natives
internal/java      runtime discovery, major matching, runtime installation
internal/launch    command line assembly + process start
docs/              architecture + design decisions (keep in sync with code)
```

## Hard rules

1. **No hardcoded user-visible text.** Every string goes through
   `i18n.T("key")`, with the key present in *both* `internal/i18n/en.go` and
   `internal/i18n/zh.go`. The test `TestSourceKeysExist` scans the repository and
   fails on a missing key; `TestCatalogsInSync` fails when the catalogs drift
   apart. Run `go test ./...` before every commit.
2. **Zero third-party dependencies.** The build must stay `go build` with the
   standard library only. Adding a dependency needs the maintainer's explicit
   approval first.
3. **Never print secrets.** Show paths, lengths or commands instead of token
   values.
4. **Keep `gofmt -l .` empty** and `go vet ./...` clean.
5. **commit + push often** (small, working increments) — do not batch.
6. Docs are part of the deliverable: README.md, README.zh-CN.md, AGENTS.md and
   `docs/*` must match the code you just wrote.

## Adding a command

1. Add usage/description keys (`cli.cmd.<name>.usage`, `.desc`) to both catalogs.
2. Add it to the `cmds` table in `printHelp` (main.go) and to the `switch` in
   `run`.
3. Implement `func (a *App) cmdX(args []string) int` with
   `parseFlags("x", args, map[string]flagSpec{...})`; unknown options must stay
   errors so typos cannot silently change behaviour.
4. Return `a.fatal(err)` (exit 1) or `a.usageError("cli.missing_arg", …)`
   (exit 2). Print results with `a.Log.Printf`, diagnostics with `a.Log.Infof`….
5. Tests: cover the parsing/decision logic, not the network.

## Verified environment facts (2026-10, checked with curl from this machine)

* `https://bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json` may answer
  **302 to a `*.sakura.sld.tw` node that is often slow or stalls**; the official
  `piston-meta.mojang.com` copy is the reliable fallback. This is why every
  download is a candidate list and `dl` has a stall watchdog (20 s without
  bytes cancels the attempt and moves to the next candidate).
* BMCLAPI serves `/version/<id>/json` and `/version/<id>/client`, mirrors
  `piston-meta`/`piston-data` paths verbatim, maven libraries at `/maven/<path>`
  and asset objects at `/assets/<first2>/<hash>`. There is **no**
  `/assets/indexes/<id>.json`; use `/v1/packages/<sha1>/<id>.json` (works).
  `/java/list` returns 403 — do not build on it.
* Mojang's java runtime product manifest
  (`launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96…/all.json`) is
  mirrored by BMCLAPI, is keyed by platform (`linux`, `linux-i386`,
  `windows-x64`, `mac-os`, `mac-os-arm64`, …) then component, and each component
  manifest lists `files` with `type: file|directory|link`, `executable` and
  `downloads.raw` (sha1/size/url on piston-data). Components seen:
  `jre-legacy`=8, `java-runtime-alpha`=16, `-beta`/`-gamma`=17, `-delta`=21,
  `-epsilon`=25.
* Version JSON: modern documents carry `arguments.jvm`/`arguments.game` (string
  or `{rules, value}`), `javaVersion.{component,majorVersion}`, natives as
  separate libraries named `…:natives-linux`; old ones carry
  `minecraftArguments` plus `natives` maps with `downloads.classifiers`. Both
  paths must keep working.
* The current release line uses the new scheme (`26.3`, snapshot
  `26.4-snapshot-5`) and needs **Java 25**; `javaVersion.majorVersion` is
  authoritative, the id-based table is only a fallback.
* Xbox Live chain endpoints that must stay reachable:
  `login.microsoftonline.com/consumers/oauth2/v2.0/{devicecode,token}`,
  `user.auth.xboxlive.com/user/authenticate`,
  `xsts.auth.xboxlive.com/xsts/authorize`,
  `api.minecraftservices.com/authentication/login_with_xbox` and
  `/minecraft/profile`. Client id `00000000402b5328`, scope
  `service::user.auth.xboxlive::MBI_SSL offline_access`.
  XSTS error codes worth translating: 2148916233 (no Xbox profile),
  2148916235 (region), 2148916238 (child account).

## Testing notes

* `internal/dl` tests spin up `httptest` servers: a candidate that returns 500,
  a flaky candidate that recovers, and a wrong-SHA-1 candidate. Keep them
  dependency-free and offline.
* `mcv` tests build synthetic version JSONs and zip fixtures; do not add tests
  that hit the network — network behaviour is checked manually with
  `mcli launch <instance> --dry-run` and by a real install.
* Useful manual smoke test with isolated state:

  ```sh
  export MCLI_HOME=/tmp/mcli-home MCLI_MINECRAFT=/tmp/mcli-mc
  ./mcli list --available --limit 5
  ./mcli create demo --version 1.21.4
  ./mcli launch demo --dry-run --yes
  ```

* `launch --dry-run` prints the full java command line; use it to sanity check
  classpath order, `-Djava.library.path`, the logging argument (must come before
  `-cp`) and the game arguments.