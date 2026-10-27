# Guide

## Install

Build the single binary (Go 1.26 or newer, standard library only):

```sh
git clone https://github.com/Kailoinf/MCL-CLI
cd MCL-CLI
go build -o mcli .
```

Put it somewhere on your `PATH`. There are no runtime dependencies: no Java
bundled, no Python, no node. mcli installs the Java runtime a version needs when
it is missing.

## First run

```sh
mcli login                       # opens the Microsoft page in a browser
mcli create vanilla --version latest
mcli launch vanilla
```

`launch` installs what the version needs and then starts the game. The game's
output goes to `<mcli home>/logs/game-<timestamp>.log`; `mcli doctor` prints the
newest one.

## Where things live

```
~/.mcl-cli/     mcli.toml (config) · state.toml (identity, no secrets)
                refresh.toml (long-lived tokens) · session.toml (short-lived)
                last.toml (previous launch) · jdk/ · cache/ · logs/
~/.minecraft/   libraries/ · assets/ · versions/<version>/ · versions/<instance>/
```

`MCLI_HOME` and `MCLI_MINECRAFT` override the two directories, which is how a
second, isolated setup is kept for testing. `mcli config path` prints them.

## Accounts

Several accounts can be logged in at once. Reinstalling the program does not touch your `~/.mcl-cli` directory. If a file there
was written by a much older layout — a `refresh.toml` mentioning unknown keys — mcli
says which keys it skipped and that the stored login cannot be used; it never rewrites
an older file by guessing. Log in again (which rewrites it) or delete
`~/.mcl-cli/refresh.toml`.

`mcli account list` shows them, `use`
switches the default, `launch --account <name>` overrides it for one run, and
`--no-switch` adds an account without changing the default. Credentials are
encrypted with a key derived from this machine, so the two token files cannot be
decrypted elsewhere — while `state.toml` stays portable.

## Instances

An instance is a named directory with its own saves, options and settings; the
heavy shared parts (libraries, assets, version files) live once in
`~/.minecraft`. `mcli instance set` changes memory, Java and arguments;
`mcli instance copy` clones one; `mcli instance scan` lists every version
directory on disk, including those another launcher created, and
`mcli instance import` adopts one in place — files and saves stay where they are.

`mcli list` shows the instances mcli has registered, not everything under the game
directory: pointing `minecraft_dir` at a full `.minecraft` (an official launcher, PCL,
HMCL) does not register anything by itself, so the list can look empty next to a
complete install. It says so when it sees unregistered versions, and adopting one is
one command:

```sh
mcli instance scan                    # what is there, and which are registered
mcli instance import 1.21.4           # adopt in place (--as <name> to rename)
```

Switching the directory (`mcli config set minecraft_dir <path>`) scans it and takes
over what it finds: every version directory with a version document becomes an
instance of the same name, files untouched, and each adoption is named in the output.
`mcli list` does the same before listing, so a full `.minecraft` never shows up as an
empty launcher.

## Servers

```sh
mcli server create survival --version latest --port 25566 --xmx 4G
mcli server start survival              # attached: the console is yours, Ctrl+C saves and stops
mcli server start survival --detach     # background, pid recorded
mcli server attach survival             # follow output and type commands; Ctrl+C only leaves
mcli server cmd survival say hello      # one command, no session
mcli server stop survival               # the server's own `stop`, so the world is saved
```

The server lives in `<minecraft_dir>/servers/<name>`. `eula.txt` and
`server.properties` are created once and never overwritten; the Minecraft EULA
URL is printed and has to be accepted (`--eula` skips the question, not the
notice).

## Fabric

```sh
mcli fabric list latest                                     # loader builds for a game version
mcli create modded --version latest --loader fabric         # newest stable loader
mcli create pinned --version latest --loader fabric --loader-version 0.16.9
mcli instance set modded --loader vanilla                    # and back to vanilla
```

The loader is a **choice on the version**, not a command of its own: `--loader
vanilla|fabric` on `create` and `instance set`, with `--loader-version` when you
want a specific build. A value outside the choice is an error that lists the
alternatives, never a silent fallback. The profile Fabric publishes is written
into mcli's own `versions/` directory, so it works offline afterwards and can be
launched, repaired and diagnosed like any other version.

## CurseForge needs your own API key

Modrinth needs nothing. CurseForge needs an API key belonging to the person using it,
so mcli will not ship one:

```sh
mcli config set curseforge_api_key <your key>   # stored in mcli.toml
mcli config get curseforge_api_key              # reports the length, never the value
mcli search jei --source curseforge
mcli add jei --instance modern --source curseforge
```

The key is sent as `x-api-key`, never printed, and never written to a log. Without one,
`search`/`add` with `--source curseforge` stop and tell you this exact command, and a
key CurseForge refuses is reported as refused, not as a network problem.

## Mods and content

Modrinth is the source mcli uses — it needs no API key, and its API is small enough
to wrap honestly.

```sh
mcli search sodium --version 1.21.4 --loader fabric    # one query, one page
mcli search faithful --type resourcepack --version 1.21.4
mcli add sodium --instance modded                     # downloads the file that fits
mcli add faithful --instance modded --dry-run          # what would land where
mcli mods modded                                       # what is already there
```

* The search is deliberately thin: one query and one page (`--limit`, `--offset`
  for the next), showing the slug to install, the type, downloads, author and
  title. There is no browsing UI to maintain.
* `add` picks the newest version that matches the instance's game version and
  loader, downloads its primary file with the SHA-1 the API declares, and puts it
  in the right place by project type: `mods/`, `resourcepacks/`, `shaderpacks/`,
  `datapacks/`.
* A mod needs a loader. Installing one into a vanilla instance is refused with the
  command that fixes it (`--loader fabric`), rather than dropping a Fabric jar into
  a game that cannot load it. `--loader` overrides when you know what you are doing.
* Required dependencies are printed but not installed automatically: a launcher
  that silently pulls three more projects makes a mess nobody can review.

Modpacks are the same `add` command: the project type is read from the project, so a
`.mrpack` installs as a pack.

```sh
mcli add fabulously-optimized --instance modded --dry-run   # what the pack wants
mcli add fabulously-optimized --instance modded             # install it
```

A pack is a list of files plus a loader plus a game version, so installing one is
mostly copying files to paths someone else decided — and the launcher's job is to
say what does not fit:

* The pack's own game version is checked against the instance. A 1.21.4 pack into a
  1.20.1 instance is refused with the `mcli create` command that builds the right
  one, because the result would not start and guessing would be worse.
* The loader the pack asks for is installed if the instance has none; if the
  instance already has a different build of that loader, mcli says so and carries
  on — loader differences usually work, but the pack's own build is the tested one.
* Every file is checked against the SHA-1 the pack lists, and paths are refused if
  they would land outside the instance directory. A pack is data from the internet,
  not an instruction to write wherever it likes.
* The pack's `overrides/` are copied in, but never over a file that already exists:
  a world or a config someone tuned by hand is not something a pack gets to
  overwrite. What was kept is reported.

## Moving an instance to another version

```sh
mcli update modded                       # to the newest release, keeping its loader
mcli update modded --version 1.21.4      # or a version you name
mcli update modded --dry-run             # what it would fetch, and nothing else
```

Everything that is not the game version stays where it is: the game directory, the
memory, the arguments, the mods, the worlds. An instance with a loader stays with
that loader — the new version is built as a profile for it — and the previous
version's files are left alone, because another instance may well be running them.
An instance already on the newest release is simply checked for completeness.

### Updating a pack

```sh
mcli add fabulously-optimized --instance modded --pack-version 15.0.0-beta.1   # a named pack version
mcli update modded --pack --dry-run      # what the move would add and take back
mcli update modded --pack                # move to the newest version that fits
```

A pack update is two lists: the files the new version wants, and the ones the old
version added that it no longer does. The second list is why each pack file is recorded
when the pack is installed — with the digest the pack declared for it. mcli compares
against that record, never against a directory listing, so a file you dropped in by
hand can never be removed; and a file is deleted only while its digest still
matches, so one you have replaced since is kept and reported instead.

## Platforms

mcli is one Go binary with no dependencies, and it compiles and is developed as a
plain Go binary for every platform below. `./scripts/check-cross.sh` builds all of
them from the repository root, and a target that stops compiling is treated as a bug.

| | Linux | Windows | macOS | note |
| --- | --- | --- | --- | --- |
| install, launch, instances, accounts | yes | yes | yes | the core |
| mods, resource packs, modpacks | yes | yes | yes | Modrinth needs no key |
| automatic heap sizing | yes | yes | yes | Windows asks kernel32, macOS half the physical total |
| servers, foreground and detached | yes | yes | yes | detached writes `logs/console.log` |
| `server attach` / `server cmd` | yes | **no** | yes | needs a FIFO for the server's input |
| `server stop` through the server's console | yes | **no** | yes | Windows has no FIFO: `--force` kills it and the world keeps its last autosave |
| free-space line in `doctor` | yes | yes | yes | Windows asks kernel32 |

Where something is missing, mcli says so and names the alternative rather than
failing with a low-level error: on Windows, `server attach` tells you to run the
server in the foreground, and a graceful stop says to use `--force`.

What has been *run*, rather than compiled, is listed in
[the status page](/status): everything except Windows and macOS is verified on
Linux, and Windows/macOS are compile-checked plus covered by tests that run
anywhere (the Windows classpath separator and quoting, for instance).
