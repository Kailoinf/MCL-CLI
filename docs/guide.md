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
