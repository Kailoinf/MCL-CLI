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

Several accounts can be logged in at once. `mcli account list` shows them, `use`
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

## Servers

```sh
mcli server create survival --version latest --port 25566 --xmx 4G
mcli server start survival              # attached: the console is yours, Ctrl+C saves and stops
mcli server start survival --detach     # background, pid recorded
mcli server attach survival             # follow output and type commands; Ctrl+C only leaves
mcli server cmd survival say hello      # one command, no session
mcli server stop survival               # SIGINT, so the world is saved
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
