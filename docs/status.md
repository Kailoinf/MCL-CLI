# Working state — mcli

`AGENTS.md` keeps the rules that rarely change. This file is the part that
changes every session: where the project stands, what has actually been verified,
and what is left. Update it in the same commit as the work.

## Now

* version `0.2.0` (`app.go`), on-disk `db_version = 3`.
* `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green,
  `go build -o mcli .` green.
* Latest commits on `main`: `bef4e06` (instance command), `3c295a6` (credentials
  split, account command, adopting existing installs).

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
* The credential split: an old hand-written `state.toml` (v2, tokens inline) is
  migrated on open — `state.toml` comes out secret-free, `refresh.toml` gets the
  salts and refresh tokens, `session.toml` the Minecraft token and expiry,
  `mcli account list` renders throughout, `logout <name>` removes the account
  from all three documents.
* Runtime install: `jre-legacy` (8) and `java-runtime-delta` (21) are on disk and
  `bin/java -version` runs for both. `java install 25` (java-runtime-epsilon,
  ~700 files) is slow through the mirror; see "Environment quirks".
* The partial-runtime bug is fixed and exercised: an interrupted install (no
  `.mcli-ok` marker) no longer reports "already installed".
* `dl.SourceHealth` unit test: six files, a dead mirror is attempted exactly
  three times and the official source serves all six.

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

## Loose ends, in the order they matter

1. **`mcli doctor` / `mcli repair`** — `instance import` already prints "next:
   `mcli doctor <name>`", so the promise is outstanding. Plan: lift the file
   enumeration out of `mcv.Installer.Ensure` into a reusable `Plan` (it already
   computes `missing` via size + SHA-1), report per-instance health from it
   (missing / corrupt / bytes, index present, java match, saves present), and let
   `repair` fetch exactly that list with `--force` for a full re-download.
2. **`auto_memory_percent` is a dead config key** — `launch.RecommendMemory()`
   ignores it and uses a hardcoded 50 %/75 % rule. Either honour it or remove the
   key; leaving a documented setting that does nothing is worse than both.
3. **`launch --server host[:port]`** (modern versions want
   `--quickPlayMultiplayer`, older ones `--server`/`--port`) and a `--fullscreen`
   equivalent, for people who launch straight into a server.
4. **Instance-level `--force` reinstall** (`launch --force` re-downloads the
   instance's files without touching saves).
5. **Windows pass**: run the scan/import/dry-run chain on the maintainer's
   machine (UCRT64) and fix whatever path handling surfaces.