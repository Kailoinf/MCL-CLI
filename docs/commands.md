# Commands

Every command accepts the global options: `--lang <en|zh>`, `--config <path>`,
`--yes` (never prompt), `--quiet`, `--verbose`, `--no-color`, `-h/--help`.

## Accounts

| Command | What it does |
| --- | --- |
| `mcli login [--flow browser\|device] [--label <name>] [--no-browser] [--no-switch] [--timeout <sec>]` | Microsoft login (browser flow by default) |
| `mcli logout [<account>] [--all]` | remove stored credentials |
| `mcli account [list \| use <name> \| remove <name>]` | show and switch the logged-in accounts |

## Instances

| Command | What it does |
| --- | --- |
| `mcli list [--instances] [--accounts] [--available] [--type <t>] [--search <text>] [--limit N]` | instances, accounts or downloadable versions |
| `mcli create <name> --version <v> [--loader vanilla\|fabric] [--loader-version <v>] [--java <path>] [--xmx 4G] [--xms 1G] [--jvm-arg …] [--game-arg …] [--force]` | create an instance, optionally on a mod loader |
| `mcli launch <name>\|<version>\|--last [--version <v>] [--java <path>] [--xmx] [--xms] [--jvm-arg] [--game-arg] [--width N] [--height N] [--server host[:port]] [--fullscreen] [--demo] [--dry-run] [--wait] [--account <a>]` | install if needed and start |
| `mcli instance [list \| show <name> \| set <name> [--loader vanilla\|fabric] [--loader-version <v>] … \| copy <src> <dst> [--with-files] \| rename <old> <new> \| scan [--all] \| import <version> [--as <name>]]` | inspect and change instances, adopt existing installs |
| `mcli delete <name> [--keep-files] [--yes]` | delete an instance |
| `mcli update [<instance>] [--version <v>] [--loader vanilla\|fabric] [--loader-version <v>] [--pack] [--pack-version <v>] [--dry-run] [--force]` | move an instance to another game version (newest release by default), or with `--pack` to a newer version of its modpack; keeps the loader, the settings and the files |
| `mcli doctor [<name>] [--all] [--deep]` | health check of the machine, of every instance, and of a server (jar against the version's checksum, EULA, port, last log); exits 1 when something is wrong |
| `mcli repair <name> [--deep] [--force] [--dry-run]` | fetch exactly the missing or damaged files; for a server, its jar — never `eula.txt`, `server.properties` or the world |
| `mcli refresh [--versions] [--all]` | force-refresh cached metadata |

## Servers

| Command | What it does |
| --- | --- |
| `mcli server create <name> --version <v> [--java <path>] [--xmx 2G] [--xms 512M] [--port 25565] [--dir <path>] [--jvm-arg …] [--eula]` | register a server |
| `mcli server set <name> [--version <v>] [--port N] [--java <path>] [--xmx] [--xms] [--jvm-arg …] [--clear-jvm-args] [--eula]` | change a server; `--eula` accepts the licence later, printing the URL and asking |
| `mcli server [list \| show <name> \| set <name> … \| delete <name> [--keep-files]]` | inspect and change servers |
| `mcli server start <name> [--dry-run] [--detach] [--eula] [--port N]` | fetch the jar if needed and start |
| `mcli server attach <name> [--lines N]` | follow a detached server's console |
| `mcli server cmd <name> <command…>` | send one command |
| `mcli server stop <name> [--force]` | stop a server the way that saves its world: the server's own `stop` through its console; `--force` kills instead |

## Fabric

| Command | What it does |
| --- | --- |
| `mcli fabric list <game> [--limit N]` | loader builds published for a game version |
| `mcli fabric list <game> [--limit N]` | the loader builds Fabric publishes (installing is `create --loader fabric`) |

## Mods and content (Modrinth)

| Command | What it does |
| --- | --- |
| `mcli search <query> [--type mod\|resourcepack\|shader\|datapack\|modpack] [--version <game>] [--loader <loader>] [--limit N] [--offset N]` | search Modrinth: one query, one page |
| `mcli add <project> [--instance <name>] [--version <game>] [--loader <loader>] [--type <t>] [--dry-run]` | install a project's matching file into an instance; `modpack` is detected from the project and installed as a whole pack (file list, overrides, loader) |
| `mcli mods [<instance>]` | list what an instance has in `mods/`, `resourcepacks/`, `shaderpacks/`, `datapacks/` |

## Java and configuration

| Command | What it does |
| --- | --- |
| `mcli java [list \| install <major> [--force] \| which <version>]` | inspect or install Java runtimes |
| `mcli config [get\|set\|unset\|list\|path]` | read or change settings in `mcli.toml` |
| `mcli version`, `mcli help [command]` | version and help |
