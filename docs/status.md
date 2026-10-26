# Working state — mcli

`AGENTS.md` keeps the rules that rarely change. This file is the part that
changes every session: where the project stands, what has actually been verified,
and what is left. Update it in the same commit as the work.

## Now

* version `0.2.0` (`app.go`), on-disk `state_version = 1`.
* `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green,
  `go build -o mcli .` green.
* Commands implemented: `version help list create delete launch refresh config
  java login logout account instance doctor repair` (`fix` aliases `repair`).

### Layout that the code assumes today

```
~/.mcl-cli/     mcli.toml (config) · state.toml (identity, no secrets)
                refresh.toml (long-lived tokens) · session.toml (short-lived)
                last.toml (previous launch) · jdk/ · cache/ · logs/
~/.minecraft/   libraries/ · assets/ · versions/<version-id>/ · versions/<instance>/
```

## Verified with real commands (2026-10-26)

* Instance lifecycle on an isolated home: `create`, `instance list/show/set/copy/
  rename`, `scan`, `import`, plus the error paths (unknown instance, unknown
  option, extra positional → exit 2).
* A **real PCL install** (a Windows `.minecraft`, `7za x` into
  `/root/.hermes/cache/scratch/mcuser`): `instance scan` found
  `26.3-Fabric 0.20.1`, `import` registered it, and `launch --dry-run` produced a
  full command line — 55 classpath entries with the client jar last, taken from
  the profile's own directory, `net.fabricmc.loader.impl.launch.knot.KnotClient`
  as main class, `natives/{java,jna,lwjgl,netty}` referenced and created by the
  installer's extraction step, Fabric libraries from `maven.fabricmc.net` passed
  through to their own host. Exactly one library was missing on disk
  (`netty-transport-native-epoll`), which a real install would fetch. A dry run
  reports "will download 14 files, 10.0 MiB (5202 already present)".
* Values from a version JSON are passed through **verbatim**, including one that
  looks wrong: this profile's `-DFabricMcEmu= net.minecraft.client.main.Main `
  really carries a leading and a trailing space in the JSON that Fabric's
  installer wrote. Do not "clean" argument values — a path with a space and a
  game argument with a space both have to survive, and every other launcher
  passes this one through as it is.
* `launch --version <other>` points the client jar and natives at the *version's*
  directory, not the instance's (previously wrong).
* Runtime install: `jre-legacy` (8) and `java-runtime-delta` (21) are on disk and
  `bin/java -version` runs for both. `java install 25` (java-runtime-epsilon,
  ~700 files) is slow through the mirror; see "Environment quirks".
* The partial-runtime bug is fixed and exercised: an interrupted install (no
  `.mcli-ok` marker) no longer reports "already installed".
* `dl.SourceHealth` unit test: six files, a dead mirror is attempted exactly
  three times and the official source serves all six.
* `mcli doctor` / `mcli repair` against the real PCL install: `doctor
  "26.3-Fabric 0.20.1"` reported 5202 of 5216 files in place, 14 missing
  (10.0 MiB), the asset index present (5147 objects), natives not yet extracted,
  one mod jar, the directory size, and exited 1. `repair … --yes` fetched exactly
  those 14 files, re-verified (5216/5216), extracted 24 native files — including
  `natives/lwjgl/linux/x64/org/lwjgl/...` plus the empty `java`/`jna`/`netty`
  subdirectories the version's JSON points at — and checked the runtime; a second
  `doctor` found no problems and exited 0.
* `doctor --all` with nothing installed reports the environment (home, the five
  files, free space, java runtimes, manifest cache, newest game log), lists the
  instances, and exits 1 exactly when something is wrong.
* `launch --server host[:port]` (`--fullscreen`, `--demo`) on both argument
  generations: the modern profile produced `--quickPlayMultiplayer
  example.com:25565` from its own JSON (feature `is_quick_play_multiplayer`) and
  no `--quickPlayPath` — that one is only passed when a launcher has a quick play
  file to hand over — while 1.8.9 got the classic `--server play.example.net
  --port 25566 --demo --fullscreen` appended to its `minecraftArguments`. A bad
  address (`host:abc`) exits 2, and a launch without the switches adds none of
  them. `internal/launch/server_test.go` pins all of it.
* The no-compatibility decision is in force: the pre-TOML JSON database import,
  the v2 `state.toml` with inline tokens, `paths.LegacyDB` and every migration
  test are gone, and the store is no longer called a database anywhere
  (`store.Store`, `store.StateVersion`, on-disk `state_version`).
* A second, newer version was fetched end to end on the maintainer's request:
  `create modern --version latest` (26.3, the manifest's current release) then
  `repair modern --yes` through BMCLAPI — 5216/5216 files, 5147 asset objects,
  natives extracted, `doctor` clean. The Fabric fixture next to it stayed intact.
* `auto_memory_percent` really sizes the heap now: on this 7.5 GiB box, 25 →
  `-Xmx1043M`, 50 → `-Xmx2085M`, 75 → `-Xmx3142M`, clamped to half of physical
  memory and never above what is free.
* `internal/mcv/plan_test.go` pins the plan offline: the file list, the natives
  count, asset objects at `objects/<first2>/<hash>`, all three missing kinds
  (absent / wrong size / wrong hash), and that planning itself writes nothing.

## Not verified, and why

* A real sign-in: needs a human in a browser with a Microsoft account. Never
  attempt it, never ask for credentials.
* A real game launch: this box is headless, and the maintainer asked explicitly
  that launches not be attempted. Correctness is judged from
  `launch --dry-run` output (classpath, placeholders, natives paths, game args).
* Windows: the maintainer runs mcli from UCRT64 (Git Bash) on Windows; all of the
  above was verified on Linux. Windows-only risks: path separators inside natives
  arguments, quoting an instance name that contains a space, and drive-letter
  paths in `minecraft_dir`.
* Nothing of the maintainer's own `.minecraft` was written to: the archive was
  unpacked into scratch and all homes used for testing live under
  `/root/.hermes/cache/scratch/` (`mtest`, `mtest2`, `mcuser-home`).

## Service facts (verified with real requests, 2026-10)

* `bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json` can answer 302 to a
  `*.sakura.sld.tw` node that stalls; the official `piston-meta.mojang.com` copy
  is the reliable fallback. Every download is therefore a candidate list and `dl`
  keeps its 20 s stall watchdog — do not remove the official fallback.
* BMCLAPI serves `/version/<id>/json` and `/version/<id>/client`, mirrors
  `piston-meta`/`piston-data` paths verbatim, maven libraries at `/maven/<path>`,
  asset objects at `/assets/<first2>/<hash>`, and Forge maven under `/maven`.
  There is **no** `/assets/indexes/<id>.json` (404) — use
  `/v1/packages/<sha1>/<id>.json`. `/java/list` is 403: do not build on it.
* Libraries on other hosts (`maven.fabricmc.net`, `maven.minecraftforge.net`)
  pass through to their official URL untouched; only Mojang hosts are mirrored.
* Mojang's java runtime product manifest
  (`launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96…/all.json`) is
  mirrored by BMCLAPI, keyed by platform (`linux`, `linux-i386`, `windows-x64`,
  `mac-os`, `mac-os-arm64`, …) then component. Each component manifest lists
  `files` with `type: file|directory|link`, `executable` and `downloads.raw`
  (sha1/size/url on piston-data). Components seen: `jre-legacy`=8,
  `java-runtime-alpha`=16, `-beta`/`-gamma`=17, `-delta`=21, `-epsilon`=25.
* Version JSON: modern documents carry `arguments.jvm`/`arguments.game` (a string
  or `{rules, value}`), `javaVersion.{component,majorVersion}` and natives as
  separate libraries named `…:natives-linux`; old ones carry
  `minecraftArguments`, a `natives` map and `downloads.classifiers`. Both paths
  must keep working. The current release line (`26.3`, snapshot `26.4-snapshot-5`)
  needs **Java 25**; `javaVersion.majorVersion` is authoritative.
* Login is a two-flow affair and that is not cosmetic: the browser flow (default)
  uses `login.live.com/oauth20_authorize.srf` with `response_type=code`, a loopback
  server on 29116-29126 and `login.live.com/oauth20_token.srf` for the exchange —
  no client secret, no PKCE. The device flow uses
  `login.microsoftonline.com/consumers/oauth2/v2.0/devicecode`. The reference
  launcher's client id (`288ec5dd-6736-4d4b-9b96-30e083a8cad2`) is accepted by
  Microsoft but answers `AADSTS9002346` ("not configured for the device code
  flow"), and `00000000402b5328` is gone (`AADSTS700016`) — hence the browser
  flow is the default and `auth_client_id` is configurable.
* Xbox Live chain: `user.auth.xboxlive.com/user/authenticate`,
  `xsts.auth.xboxlive.com/xsts/authorize`,
  `api.minecraftservices.com/authentication/login_with_xbox`,
  `/minecraft/profile`. XSTS codes worth translating: 2148916233 (no Xbox
  profile), 2148916235 (region), 2148916238 (child account).

## Manual recipes that worked

```sh
# isolated home — never point a run at the real ~/.mcl-cli
export MCLI_HOME=/root/.hermes/cache/scratch/mtest2/home
export MCLI_MINECRAFT=/root/.hermes/cache/scratch/mtest2/mc

./mcli version && ./mcli help && ./mcli config path
./mcli list --available --limit 5
./mcli create demo --version 1.21.4 --force        # --force skips the manifest
./mcli instance set demo --xmx 4G --jvm-arg=-XX:+UseG1GC
./mcli instance show demo && ./mcli instance list
./mcli instance copy demo clone --with-files && ./mcli instance rename clone renamed
./mcli launch demo --dry-run --yes                 # inspect the java command line
./mcli doctor --all                                # what is wrong here, instance by instance
./mcli repair demo --dry-run                       # what repair would fetch (exit 1 when broken)
./mcli java which 1.21.4                           # -> 21
```

* Browser login without an account (proves the loopback + exchange wiring): start
  `./mcli login --no-browser --timeout 25` in the background, read the port out of
  the printed `redirect_uri`, then
  `curl "http://localhost:<port>/authentication-response?code=FAKE"`. Expect the
  callback page and Microsoft's `invalid_grant` for the fake code.
* Real installs: `./mcli create tiny --version rd-132211 && ./mcli launch tiny --yes`
  installs a Java runtime plus the smallest possible version, then stops with
  "no account is logged in" — the expected end of the run. Reuse `MCLI_HOME` and
  the second run is instant.
* Adopting a real install: `7za x .minecraft.7z` into a scratch directory, point
  `MCLI_MINECRAFT` at the unpacked `.minecraft`, then `instance scan` →
  `instance import "<name>"` → `launch "<name>" --dry-run --yes`.
* Check a dry-run command line for: classpath ending with the client jar, the
  log4j argument before `-cp`, `-Djava.library.path` and the natives
  subdirectories, `--gameDir` pointing at the instance, `--assetIndex` matching
  the version's asset id.

## Environment quirks worth remembering

* BMCLAPI answers 302 to `*.sakura.sld.tw` nodes that stall with
  `net/http: TLS handshake timeout`. `WARN` lines about a failed candidate are
  normal. The official fallback must stay in every candidate list; on top of that
  `dl.SourceHealth` now moves a source that fails three times in a row to the end
  of the list for the rest of the run (once per source, logged once).
* A Java runtime is ~700 small files, so a stalling mirror hurts most there: the
  first attempt at `java-runtime-epsilon` spent minutes on TLS timeouts before
  the download was interrupted. Prefer running it when the network is good, and
  re-run to resume (verification skips what is already correct).
* A full modern install is large: assets for 26.3 are ~474 MB, libraries
  ~88 MB. A machine that already has another launcher's data needs almost none of
  that re-downloaded, which is the point of `instance import`.
* `7za` (p7zip) unpacks `.minecraft.7z`; the maintainer's upload lives at
  `/root/mcli-test/.minecraft.7z` (506 MB compressed, 630 MB unpacked).

## Planned work, in the order the maintainer asked for it

1. **Server-side launch** — `mcli server` next to the client commands: download
   `downloads.server` from the version JSON, write `eula.txt` and a minimal
   `server.properties`, then start it with the version's own Java major and
   `-Xmx/-Xms`. A server needs no display, so this one *can* be verified end to
   end on this box (start it, wait for "Done", stop it).
2. **Fabric loader installation** — today mcli only *runs* an existing modded
   profile. Installing means fetching the loader's version JSON from
   `meta.fabricmc.net` (`/v2/versions/loader/<game>/<loader>/profile/json`), which
   is already the shape `mcv.Version` parses, and merging it as a child of the
   game version.
3. **Mod and resource-pack search** — mirror what PCL and Console Minecraft
   Launcher do: query Modrinth's and CurseForge's search APIs, list results, and
   install a hit into `mods/` or `resourcepacks/` of an instance. Keep the search
   itself thin (one query, one page, the fields that matter) — the interesting
   work is installing the result into the right instance.

## Conventions (detail AGENTS.md deliberately leaves out)

* **Adding a command**: usage/description keys (`cli.cmd.<name>.usage`, `.desc`)
  in both catalogs, an entry in the `cmds` table in `printHelp` (main.go) and in
  the `switch` in `run`, then `func (a *App) cmdX(args []string) int` built on
  `parseFlags("x", args, map[string]flagSpec{...})`. Unknown options stay errors
  so a typo cannot change behaviour, and extra positionals are rejected with
  `cli.unexpected_arg` rather than ignored. `a.fatal(err)` exits 1,
  `a.usageError(...)` exits 2.
* **Tables** pad by display width (`table.go`), never `% -20s`: a CJK path or a
  Chinese status word would shift every following column.
* **Where things live**: `main.go` dispatch/help · `commands.go`
  login/logout/list/create/delete/refresh/config/java · `instance_cmd.go`
  instance sub-commands · `instance_import.go` scan/import + name rules ·
  `account_cmd.go` accounts · `doctor_cmd.go` doctor/repair · `launch_cmd.go`
  launch + java choice · `internal/{i18n,paths,toml,config,store,secret,logx,mirror,dl,mcv,java,auth,launch}`.
* **Tests stay offline**: `httptest` stands in for failing mirrors, synthetic
  version JSONs and zip fixtures for `internal/mcv`, temp directories for
  `internal/store`. Parsing and decisions get tested; the network, a browser and a
  display are verified by hand.

## Loose ends, in the order they matter

1. **`mcli launch --server host[:port]`** (modern versions want
   `--quickPlayMultiplayer`, older ones `--server`/`--port`) and a `--fullscreen`
   equivalent, for people who launch straight into a server.
2. **Instance-level `--force` reinstall** (`launch --force` re-downloads the
   instance's files without touching saves) — `repair --force` already covers the
   file half of this.
3. **Windows pass**: run the scan/import/dry-run/doctor chain on the maintainer's
   machine (UCRT64) and fix whatever path handling surfaces (`disk_windows.go`
   only omits the free-space line; the rest of doctor is path-agnostic).
4. **`mcli doctor` for Java**: doctor reports the runtime a version needs and
   which one would be used, but does not yet check that the chosen runtime
   actually starts.