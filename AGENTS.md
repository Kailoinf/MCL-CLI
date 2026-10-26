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
internal/paths     ~/.mcl-cli layout (three TOML files, cache, jdk, logs)
internal/toml      dependency-free TOML writer/reader + comment-preserving edits
internal/config    the config table inside mcli.toml (keys + defaults + validation)
internal/store     mcli.toml + state.toml + last.toml (config, accounts, instances, previous launch)
internal/secret    AES-GCM token encryption, PBKDF2 from machine identity + per-account salt
internal/logx      leveled terminal + file logging
internal/mirror    official URL -> ordered BMCLAPI/official candidate list
internal/dl        downloader: retries, stall watchdog, SHA-1, Y/N confirm, worker pool
internal/mcv       version manifest, version JSON + inheritance, rules, args, install, natives
internal/java      runtime discovery, major matching, runtime installation
internal/auth      Microsoft login: browser (authorization code + loopback) and device code
internal/launch    command line assembly + process start
docs/              architecture + design decisions (keep in sync with code)
```

## Where the project stands (2026-10-26, handoff note)

Version 0.1.0. Remote `github.com/Kailoinf/MCL-CLI` is up to date with `main`
(commits `44fbe15` then `c1929706`). `gofmt -l .`, `go vet ./...`,
`go test ./...` and `go build -o mcli .` are all green.

**Verified with real commands, not assumptions**

* version manifest pulled from BMCLAPI (920 versions, TTL-cached in `cache/`);
  version JSONs for 1.21.4 and 1.7.10 downloaded from the mirror.
* `launch --dry-run` produces a correct full command line for a modern version
  (61 classpath entries, client jar last, log4j argument before `-cp`) and for
  the legacy format (1.7.10 `minecraftArguments`, RubyDung for rd-132211).
* a real end-to-end install: `jre-legacy` (300 files, 225 MiB) unpacked and
  **the extracted `bin/java -version` runs** (`1.8.0_202`); a game version
  (rd-132211) client jar + 9 libraries + 6 extracted natives; `mcli java list`
  then reports the runtime as source `mcli`, and `mcli java which rd-132211`
  picks it for the required major 8.
* the browser login flow up to the exchange: loopback on port 29116, callback
  page rendered, code sent to `login.live.com/oauth20_token.srf` (Microsoft
  answers `invalid_grant` for a fake code, which is the expected proof).

**Not verified, and why**

* a real sign-in: needs a human in a browser with a Microsoft account. Never
  attempt it, never ask for credentials.
* a real game launch: needs a display. This box is headless; expect the JVM to
  start and then die on graphics.
* `mcli java install 21` (java-runtime-delta, ~115 MiB) was not run — only
  `jre-legacy` was, so the non-legacy component path is untested end to end.
* Forge/Fabric and mod loading are out of scope by design.

**Environment quirks worth remembering**

* BMCLAPI answers with 302 to `*.sakura.sld.tw` nodes that frequently blow up
  with `net/http: TLS handshake timeout`. Everything still lands thanks to the
  candidate list + retries (`WARN` lines are normal, not a bug); do not "fix"
  this by removing the official fallback.
* Test homes used so far (safe to delete, nothing outside them matters):
  `/root/.hermes/cache/scratch/mtest` (instances, migrated legacy db) and
  `.../mtest2` (fresh TOML home).

**Loose ends for the next session**

1. Run a real `mcli launch <instance> --yes` on a machine with a display and a
   signed-in account; until that happens the process spawn path (pid/exit code,
   game log file) is only exercised by unit tests.
2. Try `mcli java install 21` once to prove the `java-runtime-delta` path (the
   code path is shared with jre-legacy, but untested).
3. Optional: a `--check`/`doctor` command (verify hashes of installed files),
   and per-instance `saves/` backup hints.

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
* Login is a two-flow affair, and that is not cosmetic:
  * `--flow browser` (default) uses `https://login.live.com/oauth20_authorize.srf`
    with `response_type=code`, a loopback server on ports 29116-29126 and
    `https://login.live.com/oauth20_token.srf` for the exchange. Scope
    `XboxLive.signin offline_access`. No client secret, no PKCE. This is what the
    reference launcher does, and it is the only flow the shipped client id
    supports.
  * `--flow device` uses
    `login.microsoftonline.com/consumers/oauth2/v2.0/devicecode` with scope
    `service::user.auth.xboxlive.com::MBI_SSL offline_access`.
  * Verified 2026-10-26: the reference launcher's client id
    (`288ec5dd-6736-4d4b-9b96-30e083a8cad2`, read out of its release jar's
    manifest) is accepted by Microsoft but answers
    `AADSTS9002346: not configured for the device code flow`; the legacy
    `00000000402b5328` is gone entirely (`AADSTS700016`). Hence the browser flow
    is the default and `auth_client_id` is configurable.
* Xbox Live chain endpoints that must stay reachable:
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
* Manual smoke test with isolated state (never point tests at `~/.mcl-cli`):

  ```sh
  export MCLI_HOME=/tmp/mcli-home MCLI_MINECRAFT=/tmp/mcli-mc LANG=en_US.UTF-8
  ./mcli version && ./mcli help && ./mcli config list | head
  ./mcli config set http_timeout 45 && cat "$MCLI_HOME/mcli.toml"   # comment-safe write
  ./mcli list --available --limit 5
  ./mcli create demo --version 1.21.4 && ./mcli create old --version 1.7.10
  ./mcli launch demo --dry-run --yes      # modern arguments
  ./mcli launch old  --dry-run --yes      # legacy minecraftArguments
  ./mcli java which 1.21.4                # -> 21
  ```

* Browser login without an account (proves the loopback + exchange wiring):
  start `./mcli login --no-browser --timeout 25` in a background terminal, read
  the port out of the printed `redirect_uri`, then
  `curl "http://localhost:<port>/authentication-response?code=FAKE"`. Expect the
  callback page plus Microsoft's `invalid_grant` for the fake code.

* Real downloads: `./mcli create tiny --version rd-132211 && ./mcli launch tiny --yes`
  installs a Java runtime (jre-legacy, 300 files) plus the smallest possible
  version, then stops with `no account is logged in` — the expected end of the
  run. Reuse `MCLI_HOME` to make the second run instant.

* `launch --dry-run` prints the full java command line; use it to sanity check
  classpath order, `-Djava.library.path`, the logging argument (must come before
  `-cp`) and the game arguments.