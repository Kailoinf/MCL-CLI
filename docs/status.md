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
  (`netty-transport-native-epoll`), which a real install would fetch.
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
6. **AGENTS.md trim** — it still carries the old v0.1.0 handoff note and the
   "three TOML documents" description. It is a protected file: the maintainer
   has to approve the edit. Keep the rules and the code map there, keep the live
   state here.