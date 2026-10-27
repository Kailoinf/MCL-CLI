# Working state — mcli

`AGENTS.md` keeps the rules that rarely change. This file is the part that
changes every session: where the project stands, what has actually been verified,
and what is left. Update it in the same commit as the work.

## Now

* version `0.2.0` (`app.go`), on-disk `state_version = 1`.
* `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green,
  `go build -o mcli .` green.
* Commands implemented: `version help list create delete launch refresh config
  java login logout account instance doctor repair server fabric search add mods`
  (`fix` aliases `repair`, `srv` aliases `server`, `fab` aliases `fabric`, `find`
  aliases `search`).
* `docs/` is a VitePress site (`pnpm install && pnpm docs:build`, output in
  `docs/.vitepress/dist`); `skill/SKILL.md` ships in the repository and is updated
  with the feature it describes.

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
* The EULA can be accepted after creation: `mcli server set <name> --eula` prints the
  URL, asks, and writes the file — the command `doctor` now names when it finds a
  server that would refuse to start. Verified end to end: doctor reports the refusal
  and the command, the command writes `eula=true`, and the next doctor passes.
* The first version of that command lied: it reported "eula.txt now accepts the
  licence" while the file still said `false`, because it went through `WriteFiles`,
  which only creates files that are missing. Accepting the licence now writes the file
  outright (`mcserver.AcceptEula`), in `server set --eula` and in `server create
  --eula` (where an existing refusal would have survived the same way). The claim was
  checked against the file, not against the message; the tests pin it.
* Adopting is automatic now, because it was asked for: `list` and a `minecraft_dir`
  switch take over every version directory with a version document, name each one, and
  keep the files where they are. Verified with a directory holding a vanilla install, a
  PCL-style Fabric profile and a folder with no JSON: the first two became instances
  named after their directories, the third was skipped, and a second `list` adopted
  nothing twice.
* Two things a real `launch` on someone else's machine exposed:
  * A version id that is not in the vanilla manifest is not an error — a PCL/HMCL
    profile is exactly that. The line is now "this version is not in the vanilla list;
    using the version file on disk" instead of a warning that the next lines contradict.
  * A stored account whose credential cannot be read (its `token_salt` is gone) used to
    be reported *after* the whole install work. The account is resolved before anything
    is fetched now: `launch` fails immediately with the log-in-again message, and
    `--dry-run` warns and continues, as before.
* `config set minecraft_dir <path>` scans the directory it was just pointed at and
  says how many versions are waiting to be adopted — the same report `list` gives —
  because a switch that registers nothing is indistinguishable from a switch that
  failed. Verified: a directory with 3 versions → the count and both commands; a
  directory without `versions/` → said so; a path that does not exist → said mcli will
  create it; an unrelated key → no scan.
* One `y` used to be not enough for a large download: `server start` offered the jar and
  the download policy offered the same file again, so the question appeared twice and
  the second copy read whatever was left on stdin. The answer is remembered per file
  and per batch for the rest of the run now, and a *different* file still asks.
  `TestTheSameLargeFileIsAskedAboutOnce` pins it: one line of input, one question for
  that file, and a second question for a file that has not been agreed to.
* A login that succeeds can still leave the account unusable: `storeSession` derived
  the per-account salt **before** giving a brand-new account its UUID, so the salt was
  written under a nameless key, the account itself kept none, and the next command
  said "this account has no token_salt; log in again" right after a login that reported
  success. Found from a user's transcript and reproduced by
  `TestLoginPutsTheSaltOnTheStoredAccount`, which fails on the old order and passes on
  the identity-first one. The writer also refuses to marshal a nameless record now, so
  mcli cannot write a credential document it would then complain about.
* refresh.toml written by this build is pinned by a test to the per-account shape
  (`[tokens.<uuid>]`), including that a record keyed by nothing is not turned into the
  flat `[tokens]` section the reader rejects — mcli must never warn about a file it
  wrote itself. The file, as written by this build, reads:

  ```
  [tokens.0123456789abcdef0123456789abcdef]
  token_salt = "…"
  refresh_token_enc = "…"
  ```
* An empty instance list next to a full `.minecraft` used to say only "no instances"
  and stop there, which is exactly the wrong information for someone who just pointed
  `minecraft_dir` at an existing install: the "unregistered versions" hint was printed
  only when at least one instance existed. The empty branch reports them now. Verified:
  0 instances + 2 version directories on disk → the list names both and the adopt
  command; `mcli instance import 1.21.4` → the table, with the remaining one still
  named.
* Reinstalling the program does not touch `~/.mcl-cli`, so an old layout can survive a
  new binary. Unknown keys in `refresh.toml`/`session.toml` are reported as what they
  are — keys this build skipped — and that is now followed by what follows from it:
  the stored login cannot be used, and mcli will not rewrite an older file by guessing.
  Reproduced from the shape a user reported (a flat `[tokens]` with `token_salt` and
  `refresh_token_enc`); the keyed `[tokens.<uuid>]` shape stays quiet. An earlier note
  here claimed the flat shape could not have come from this code at all — that was
  wrong. A fresh login did leave credentials under a nameless key (see the entry
  above), which is exactly the kind of file this warning is for. It also names the file
  and when it was last written now, because "which file, and how old" is the first
  thing worth knowing. Logging in again rewrites the file and the warning goes away.
* CurseForge is wired for `search` and `add` (`--source curseforge`). Verified here: no
  key means the exact instruction and no request; a wrong key makes a live call to
  api.curseforge.com and comes back `HTTP 403`, which the CLI reports as "the key was
  rejected" plus the command that shows its length; the key itself never appears in
  output (checked by grepping for it). Pinned by tests: the request (x-api-key,
  gameId=432, classId, gameVersion, modLoaderType, paging), the parsing, hash
  selection (algo 1 is SHA-1), the "newest release for this game version, then beta,
  then alpha" file rule, and the sentinel errors.
* Not verified: a working key. Nobody has run a real CurseForge search or install
  here, so the success path — an actual file list and a download from
  media.forgecdn.net — is untested. Think of it as "the shape is right, the last step
  has never been seen".
* The API key is a credential, so `config get`/`config list` report its length and
  whether it is set, never its value. That is a rule in `config.Secret`, not a habit:
  `mcli.toml` holds the key in plain text, and every path that prints configuration
  goes through the same masking.
* Servers are in `doctor` and `repair`. `doctor <server>` checks the jar against the
  checksum the version publishes, `eula.txt`, `server.properties` (and whether another
  mcli server claims the same port), the Java the version needs, a recorded pid that is
  no longer running, and how the last log ended. Verified directly: a removed jar is
  reported with its expected size and exit 1; a refused EULA is reported with the URL;
  `repair --yes` leaves `eula.txt` at `eula=false` — it owns the jar and nothing else; a
  second server configured on the same port is named; a detached server killed with
  `-9` shows up as a stale pid; and `doctor --all` covers servers as well as instances.
* `repair <server>` deliberately fetches one file: `eula.txt`, `server.properties` and
  the world are the person's own. A repair that overwrote them would be a repair that
  cost someone a world.
* The working skill was rewritten into the shape the authoring guide asks for: 429
  lines of appended lessons became a 150-line guide plus
  `references/verification-recipes.md` and `references/service-facts.md`. It had
  drifted into a log (dated "verified …" notes), a second copy of what belongs on this
  page, and two contradictions of its own — one line still said to stop a server with
  SIGINT because "the world is saved on the way out", after the measurement above
  showed a signal saves nothing. Rules now live in the skill, facts and state here.
* A fresh `pnpm install` stopped with `ERR_PNPM_IGNORED_BUILDS` (`Ignored build
  scripts: esbuild@0.21.5`), so the documented way to build the docs site did not
  work from a clean clone. The setting had been in a `pnpm` field in package.json,
  which pnpm 10 and later ignore outright ("The \"pnpm\" field in package.json is no
  longer read by pnpm"), and the name pnpm 12.14 wants is `allowBuilds` in
  `pnpm-workspace.yaml` — not the older `onlyBuiltDependencies`, which pnpm reads into
  its config but does not act on. Found by running `pnpm approve-builds --all` and
  reading what it wrote. Verified by deleting `node_modules`, installing again (clean,
  no error) and building the site (`pnpm docs:build`, 13 pages).
* The console gate has to know *which* run it is waiting on: `latest.log` still holds
  the previous server's output until the new one rotates it, so asking only "is
  `Done (` in the log?" answered yes at once and the piped commands went early again
  — the same two errors, from a gate that looked right. It requires the log to have
  been written after this start now (`WaitReady(dir, since, timeout)`), and a test
  pins that a stale line is not readiness.
* A drain for leftover console commands was written, measured and then deleted: a
  FIFO's buffered bytes are discarded when the last handle closes, so what a stopped
  server's pipe held never reaches the next one. There is nothing to drain, so the
  code is not there — and `internal/mcserver` has no `DrainConsole`.
* Commands sent to a server before its command dispatcher is up are answered with
  `An unexpected error occurred while trying to execute that command` — seen twice,
  with a script feeding `list` and `stop` into a freshly started server. The console
  pump therefore waits for the server's own readiness line (`Done (`) before
  forwarding anything: what a person types meanwhile waits in their terminal, and
  what a pipe holds waits in the pipe. A scripted console — `printf 'list\nstop\n' |
  mcli server start <name>` — is a case worth keeping working.
  Re-run after the fix, both orderings at once: `printf 'list\nstop\n' | mcli server
  start` now answers `There are 0 of a max of 20 players online` and stops cleanly
  whether the lines arrive before or after the server is up (`exit=0` both times, 0
  leftover processes, port free); before the fix the early ordering produced the
  "unexpected error" line above. That script is the regression check for this.
* A console pipe must not be opened before something holds the other end: a FIFO
  opened write-only waits for a reader, so `mcli server start` hung before it had
  started anything. Found by looking at a real run rather than guessing — mcli was
  asleep inside `fifo_open`. The console writer opens read-write (which returns at
  once on Linux), and the foreground pump now starts from the child's pid callback,
  after the server holds the pipe. `internal/mcserver/console_unix_test.go` pins
  both: the writer does not block with no reader, and the pipe is created once.
* **A signal is not a graceful stop for a headless server, and mcli was claiming it
  was.** `server stop` printed "it saves the world before exiting" while sending
  SIGINT. Measured on this box against a real 26.3 server: both SIGINT and SIGTERM
  end the process outright — its own log shows neither `Stopping server` nor
  `Saving worlds`, so the world keeps only what the last autosave wrote. The
  server's own console command `stop` does the real thing (verified: after writing
  it, the log has `Stopping server` and `Saving worlds`, the listener stops, the pid
  clears and the port is free). `server stop` writes that command into the console
  FIFO now; `--force` kills, and says that it killed.
* A foreground server's pid used to be recorded only *after* it exited, so a
  `timeout 100 mcli server start` left a server running on its port with nothing in
  the state pointing at it — invisible to `server list` and unreachable by
  `server stop`. That is how this was found. The pid is recorded the moment the
  process exists, in both modes, and cleared when it goes.
* A foreground run now owns the console the way a detached one does (input through
  the FIFO, output to the terminal, the server in its own process group), so Ctrl+C,
  `timeout`'s SIGTERM and `mcli server cmd <name> stop` all stop it through its own
  command instead of killing a JVM that would save nothing.
* A platform pass, because the maintainer runs mcli from Git Bash on Windows and a
  launcher that only works where it was written is not much of a launcher. What was
  found and fixed, and what it is now checked by:
  * **every 32-bit target failed to compile** — the XSTS error codes Microsoft
    returns (2148916233, …) do not fit in a 32-bit int. They are named int64
    constants now, and `linux/386`, `windows/386` and `linux/arm` build.
  * automatic heap sizing only worked on Linux (`/proc/meminfo`). It now also asks
    kernel32 on Windows (`GlobalMemoryStatusEx`) and sysctl on macOS (`hw.memsize`,
    with half the physical total as the available figure, because macOS's "free"
    memory means something else). The parsing is a function of its own with tests
    for an old kernel without MemAvailable and for junk input.
  * `doctor`'s free-space line was omitted on Windows; it asks kernel32
    (`GetDiskFreeSpaceExW`) there now.
  * the classpath separator is settable (`launch.Options.PathSeparator`), so the
    Windows shape — classpath joined with `;`, quoted paths — is tested from Linux.
  * `server attach`/`cmd` and graceful `server stop` already refused honestly on
    Windows with the alternative named; that is now written down in the docs rather
    than left for someone to discover.
* `scripts/check-cross.sh` builds all eight claimed targets at once (linux amd64,
  arm64 and 386; windows amd64 and arm64; darwin amd64 and arm64; freebsd amd64).
  Compiling is not behaving — the docs say which is which.
* Modpack updating: `mcli update <instance> --pack` (and `--pack-version` on both
  `add` and `update`). Verified against the real pack: beta.1 installed, then moved
  to beta.2 — 7 files taken back, 6 added, 1 changed. The removal list was checked
  against a diff computed independently from the two published indexes (same 7
  paths), and a file replaced by hand before the update survived with its contents
  while the pack's own six were deleted. What makes that exact is the record: each
  pack file is stored with the digest the pack declared, and a file is deleted only
  while its digest still matches. The first attempt compared modification times
  instead and kept three files it should have deleted, which is how this ended up
  digest-based.
* `add <pack>` --pack-version picks a named pack version; a pack version that fits
  the instance's game version is preferred over merely the newest one, and an
  instance already on the newest fitting pack says so.
* Two bugs in the TOML writer, both found by writing state that had to survive:
  * a map of plain values nested inside a struct was **dropped entirely** — no
    error, no key, the data simply was not there. That shape is what records pack
    files, so it is now written as its own table (`[instances."x".pack_files]`),
    with `internal/toml/nested_map_test.go` pinning the round trip
  * `omitempty` was ignored, so every optional field was written as `""`, `[]` or a
    zero: `java = ""`, `xmx = ""`… in every instance. Empty optional values are now
    left out, which is what the tag always claimed
* A consequence worth knowing: state.toml carries one shape and no migration (a
  standing decision), so a file written by an earlier build — pack files as an
  array instead of a table — makes mcli stop with
  `instances.<name>.pack_files: unsupported field kind map` rather than guess. The
  message names the field.
* `instance show` gained a row for the pack an instance was built from.
* `mcli update` moves an instance to another game version and was verified three
  ways: `update modtest --version 1.21.4` fetched 189 files / 97.7 MiB (a version
  that was not on this box at all — client jar, libraries, natives, assets) and
  repointed the instance; `update legacy --version 26.3` moved a 1.8.9 instance and
  reported `1.8.9 → 26.3`; and a Fabric instance moved to 26.3 by building the
  loader profile for the new game version rather than dropping the loader. `update`
  with no `--version` means the newest release; an instance already on it is only
  checked for completeness (`26.3 is already installed and complete`), and the
  version it left behind keeps its files, since another instance may run them.
* `instance set --loader vanilla` used to answer "no options given" on a profile
  another launcher wrote: it decided whether the instance was modded by looking at
  how the id was spelled. It now asks the version JSON, which is the same fix as
  the pack bug above, and the message for "options given, nothing to change" is its
  own line instead of the usage dump.
* Modpack installation, verified against a real pack: `add fabulously-optimized`
  into an instance another launcher made fetched the `.mrpack` (173,859 bytes), read
  its index (48 files, 41.9 MB, `fabric-loader 0.19.5` + `minecraft 26.3`) and
  installed it — 45 jars into `mods/`, 3 zips into `resourcepacks/`, 51 files from
  the pack's `overrides/` (a re-run writes 0 and keeps 51, and a file edited by hand
  survives), every download checked against the SHA-1 the pack lists. The pack's game version is checked against the instance — the same command
  into a 1.21.4 instance refused it by name and printed the `mcli create` line that
  builds the right one — and a loader build the pack did not name is a warning, not
  a failure. Pack paths that would land outside the instance directory are refused,
  and `overrides/` never overwrites a file that already exists.
* Two bugs that only a real pack could find, both in what an instance *is*:
  1. That instance was built by PCL, which writes its own id and puts the game
     version in `clientVersion` instead of `inheritsFrom`. Reading the game version
     off the profile id compared the pack's `26.3` against `26.3-Fabric 0.20.1` and
     refused a pack that fits. Versions now answer for themselves:
     `Version.GameVersion()` prefers `inheritsFrom`, then `clientVersion`, then the
     id, and `Version.Loader()` reads the loader and its version from the main class
     and the loader libraries — so a profile mcli did not write answers the same
     questions, and the loader-version warning prints `0.19.5` vs `0.20.1` instead of
     two profile names.
  2. A zip stores its folders as entries of their own (`overrides/` with nothing
     after the prefix). Resolving that empty path failed the whole overrides step
     *after* 42 MB had been downloaded. Folder entries are skipped before a path is
     resolved; the fixture in `internal/mrpack` now contains them, which is why the
     unit tests had missed it.
* Modrinth search and install, verified against the live API: `search sodium
  --type mod --version 1.21.4 --loader fabric` listed 5 of 35 hits with downloads
  and authors; `add sodium --instance mf` downloaded the Fabric build
  (`sodium-fabric-0.6.13+mc1.21.4.jar`, 1,306,799 bytes) with the SHA-1 the API
  declares, and a second run reported it as already installed and intact;
  `mods mf` listed it. A resource pack went to the right place too
  (`add faithful-32x --type resourcepack` picked `Faithful 32x - 1.21.4.zip`,
  10.0 MiB, into `resourcepacks/`), the loader was inferred from the instance
  (`fabric`), and a vanilla instance was refused with the command that fixes it
  instead of getting a jar it cannot load.
* The mod loader became a **choice on the version** rather than a second positional
  and a command of its own: `mcli create <name> --version <v> --loader vanilla|fabric
  [--loader-version <v>]` and the same pair on `mcli instance set`, with
  `mcli fabric list <game>` left as discovery. `fabric install` is gone. The
  reasoning is the standard one — one positional for the thing the command acts on,
  everything else a flag with a closed set of values (GNU long options; clap's
  ValueEnum; CMCL hangs the loader off its version command, Fabric's own installer
  takes `-loader`) — and an unknown value exits 2 listing the alternatives.
* Verified round trip: `create mf --version 1.21.4 --loader fabric` installed
  profile 0.20.1 and pointed the instance at `fabric-loader-0.20.1-1.21.4`
  (dry-run: 69 classpath entries, `KnotClient`, `fabric-loader-0.20.1.jar`);
  `instance set modtest --loader fabric` moved an existing instance onto the
  profile and `--loader vanilla` moved it back to `1.21.4`. Real testing also
  caught a bug the unit tests could not: an empty `--loader-version` did not mean
  "newest stable" (the profile URL came out with a blank build and got a 404), so
  `isLatest` now accepts the empty string and a test pins that.
* A real server, start to stop, on this box (which is exactly what a headless
  machine can still verify end to end): `server create mc-test --version 26.3
  --port 25566 --eula` printed the EULA URL, wrote `eula.txt` and
  `server.properties`; `server start --detach` fetched the 62,294,556-byte server
  jar through BMCLAPI (exactly the size the JSON declares), the bundler unpacked
  `libraries/` and `versions/` into the server's directory, the log reached
  `Done (3.533s)`, the port listened on `*:25566`, and the world was created.
  `server list` then showed `运行中` with the pid, `server stop` SIGINTed it and
  returned in ~1s, `world/level.dat` was written on the way out, and a second
  `stop` reported `没有在运行` (exit 1).
* A detached server must not inherit mcli's stdout: an open pipe makes every
  caller wait for a process that never ends. It now writes to
  `logs/console.log` in its own session, so `start --detach` returns in 0.2s.

* Fabric, against the loader PCL had already installed on the same machine:
  `fabric list 26.3` reported 255 builds with 0.20.1 newest stable, `fabric
  install 26.3` wrote `versions/fabric-loader-0.20.1-26.3/fabric-loader-0.20.1-26.3.json`,
  and `create fab2 --version 26.3 --loader fabric` produced an instance whose dry-run
  command line has `KnotClient`, `-DFabricMcEmu= net.minecraft.client.main.Main `
  verbatim, the parent's `26.3/26.3.jar` and **62** classpath entries. The entry
  list is byte-identical to the one mcli builds for PCL's own
  `26.3-Fabric 0.20.1` profile (`diff` of the two classpaths is empty).
* A real bug that only Fabric exposed: Fabric's profile lists its libraries as
  `name` + `url` + `sha1` with **no** `downloads` block, and `Library.ArtifactDownload`
  returned false for anything without one — so the loader never reached the
  classpath (54 entries instead of 62) and the game would have died looking for
  its main class. The path is now built from the maven name and the repository the
  entry names; `internal/mcv/library_url_test.go` pins it, including that a
  natives classifier still produces no classpath artifact.
* `internal/i18n/callsites_test.go` counts the arguments of every `i18n.T("key", …)`
  call site and compares them with the key's verbs. It found eight broken call
  sites on its first run (`fab.installed`, `launch.auto_create`, …) which had been
  printing `%!s(MISSING)` at run time — the catalog tests could not see those.
* All three server console modes, verified for real: the foreground console with
  stdin held open ran `list` (logging `There are 0 of a max of 20 players
  online:`) and `stop` (`Stopping the server`), ending with exit code 0;
  `server attach` against a detached server delivered a piped `list`; and
  `server cmd <name> say hello from mcli` appeared in the log as
  `[Server] hello from mcli`. A pipe that closes before the server's console
  thread exists loses its commands, so keep stdin open until the log says `Done`.
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
* Windows and macOS behaviour: both compile (`scripts/check-cross.sh`), and the
  platform-shaped parts that can be tested from Linux are tested (the classpath
  separator, quoting a path with a space, and the memory parsing). What is *not*
  verified is running mcli on Windows itself — a real launch there, the detached
  server's console path, and Windows path handling under a running process.
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


1. **CurseForge**: Modrinth needs no key and is wired; CurseForge needs an API key
   per user, so it would need a config key and a key-shaped error path. Not started.
2. **CurseForge packs**: `.mrpack` packs install and update; a pack that ships as a
   CurseForge zip is not implemented. Updating both the game version and the pack
   inside an instance works (`mcli update`, `mcli update --pack`).

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
5. **`server stop` on Windows**: a console interrupt cannot be delivered there, so
   the command reports the pid as unstoppable and points at `--force` (Kill). A
   Windows pass should decide whether to keep that or drive the console instead.
6. **Servers in `doctor`**: health checks cover client instances only; a server's
   jar, world, port and runtime could use the same line.