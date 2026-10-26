# Verification recipes

How to prove a part of mcli works, without a display, against a scratch home. Run
these from `/root/mcli` with the fixtures in `SKILL.md` exported. Anything that
installs a version is slow (~560 MB for a modern release): run it as a background
process with `notify` and read the log file back.

## Launch argument lines

```sh
export MCLI_HOME=/root/.hermes/cache/scratch/mtest/home MCLI_MINECRAFT=/root/.hermes/cache/scratch/mtest/mc
./mcli create demo --version 1.21.4 && ./mcli create old --version 1.7.10
./mcli launch demo --dry-run --yes   # modern: arguments.jvm/arguments.game
./mcli launch old  --dry-run --yes   # legacy: minecraftArguments
```

Expect: classpath ends with the client jar, the log4j argument sits **before** `-cp`,
`-Djava.library.path` points at `versions/<version>/natives`, `--gameDir` at
`versions/<instance>`, memory flags first. Both argument generations must work.

Server and one-off switches:

```sh
./mcli launch <modern> --server example.com --dry-run --yes
#   -> --quickPlayMultiplayer example.com:25565, and no --quickPlayPath
./mcli launch <legacy> --server play.example.net:25566 --demo --fullscreen --dry-run --yes
#   -> --server play.example.net --port 25566 --demo --fullscreen, after minecraftArguments
```

## Health, and installing without an account

```sh
./mcli doctor --all                   # machine + every instance; exit 1 when broken
./mcli doctor "<instance>" --deep     # also checks asset SHA-1s, not just sizes
./mcli repair "<instance>" --dry-run  # exactly the file list repair would fetch
./mcli repair "<instance>" --yes      # and fetch it
```

`doctor` and `repair` read one shared plan (`mcv.Plan`), so they cannot disagree.
`repair` is also the way to install a version without logging in.

## Servers (the one thing to run for real)

```sh
./mcli server create mc-test --version 26.3 --port 25566 --eula
./mcli server start mc-test --detach --yes
for i in $(seq 1 30); do grep -q "Done (" .../servers/mc-test/logs/latest.log && break; sleep 3; done
./mcli server stop mc-test                        # console `stop`: saves, then exits
grep -E "Stopping server|Saving worlds" .../servers/mc-test/logs/latest.log
```

Expect: `--detach` returns in well under a second with a real pid, the log reaches
`Done (Ns)!`, the configured port listens, and the stop lines appear — `Saving worlds`
is the proof the world was written, not lost. A signal (SIGINT/SIGTERM) produces
neither line on a headless server; that is why `server stop` writes the console
command instead. Then check nothing is left: port free, `mcli server list` shows no
pid, no `java … server.jar` process.

The three console paths:

```sh
printf 'list\nstop\n' | ./mcli server start mc-test --yes   # scripted: both commands run, then a graceful stop
./mcli server attach <name>                                  # follow and type; Ctrl+C only leaves
./mcli server cmd <name> say hi                              # one command, no session
```

Caveat worth remembering: commands written before the server's dispatcher is up are
answered with `An unexpected error occurred while trying to execute that command`, so
mcli waits for the server's own `Done (` line before forwarding anything.

## A server's own health (doctor and repair)

```sh
mv .../servers/mc-test/server.jar /tmp/keep
./mcli doctor mc-test                     # names the jar, its expected size, exit 1
./mcli repair mc-test --yes               # fetches the jar, and nothing else
printf 'eula=false\n' > .../servers/mc-test/eula.txt
./mcli doctor mc-test                     # reports the refused EULA with the URL
./mcli repair mc-test --yes               # must leave eula.txt exactly as it is
./mcli server create portclash --version 26.3 --port <same port> --eula --yes
./mcli doctor mc-test                     # names the other server on that port
```

Expect: the jar is checked against the checksum the version publishes (not just its
size), a killed detached server shows up as a stale pid, `doctor --all` covers servers
as well as instances, and repair writes only the jar.

## Fabric and other-launcher profiles

```sh
./mcli fabric list 26.3                          # loader builds Fabric publishes
./mcli create fab --version 26.3 --loader fabric # writes versions/fabric-loader-<loader>-<game>/
./mcli instance set fab --loader vanilla          # and back
./mcli launch fab --dry-run --yes                 # KnotClient, fabric-loader, -DFabricMcEmu verbatim
```

The classpath must contain `net/fabricmc/fabric-loader`, `sponge-mixin` and the
`org/ow2/asm` jars: Fabric's profile lists libraries as `name` + `url` + `sha1` with
**no** `downloads` block, so a launcher reading only `downloads.artifact` drops the
loader and the game dies without its main class. Compare against a profile another
launcher installed when one is available (the `mcuser` fixture has one).

A profile another launcher wrote must answer the same questions as one mcli wrote:
`instance show` prints its game version and loader, `instance set --loader vanilla`
moves it back to that game version.

## Content: mods, resource packs, packs

```sh
./mcli search sodium --version 1.21.4 --loader fabric   # one query, one page
./mcli add sodium --instance <fabric instance> --yes     # matching file, SHA-1 checked
./mcli add sodium --instance <fabric instance> --yes     # second run: already intact
./mcli mods <fabric instance>                            # what is installed
./mcli add sodium --instance <vanilla instance>          # must refuse and name --loader fabric
./mcli add <pack> --instance <matching> --pack-version <pack version> --yes
./mcli add <pack> --instance <1.21.4 instance>           # must refuse: wrong game version
./mcli update <instance> --pack --dry-run                # the two lists, before touching anything
./mcli update <instance> --pack --yes                     # takes back what the old pack added
```

`fabulously-optimized` is the real pack used for this (48 files, 41.9 MB): expect jars
in `mods/`, zips in `resourcepacks/`, a set of override files, a re-run that writes
nothing, and a file replaced by hand surviving an update — removal is decided by the
digest the pack declared, never by a timestamp.

## Moving an instance

```sh
./mcli update <instance> --version 1.21.4 --yes   # fetch and repoint
./mcli update <instance> --dry-run                # plan only
./mcli update <fabric instance> --dry-run         # keeps the loader: builds a profile for the new game
./mcli update <not-an-instance>                   # must refuse
```

The version an instance leaves behind keeps its files (another instance may run them);
an instance already on the target is only checked for completeness.

## Windows-shaped arguments, from Linux

`internal/launch/windows_args_test.go` asserts the classpath is joined with `;` and
that a path with a space is quoted — with `launch.Options.PathSeparator` set
explicitly, so it runs anywhere. `./scripts/check-cross.sh` proves every target still
compiles; behaviour on Windows itself is not verified and `docs/status.md` says so.
